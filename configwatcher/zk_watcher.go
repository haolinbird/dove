package configwatcher

import (
	"bytes"
	"doveclient/callbacks"
	"doveclient/config"
	"doveclient/configschema"
	"doveclient/logger"
	"doveclient/projconfig"
	"doveclient/storage"
	diskStorage "doveclient/storage/leveldb"
	rs "doveclient/storage/zk"
	"doveclient/util"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	// "github.com/samuel/go-zookeeper/zk"
	"bufio"
	"context"
	"regexp"

	"github.com/coreos/etcd/clientv3"
	// "strconv"
)

const (
	// WATCH_LIST_INTIAL_SIZE 监控列表初始化大小
	WATCH_LIST_INTIAL_SIZE = 100

	// KEY_NAME_OF_STORE_DYNAMIC_REG_CONF_NODES 动态注册的配置项信息(磁盘)存储名称
	KEY_NAME_OF_STORE_DYNAMIC_REG_CONF_NODES = ":dynamically_registered_config_nodes"

	// 获取配置失败后，重新获取配置的间隔时间
	DELAY_OF_RECHECK_FAILED_CONFIG_ITEM = 15

	// MIN_INTERVAL_OF_EXEC_UPDATE_POST_CALLBACK 执行更新回调操作的最小间隔时间. 单位: 秒。（以最后的更新指令为准。更新操作生效会有固定的延时。这个值不应太大。）
	MIN_INTERVAL_OF_EXEC_UPDATE_POST_CALLBACK = 2
)

var once sync.Once

var zkStorage *rs.Storage

// 更新回调最后一次被请求执行的时间
var lastPostUpdateCallbackReqtime int64

// 更新请求发送通道
var callbackReqChan = make(chan int64, 1)

// 配置项根节点跟踪列表
var watchList = make(map[string]WatchInfo, WATCH_LIST_INTIAL_SIZE)

var schemaFiles []configschema.SchemaFile

var updateLock = make(chan int8, 1)
var updateCounter = &struct {
	lock    *sync.Mutex
	counter map[string]int
}{new(sync.Mutex), make(map[string]int)}

var watchListWRLock = make(chan int, 1)
var lastPostUpdateCallbackReqtimeWRLock = make(chan int, 1)
var registeredNodesRWLock = make(chan int, 1)

// 通过API动态获取的配置项
type registeredNodeInfo struct {
	Name           string
	LastAccessTime int64
	RegTime        int64
}

var registeredNodes = make(map[string]registeredNodeInfo, 200)

type WatchInfo struct {
	Name    string
	RegTime int64
	// 1 正在监听; 0 已经取消监听
	State int8
	// watcher 通讯通道: 1 退出监听
	Chan *chan int8
}

var monitorNodeLease clientv3.LeaseID
var leaseOpLock chan int = make(chan int, 1)

func saveMonitorNodeLease(lease clientv3.LeaseID) {
	leaseOpLock <- 1
	defer func() {
		<-leaseOpLock
	}()

	monitorNodeLease = lease
}

func getMonitorNodeLease() clientv3.LeaseID {
	leaseOpLock <- 1
	defer func() {
		<-leaseOpLock
	}()

	return monitorNodeLease
}

func lockWatchList() {
	watchListWRLock <- 1
}

func unlockWatchList() {
	<-watchListWRLock
}

func getRegisteredNodeInfo(name string) *registeredNodeInfo {
	registeredNodesRWLock <- 1
	if nodeInfo, exists := registeredNodes[name]; exists {
		<-registeredNodesRWLock
		return &nodeInfo
	} else {
		<-registeredNodesRWLock
		return nil
	}
}

func getAllRegisteredNodeInfo() map[string]registeredNodeInfo {
	registeredNodesRWLock <- 1
	nodesInfo := registeredNodes
	<-registeredNodesRWLock
	return nodesInfo
}

func setRegisterdNodeInfo(name string, nodeInfo registeredNodeInfo) {
	registeredNodesRWLock <- 1
	registeredNodes[name] = nodeInfo
	<-registeredNodesRWLock
}

func deleteRegisteredNodeInfo(name string) {
	registeredNodesRWLock <- 1
	delete(registeredNodes, name)
	<-registeredNodesRWLock
}

func setLastPostUpdateCallbackReqtime(t int64) {
	lastPostUpdateCallbackReqtimeWRLock <- 1
	lastPostUpdateCallbackReqtime = t
	<-lastPostUpdateCallbackReqtimeWRLock
}

func GetLastPostUpdateCallbackReqtime() int64 {
	lastPostUpdateCallbackReqtimeWRLock <- 1
	t := lastPostUpdateCallbackReqtime
	<-lastPostUpdateCallbackReqtimeWRLock
	return t
}

func getWatchNodeFromWatchList(name string) (*WatchInfo, bool) {
	lockWatchList()
	defer unlockWatchList()
	if info, exists := watchList[name]; exists {
		return &info, exists
	} else {
		return nil, exists
	}
}

/*func monitorZkConn(eventChan <-chan zk.Event) {
	lastConnState := zk.StateUnknown
	for {
		ev := <-eventChan
		switch ev.State {
		case zk.StateDisconnected:
			lastConnState = zk.StateDisconnected
			logger.GetLogger("WARN").Print("Zookeepr disconnected! Waiting for re-establishment of conneciton...")
		case zk.StateConnected:
			if lastConnState == zk.StateDisconnected {
				lastConnState = zk.StateConnected
				logger.GetLogger("INFO").Print("Zookeepr reconnected! Updating of all configs will be added to recheckqueue.")
				addToRecheckQueue("*")
			}
		}
	}
}*/

// 发送更新回调执行请求
func reqCallback(timestamp int64) {
	callbackReqChan <- timestamp
}

// 接收更新回调执行请求
func receiveReqCallback() {
	for {
		setLastPostUpdateCallbackReqtime(<-callbackReqChan)
	}
}

//是否可以执行更新回调，以免回调被执行的过于频繁
func getLockOfExecCallback() bool {
	t := time.Now()
	timestamp := t.UnixNano()
	reqCallback(timestamp)
	time.Sleep(time.Second * MIN_INTERVAL_OF_EXEC_UPDATE_POST_CALLBACK)
	return GetLastPostUpdateCallbackReqtime() == timestamp
}

var reportTimeTag int64
var reportCheckLock = new(sync.Mutex)

//是否可以执行更新结果上报，避免频繁上报更新信息.
func getLockOfExecResultReporting() bool {
	myTag := time.Now().UnixNano()
	reportCheckLock.Lock()
	reportTimeTag = myTag
	reportCheckLock.Unlock()
	time.Sleep(time.Second * MIN_INTERVAL_OF_EXEC_UPDATE_POST_CALLBACK)
	reportCheckLock.Lock()
	tag := reportTimeTag
	reportCheckLock.Unlock()
	return tag == myTag
}

func Start() error {
	// 加载配置文件状态
	schemaFiles = configschema.LoadSchemaFiles()
	var err error
	// var connEvent <-chan zk.Event
	// zkStorage, connEvent, err = rs.CreateConn()
	zkStorage, err = rs.GetConn(updateProjectConfigByRootNode)
	if err != nil {
		return err
	}

	go func() {
		logger.GetLogger("INFO").Print("Registering client to config server...")
		err = registerMonitor()

		if err != nil {
			errMsg := fmt.Sprintf("Failed to register client to config server: %s", err)
			logger.GetLogger("ERROR").Print(errMsg)
		} else {
			logger.GetLogger("INFO").Print("Client register successful.")
		}
		// 从磁盘加载通过API动态注册的配置项,以便在配置更新时，这些配置项也能被更新到。
		LoadRegisteredNodesFromDisk()

		// go monitorZkConn(connEvent)
		go syncRegisteredNodesToDisk()
		go receiveRecheck()
		go receiveReqCallback()
		go gcWatcher()

		// 启动时，主动检查所有配置的更新(包括registeredNodes)
		logger.GetLogger("INFO").Println("Init run...check updates for all configs....")
		updateProjectConfigByRootNode("*", true)

		items := configschema.GetAllSchemaItemRootNodes()
		for _, rootNode := range items {
			logger.GetLogger("INFO").Printf("Adding config root node [%s] to watching list...", rootNode)
			AddToWatchList(rootNode)
		}
		go watchDebugOrSchemafiles()
	}()
	return nil
}

// 这些节点需被添加到watchlist中。
// 一般只在客户端启动时运行一次。
func LoadRegisteredNodesFromDisk() bool {
	logger.GetLogger("INFO").Print("Loading dynamically registered config item names from disk...")
	if !diskStorage.IsIntialized() {
		diskStorage.InitDb(diskStorage.Config{Dsn: config.GetLocalStorageDir()})
	}
	errMsgTpl := "Failed to load dynamically registered config nodes from disk or no nodes have been registered: %s"
	db, err := diskStorage.GetDb()
	if err != nil {
		logger.GetLogger("ERROR").Printf(errMsgTpl, err)
	} else {
		var nodesB []byte
		nodesB, err = db.Conn.Get([]byte(KEY_NAME_OF_STORE_DYNAMIC_REG_CONF_NODES), nil)
		nodes := make(map[string]interface{}, 200)
		if err != nil {
			logger.GetLogger("WARN").Printf(errMsgTpl, err)
		} else {
			err = json.Unmarshal(nodesB, &nodes)
			if err != nil {
				logger.GetLogger("WARN").Printf(errMsgTpl, err)
			} else {
				for name, infoRaw := range nodes {
					info := infoRaw.(map[string]interface{})
					node := registeredNodeInfo{Name: info["Name"].(string), RegTime: int64(info["LastAccessTime"].(float64)), LastAccessTime: int64(info["LastAccessTime"].(float64))}
					if oNode := getRegisteredNodeInfo(name); oNode != nil && (*oNode).LastAccessTime > node.LastAccessTime {
						node.LastAccessTime = (*oNode).LastAccessTime
					}
					setRegisterdNodeInfo(name, node)
					AddToWatchList(strings.Split(name, ".")[0])
				}
			}
		}
	}

	if err != nil {
		return false
	}
	return true
}

func SaveRegisteredNodesToDisk() bool {
	logger.GetLogger("INFO").Print("Sync dynamically registered config item names to disk...")
	if !diskStorage.IsIntialized() {
		diskStorage.InitDb(diskStorage.Config{Dsn: config.GetLocalStorageDir()})
	}
	db, err := diskStorage.GetDb()
	if err == nil {
		registeredNodesRWLock <- 1
		nodesB, err := json.Marshal(registeredNodes)
		<-registeredNodesRWLock
		if err == nil {
			err = db.Conn.Put([]byte(KEY_NAME_OF_STORE_DYNAMIC_REG_CONF_NODES), nodesB, nil)
		}
	}
	if err != nil {
		logger.GetLogger("ERROR").Printf("Failed to load dynamically registered config nodes from disk: %s", err)
		return false
	}
	return true
}

func getUpdateLock(name string) bool {
	updateCounter.lock.Lock()
	if _, ok := updateCounter.counter[name]; ok {
		logger.GetLogger("INFO").Printf("Current waiting count for update tasks[%v]: %v", name, updateCounter.counter[name])
		if updateCounter.counter[name] > 1 {
			// maybe, already has one waiting and one running, and the one that is waiting must have the latest version of configs.
			updateCounter.lock.Unlock()
			return false
		} else {
			updateCounter.counter[name]++
		}
	} else {
		updateCounter.counter[name] = 1
		logger.GetLogger("INFO").Printf("Current waiting count for update tasks[%v]: %v", name, updateCounter.counter[name])
	}
	updateCounter.lock.Unlock()
	updateLock <- 1
	return true
}

func releaseUpdateLock(name string) bool {
	updateCounter.lock.Lock()
	defer updateCounter.lock.Unlock()
	updateCounter.counter[name]--
	<-updateLock
	return true
}

// 向配置服务器注册监控节点
func registerMonitor() error {
	/*monitorNodeRoot := rs.MonitorNodeRoot()
	if rootExists, _, _ := zkStorage.Conn.Exists(monitorNodeRoot); !rootExists {
		_, err := zkStorage.Conn.Create(monitorNodeRoot, []byte{}, 0, []zk.ACL{zk.ACL{Scheme: "world", ID: "anyone", Perms: zk.PermAll}})
		if err != nil {
			return errors.New(fmt.Sprintf("Failed to create monitor root node[%s]: %s.", monitorNodeRoot, err))
		}
	}*/
	monitorNode, err := rs.MyMonitorNode()
	if err != nil {
		return errors.New(fmt.Sprintf("Failed to get monitor node: %s", err))
	}
	clientMd5, err := util.Md5File(os.Args[0])
	if err != nil {
		return errors.New(fmt.Sprintf("Failed to get client md5: %s", err))
	}

	lease, err := zkStorage.Conn.Grant(45)
	if err != nil {
		return err
	}
	saveMonitorNodeLease(lease)

	registerationInfo := map[string]interface{}{"client_md5": clientMd5 + "\nVER:" + config.GetConfig().ClientVer, "config_dir": config.GetConfig().CONFIG_DIR}
	if nodeExists, _ := zkStorage.Conn.Exists(monitorNode); !nodeExists {
		_, err := zkStorage.Conn.Create(monitorNode, []byte{}, lease)
		if err != nil {
			return err
		}
	}
	infoB, err := json.Marshal(registerationInfo)
	if err != nil {
		return errors.New(fmt.Sprintf("Failed to combine client registeration info: %s", err))
	}
	_, err = zkStorage.Conn.Set(monitorNode, infoB, lease)
	if err != nil {
		return err
	}
	_, err = zkStorage.Conn.KeepAlive(context.TODO(), getMonitorNodeLease())
	return err
}

func AddToWatchList(name string) (hasBeenWatching bool) {
	if info, exists := getWatchNodeFromWatchList(name); exists && (*info).State == int8(1) {
		return true
	}
	go watchItem(name)
	return false
}

// 记录通过API获取的配置key。(而非通过解析schema文件)
// 并对key进行watch，如果有更新，则触发配置更新事件.
// RETURN: 对应的根节点之前是否已经被监听.
func TouchDynamicNode(name string) bool {
	// 如果运行在过渡性的临时备份实例，则不进行配置监听.
	if config.GetConfig().RunAsBi {
		return true
	}
	var hasBeenWatching bool
	if oNode := getRegisteredNodeInfo(name); oNode == nil {
		setRegisterdNodeInfo(name, registeredNodeInfo{Name: name, RegTime: time.Now().Unix(), LastAccessTime: time.Now().Unix()})
		rootNode := strings.Split(name, ".")[0]
		if info, exists := getWatchNodeFromWatchList(name); exists && (*info).State == int8(1) {
			hasBeenWatching = true
		} else {
			hasBeenWatching = false
		}
		AddToWatchList(rootNode)
		SaveRegisteredNodesToDisk()
	} else {
		hasBeenWatching = true
		temp := getRegisteredNodeInfo(name)
		(*temp).LastAccessTime = time.Now().Unix()
		setRegisterdNodeInfo(name, (*temp))
	}
	return hasBeenWatching
}

func syncRegisteredNodesToDisk() {
	for {
		time.Sleep(time.Second * 300)
		SaveRegisteredNodesToDisk()
	}
}

// 监听配置在服务端的更新。
// 如果收到退出监听信号时则return, 不再进行监听(发生在配置可能不再被使用时)。
func watchItem(name string) {
	// var err error
	// var exists bool
	// var evChan <-chan zk.Event
	regTime := time.Now().Unix()
	path := rs.CN2P(name)
	wChan := make(chan int8, 1)
	lockWatchList()
	watchList[name] = WatchInfo{RegTime: regTime, Name: name, State: int8(1), Chan: &wChan}
	unlockWatchList()
	evChan := zkStorage.Conn.ExistsW(path)
	logger.GetLogger("INFO").Printf("watched new config prefix[%v] has been watched.", path)
	for {
		/*exists, _, evChan, err = zkStorage.Conn.ExistsW(path)
		if !exists {
			logger.GetLogger("WARN").Printf("config [%s] not exists on config server, your app may encounter errors if it's required!\n", name)
		}*/

		/*if err != nil {
			logger.GetLogger("WARN").Printf("Failed to watch config [%s]: %s. It's hard to deal such problem. Please info the admin ASAP! Retry in 3 seconds.\n", name, err)
			time.Sleep(time.Second * 3)
		} else {*/
		select {
		case ev := <-evChan:
			/*switch ev.Type {
			case zk.EventNodeDataChanged, zk.EventNodeCreated:
				logger.GetLogger("INFO").Printf("Received config change event on node [%s], begin to perform update...\n", name)
				go updateProjectConfigByRootNode(name, false)
			default:
				logger.GetLogger("WARN").Printf("Non-handlable zk event: %s on item: %v, state: %v\n", ev.Type, ev.Path, ev.State)
			}*/
			rewatch := false
			if ev.Canceled {
				logger.GetLogger("WARN").Printf("watch on [%v] has been canceled by server, prepare to re-watch.", name)
				rewatch = true
			} else if err := ev.Err(); err != nil {
				logger.GetLogger("WARN").Printf("watch on [%v] encountered an error: %v. prepare to re-watch.", name, err)
				rewatch = true
			}

			if ev.Events != nil {
				logger.GetLogger("INFO").Printf("Received config change event on node [%s], begin to perform update...\n", name)
				go updateProjectConfigByRootNode(name, false)
				continue
			} else {
				// connection streams may be broken.
				rest := time.Second * 5
				logger.GetLogger("WARN").Printf("watcher streams is broken for [%v], try re-watch in %v...", name, rest)
				time.Sleep(rest)
				rewatch = true
			}

			if rewatch {
				logger.GetLogger("INFO").Printf("re-watching item [%v]...", name)
				evChan = zkStorage.Conn.ExistsW(path)
				continue
			}

		// the connection to server has been re-created, re-watch is needed.
		case <-zkStorage.Conn.GetDriverResetChan():
			evChan = zkStorage.Conn.ExistsW(path)

		// 收到退出监听的信号
		case <-wChan:
			logger.GetLogger("INFO").Printf("Un-watch directive received, quit watching[%s]...", name)
			for itemNode, _ := range getAllRegisteredNodeInfo() {
				if strings.Split(itemNode, ".")[0] == name {
					deleteRegisteredNodeInfo(itemNode)
				}
			}
			syncRegisteredNodesToDisk()
			lockWatchList()
			watchList[name] = WatchInfo{RegTime: regTime, Name: name, State: int8(0), Chan: &wChan}
			unlockWatchList()
			close(wChan)
			return
		}
		// }
	}
}

// GetNewValueOfConfig 获取配置的最新值。（考虑config.debug.toml、config server、localstorage)
// cErr 配置获取时相关的错误
// 如果debug文件中有对应的值, newVersionChecked及hasUpdate始终为true
func GetNewValueOfConfig(dItem string, dbc *projconfig.DebugConfigs, schemaFile string) (valueEncrypted []byte, valuePlain []byte, valueSerialized []byte, newVersionChecked bool, hasUpdate bool, isDebugValue bool, cErr error) {
	// 从TOML反转出来的配置(json)
	var parsedContent []byte

	// 最新的配置内容
	var newContent []byte

	// 为Dev环境，并且有debug的值.json字节码
	var debugValue []byte
	var debugValueToml string
	// oErr 旧配置获取时相关的错误
	var oErr error
	// 本地存储的明文TOML格式
	oldContent, oErr := projconfig.GetFromDisk(dItem)

	if config.GetConfig().DEVMODE && dbc != nil {
		debugValue, debugValueToml = dbc.GetConfigBySchemafile(dItem, schemaFile, true)
	}

	// 获取到了debug值
	if debugValue != nil {
		logger.GetLogger("INFO").Printf("Found debug value for [%s], now take this one.", dItem)
		isDebugValue = true
		newVersionChecked = true
		valuePlain = []byte(debugValueToml)
		parsedContent = debugValue
	} else {
		logger.GetLogger("INFO").Printf("Fetching value of config[%s] from configure server...", dItem)
		// 服务端存储的明文TOML格式
		newContent, cErr = projconfig.GetFromServer(dItem)

		// 不能从服务器上获取配置内容.
		if cErr != nil {
			logger.GetLogger("ERROR").Printf("Failed to check content from config server for config[%s] : %s", dItem, cErr)
			logger.GetLogger("INFO").Printf("Try to get config[%s]  from local storage...", dItem)
			if oErr != nil {
				logger.GetLogger("ERROR").Printf("Failed to get config[%s]  from local storage: %s", dItem, oErr)
			} else {
				// 从服务器上去不到最新配置时，则使用旧配置
				cErr = nil
				parsedContent, cErr = util.TomlConfigValueToJson(string(oldContent))
				logger.GetLogger("INFO").Printf("Successfully recovered config[%s]  from local storage, but it's probably not the latest version!", dItem)
			}
		} else {
			logger.GetLogger("INFO").Printf("Successfully fetched content from config server for config[%s]\n", dItem)

			parsedContent, cErr = util.TomlConfigValueToJson(string(newContent))
			// 如果服务端的配置无法被解析，则尝试是用本地的配置。
			if cErr != nil {
				logger.GetLogger("ERROR").Printf("Failed to parse  toml string  of  config[%s] from server: %s", dItem, cErr)
				if oErr == nil {
					logger.GetLogger("INFO").Print("Try old one...")
					parsedContent, cErr = util.TomlConfigValueToJson(string(oldContent))
					if cErr != nil {
						logger.GetLogger("ERROR").Printf("Old copy of config[%s] toml string cannot be parsed, drop. ERR: %v", dItem, cErr)
					} else {
						cErr = nil
					}
				} else {
					logger.GetLogger("ERROR").Printf("Old copy of config[%s] not found! Cannot not get config content by any means.", dItem)
				}
			} else {
				// 新版本成功获取
				newVersionChecked = true
				// 如果没有获取到老版本，或者老版本与服务端的新版本不相同，则认为有更新发生.
				if oErr != nil || string(oldContent) != string(newContent) {
					hasUpdate = true
					if oErr == nil {
						logger.GetLogger("INFO").Printf("Successfully fetched new value for config[%s] from config server.", dItem)
					}
				}
			}
		}
	}

	// 成功获取到配置(但有可能是本地的老配置)
	if cErr == nil {
		if newVersionChecked {
			if !isDebugValue {
				valuePlain = newContent
			}
		} else {
			valuePlain = oldContent
		}
		valueEncrypted, cErr = projconfig.GetEncryptor().Encrypt(valuePlain)
		if cErr != nil {
			logger.GetLogger("ERROR").Printf("Failed to generate encrypted content for config[%s] : %s\n", dItem, cErr)
		}
		valueSerialized = parsedContent

		projconfig.SaveRawToml(dItem, string(valuePlain))
	}
	return
}

// 配置文件schema有更新时调用此方法。
func updateProjectConfigBySchemafile(schemafile string) {
	if !getUpdateLock(schemafile) {
		logger.GetLogger("ERROR").Printf("Failed to get config update lock. Aborting update [%s]\n", schemafile)
		return
	}

	defer releaseUpdateLock(schemafile)
	// err 无法编译模板； cErr模板中的配置内容获取出错.
	var err, cErr error
	definedItems, err := configschema.ParseItems(schemafile)
	if err != nil {
		logger.GetLogger("ERROR").Printf("Failed to parse schemafile [%s]: %s. Abort.", schemafile, err)
	} else {
		// 记录schema文件中的节点，并返回该schema本次没有，上次有的node
		newNodes := callbacks.UpdateSchemaNodesAndDiff(schemafile, definedItems)
		itemValues := make(map[string][]byte, 100)
		var dbc *projconfig.DebugConfigs
		var dErr error
		if config.GetConfig().DEVMODE {
			dbc, dErr = projconfig.LoadDebugConfigs()
			if dErr != nil {
				logger.GetLogger("WARN").Printf("Failed to load debug config file: %s", dErr)
			}
		}
		var itemValue []byte
		for _, name := range definedItems {
			hasBeenWatching := AddToWatchList(strings.Split(name, ".")[0])
			// 如果配置已经被watch,则不需要再获取最新配置.
			if hasBeenWatching {
				itemValue, cErr = projconfig.GetConfig(name, dbc, schemafile, false)
			} else {
				var valuePlain []byte
				var itemHasUpdate, isDebugValue bool
				_, valuePlain, itemValue, _, itemHasUpdate, isDebugValue, cErr = GetNewValueOfConfig(name, dbc, schemafile)
				if cErr == nil && itemHasUpdate && !isDebugValue {
					projconfig.StoreToMem(name, itemValue)
					projconfig.SaveToDisk(name, valuePlain)
					// 该节点的内容有变更
					newNodes = append(newNodes, name)
				}
			}

			if cErr == nil {
				itemValues[name] = itemValue
			} else {
				logger.GetLogger("ERROR").Printf("Failed to get config[%s] when compiling schema file[%s]: %v", name, schemafile, cErr)
			}
		}
		logger.GetLogger("INFO").Printf("Compiling schema [%s]...", schemafile)
		err = compile(&itemValues, schemafile)
		if err != nil {
			logger.GetLogger("ERROR").Printf("Failed to compile schema file[%s]: %s", schemafile, err)
		} else {
			logger.GetLogger("INFO").Printf("Compiling of schema file[%s] complete.", schemafile)
		}

		// 记录本次更新涉及到的节点(最后传递给callback)
		callbacks.AddUpdatedNodes(newNodes...)
	}

	go func() {
		if !getLockOfExecCallback() {
			return
		}
		var callbackResults, callbackScriptResults map[string]string
		if err == nil {
			callbackResults = invokeCallbacks()
			callbackScriptResults = invokeCallbackScripts()
		}
		var errMsg []string
		for _, e := range []error{err, cErr} {
			if e != nil {
				errMsg = append(errMsg, e.Error())
			}
		}
		var sErr error
		if len(errMsg) > 0 {
			sErr = fmt.Errorf("%v", errMsg)
			logger.GetLogger("INFO").Printf("Add [%v] to recheck queue.", schemafile)
			addToSchemaRecheckQueue(schemafile)
		}

		uErr := saveUpdateResults(sErr, callbackResults, callbackScriptResults)
		if uErr != nil {
			logger.GetLogger("ERROR").Printf("%s", uErr)
		}
	}()
}

// 根据配置的根节点更新相关的配置。当configRoot为""时，则更新所有配置。一般由服务端的更新事件触发(DEV模式下，调试文件的修改也可触发)。
// 此过程会将服务端最新的配置保存至本地缓存。（而从debug文件中获取的值将不会被保存）
// configRoot 等于"*"时更新所有配置项。
// 客户端初次运行时可以使用参数 updateProjectConfigByRootNode("*", true)
func updateProjectConfigByRootNode(configRoot string, forceCompile bool) {
	logger.GetLogger("INFO").Printf("Try to update projects configs, root node[%v].", configRoot)
	if !getUpdateLock(configRoot) {
		logger.GetLogger("ERROR").Printf("Failed to get config update lock. Aborting update [%s]\n", configRoot)
		return
	}
	defer releaseUpdateLock(configRoot)
	schemaFilesNew := configschema.LoadSchemaFiles()
	// schemaContainsUpdate 当前schema文件中包含的配置项是否有更新;
	// itemHasUpdate任意一个schema文件或其配置项有更新.
	// newVersionChecked 成功的对服务端的配置进行了检查
	// hasUpdateOverall schema文件或其中定义的配置有更新
	// isDebugValue 是否是从debug文件中取出来的值
	var schemaContainsUpdate, itemHasUpdate, newVersionChecked, hasUpdateOverall, isDebugValue bool
	// sErr schema编译出错; psErr schema文件解析出错;cErr 配置项获取出错; pErr 本地持久化缓存出错; err 其它错误或最终的错误; uErr 向服务端汇报更新结果出错; dErr debug配置相关的错误.
	// 其中uErr, dErr不影响最终的更新成败.
	var uErr, pErr, cErr, psErr, sErr, dErr, err error

	var valueEncrypted, valuePlain, valueSerialized []byte
	//  保存原始的加密后的TOML
	newItemValuesEncrypted := make(map[string][]byte, 200)
	// 保存解密后配置内容
	newItemValuesPlain := make(map[string][]byte, 200)
	// 保存序列化后的配置内容
	newItemValuesSerialized := make(map[string][]byte, 200)
	// 有更新的配置项
	itemsHasUpdate := []string{}

	// 新配置内容获取失败的配置项
	failedConfigItems := []string{}

	// 从debug文件中获取到的值的配置项名
	var fetchedDebugValues []string

	var dbc *projconfig.DebugConfigs

	if config.GetConfig().DEVMODE {
		logger.GetLogger("INFO").Print("Running in DEV mode, config values in debug file will be taken.")
		dbc, dErr = projconfig.LoadDebugConfigs()
		if dErr != nil {
			logger.GetLogger("ERROR").Printf("Failed to load debug configs: %v", dErr)
		}
	}

	// 根据schema中的定义来获取相关配置的最新值。只要schema中定义的配置有改变，或者schema文件有更新，都将重新编译配置文件，并执行更新回调。
	for _, schemaFile := range schemaFilesNew {
		logger.GetLogger("INFO").Printf("=====Updating configs according to [%s]...=====", schemaFile.Path)
		schemaContainsUpdate = false
		definedItems, err := configschema.ParseItems(schemaFile.Path)
		if err != nil {
			logger.GetLogger("ERROR").Printf("Failed to parse schemafile [%s]: %s. Skipp...", schemaFile.Path, err)
			psErr = fmt.Errorf("%v:%v", err, psErr)
			continue
		}

		// 获取配置项的最新值
		for _, dItem := range definedItems {
			// 在更新其它schema时已经检查过，并且有更新(则不必再次获取最新配置)，则此schema也应该进行更新。
			// (只有DEV模式下才会发生)此配置项的值可能是从之前的debug文件中获取到的，则应该继续检查（因为可能有多个debug文件包含相同的配置项）。
			if util.InArrayString(&itemsHasUpdate, dItem) && !config.GetConfig().DEVMODE {
				schemaContainsUpdate = true
				continue
			}

			itemHasUpdate = false
			if config.GetConfig().DEVMODE || ((configRoot == "*" || strings.Index(dItem, configRoot+".") == 0) && !util.ExistsInMap(newItemValuesEncrypted, dItem)) {
				valueEncrypted, valuePlain, valueSerialized, newVersionChecked, itemHasUpdate, isDebugValue, cErr = GetNewValueOfConfig(dItem, dbc, schemaFile.Path)
				if cErr == nil {
					newItemValuesEncrypted[dItem] = valueEncrypted
					newItemValuesPlain[dItem] = valuePlain
					newItemValuesSerialized[dItem] = valueSerialized
					if itemHasUpdate {
						if !util.InArrayString(&itemsHasUpdate, dItem) {
							itemsHasUpdate = append(itemsHasUpdate, dItem)
						}
						schemaContainsUpdate, hasUpdateOverall = true, true
					}

					if isDebugValue && !util.InArrayString(&fetchedDebugValues, dItem) {
						fetchedDebugValues = append(fetchedDebugValues, dItem)
					}
				}

				if cErr != nil || !newVersionChecked {
					failedConfigItems = append(failedConfigItems, dItem)
					logger.GetLogger("ERROR").Printf("Config[%s]  which defined in [%s],  cannot be checked on config server. Re-check and update in %d about seconds.", dItem, schemaFile.Path, DELAY_OF_RECHECK_FAILED_CONFIG_ITEM)
				}
			}
		}

		if forceCompile || schemaContainsUpdate {
			if forceCompile {
				logger.GetLogger("INFO").Printf("Force option enabled, begin compile project config file[%s].", schemaFile.Path)
			} else {
				logger.GetLogger("INFO").Printf("Configs or schema file changed! Begin compile config file[%s].", schemaFile.Path)
			}
			// 重新编译配置文件
			sErr = compile(&newItemValuesSerialized, schemaFile.Path)
			if sErr != nil {
				logger.GetLogger("ERROR").Printf("Failed to compile schema file[%s]: %s", schemaFile.Path, sErr)
			}
		} else {
			logger.GetLogger("INFO").Printf("No config updates for schema file[%s], skip re-compiling.", schemaFile.Path)
		}
		logger.GetLogger("INFO").Printf("Finished updating configs according to [%s]...", schemaFile.Path)
	}

	var registeredNodesHasUpdate bool

	//根据API注册进来的配置项获取最新值
	for dItem := range getAllRegisteredNodeInfo() {
		if configRoot != "*" && strings.Index(dItem, configRoot+".") != 0 {
			continue
		}
		if _, exists := newItemValuesEncrypted[dItem]; !exists {
			valueEncrypted, valuePlain, valueSerialized, newVersionChecked, itemHasUpdate, isDebugValue, cErr = GetNewValueOfConfig(dItem, dbc, "")
			if cErr == nil {
				newItemValuesEncrypted[dItem] = valueEncrypted
				newItemValuesPlain[dItem] = valuePlain
				newItemValuesSerialized[dItem] = valueSerialized
				if itemHasUpdate {
					registeredNodesHasUpdate = true
				}

				if isDebugValue {
					fetchedDebugValues = append(fetchedDebugValues, dItem)
				}
			}
			if cErr != nil || !newVersionChecked {
				failedConfigItems = append(failedConfigItems, dItem)
				logger.GetLogger("ERROR").Printf("Failed to check content of config[%s] on config server.  Re-check and update in %d about second.", dItem, DELAY_OF_RECHECK_FAILED_CONFIG_ITEM)
			}
		}
	}

	if hasUpdateOverall || registeredNodesHasUpdate {
		// 更新配置至本地磁盘存储
		if !diskStorage.IsIntialized() {
			diskStorage.InitDb(diskStorage.Config{Dsn: config.GetLocalStorageDir()})
		}
		var db *diskStorage.Db
		db, pErr = diskStorage.GetDb()
		if pErr != nil {
			logger.GetLogger("ERROR").Printf("Failed to get local storage engine. Not going to save local copies. ERROR: %s\n", pErr)
		} else {
			logger.GetLogger("INFO").Println("Saving new configs to local storage...")
			var itemValues *map[string][]byte
			if config.GetConfig().ENCRYPT_ENABLE {
				itemValues = &newItemValuesEncrypted
			} else {
				itemValues = &newItemValuesPlain
			}
			for configName, configValue := range *itemValues {
				// debug文件中的配置不保存
				if config.GetConfig().DEVMODE && util.InArrayString(&fetchedDebugValues, configName) {
					continue
				}
				logger.GetLogger("INFO").Printf("Saving config[%s] to local storage.", configName)
				pErr = db.Conn.Put([]byte(configName), configValue, nil)
				if pErr != nil {
					logger.GetLogger("ERROR").Printf("Failed to save config[%s] to local storage: %s", configName, pErr)
				}
			}
		}

		// 更新配置至内存
		for dItem, content := range newItemValuesSerialized {
			// debug文件中的配置不保存
			if config.GetConfig().DEVMODE && util.InArrayString(&fetchedDebugValues, dItem) {
				continue
			}
			storage.StoreToMem(dItem, content)
		}
	}

	var lastMsg string

	if err != nil || len(failedConfigItems) > 0 {
		logger.GetLogger("ERROR").Printf("All config items are checked through, but these are failed: %v. Sending to re-check queue...re-check and update in %d around seconds.", failedConfigItems, DELAY_OF_RECHECK_FAILED_CONFIG_ITEM)
		lastMsg = "Update for [%s] not completed, error remains."
		addToRecheckQueue(configRoot)
	} else {
		lastMsg = "Update for [%s] completed."
	}
	logger.GetLogger("INFO").Printf(lastMsg, configRoot)

	// 本次更新的节点要传给callback
	callbacks.AddUpdatedNodes(itemsHasUpdate...)
	// 结果上报过程比较耗时，为了避免漏掉监听，使用单独的协程进行处理.
	go func(hasUpdateOverall, forceCompile bool, err, cErr, sErr, psErr error) {
		var callbackResults, callbackScriptResults map[string]string
		if hasUpdateOverall || forceCompile {
			// 只执行最近一次的更新,避免频繁回调.
			// 当配置文件更新后，可能需要进行php解析器的reload或者apc缓存清理等回调工作。
			// 通过API获取的配置有更新时一般不需进行post update的回调
			if !getLockOfExecCallback() {
				logger.GetLogger("INFO").Printf("Callback invocation locked by othe routines, skip callback for [%v] update", configRoot)
			} else {
				if forceCompile {
					logger.GetLogger("INFO").Print("Force option enabled, update project config files and invoke callbacks...")
				} else {
					logger.GetLogger("INFO").Printf("Updates of configs contained in schema files detected, begin callbacks...")
				}
			}
			callbackResults = invokeCallbacks()
			callbackScriptResults = invokeCallbackScripts()
		}
		if !getLockOfExecResultReporting() {
			return
		}
		errMsg := []string{}
		for _, msg := range []error{err, cErr, sErr, psErr} {
			if msg != nil {
				errMsg = append(errMsg, msg.Error())
			}
		}
		if err != nil || cErr != nil || sErr != nil || psErr != nil || len(failedConfigItems) > 0 {
			err = fmt.Errorf("Config update has errors: %v :. configs which were not checked from server and may not have the latest values: %s", errMsg, failedConfigItems)
		}
		logger.GetLogger("INFO").Printf("Update of [%v] completed.")
		//logger.GetLogger("INFO").Printf("Sending update results of [%v] to config server...", configRoot)
		// 取消保存结果,减少服务端压力.
		return
		uErr = saveUpdateResults(err, callbackResults, callbackScriptResults)
		if uErr != nil {
			logger.GetLogger("ERROR").Printf("%s", uErr)
		}
	}(hasUpdateOverall, forceCompile, err, cErr, sErr, psErr)
}

// 根据文件类型选择不同的编译方法.
func compile(newValueContainer *map[string][]byte, filePath string) error {
	switch {
	case strings.HasSuffix(filePath, ".schema.toml"):
		callbacks.AddUpdatedSchemafiles(filePath)
		return compileTomlFile(filePath)
	case strings.HasSuffix(filePath, ".schema.php"):
		callbacks.AddUpdatedSchemafiles(filePath)
		return compileSchemaFile(newValueContainer, filePath)
	default:
		return errors.New("Unknown schema file type")
	}
}

// 编译toml文件.
func compileTomlFile(filePath string) error {
	var errMsg []string

	fp, err := os.Open(filePath)
	if err != nil {
		return err
	}

	defer fp.Close()

	var configContent []byte
	var section string

	// 兼容toml schema文件更新的时候，部分配置无法被解析的问题
	// schema文件更新的时候，会检查特定的配置是否已经被watch，在不被watch的情况下才会通过GetNewValueOfConfig更新原始toml数据
	// node是否被watch是根据特定的node prefix是否被监控判断的， 这会导致pro1.a能走到GetNewValueOfConfig, pro1.b无法获取到原始toml数据， 原因在与检查pro1.b的时候，前缀pro1已经被watch
	var dbc *projconfig.DebugConfigs
	if config.GetConfig().DEVMODE {
		dbc, err = projconfig.LoadDebugConfigs()
		if err != nil {
			logger.GetLogger("WARN").Printf("Failed to load debug config file: %s", err)
		}
	}

	configFinder := regexp.MustCompile("\"#{.+?}\"")
	sectionFinder := regexp.MustCompile("^ *\\[.+?\\] *$")
	read := bufio.NewReader(fp)

	for {
		// 读取模板文件的内容
		line, err := read.ReadBytes(byte('\n'))
		if err != nil && len(line) == 0 {
			break
		}

		line = []byte(strings.TrimSpace(string(line)))
		if len(line) == 0 {
			continue
		}

		// section
		if sectionFinder.FindString(string(line)) != "" {
			section = strings.Trim(string(line), "[] ")

			line = append(line, byte('\n'))
			configContent = append(configContent, line...)
			continue
		}

		// 找到了需要替换的配置项
		matched := configFinder.FindAllString(string(line), 1000)
		if matched != nil {
			var lineStr string = string(line)
			for _, nodeName := range matched {
				nodeName = strings.Trim(nodeName, "\"#{}")
				nodeContent, err := projconfig.GetRawToml(nodeName)
				if err != nil {
					// 兼容toml schema文件更新的时候，部分配置无法被解析的问题
					GetNewValueOfConfig(nodeName, dbc, filePath)
					nodeContent, err = projconfig.GetRawToml(nodeName)
					if err != nil {
						errMsg = append(errMsg, fmt.Sprintf("Cannot find value for config[%s].", nodeName))
						continue
					}
				}

				nodeSlice := strings.Split(nodeName, ".")
				nodeLastName := nodeSlice[len(nodeSlice)-1]
				nodeContentSlice := strings.Split(nodeContent, "\n")
				// 保存处理后的配置数据
				nodeData := ""

				// table
				if strings.HasPrefix(nodeContent, "["+nodeLastName+"]") || strings.HasPrefix(nodeContent, "["+nodeLastName+".") {
					for index, row := range nodeContentSlice {
						row = strings.TrimSpace(row)
						if (index == 0 && strings.HasPrefix(nodeContent, "["+nodeLastName+"]")) || row == "" || strings.HasPrefix(row, "#") {
							continue
						}

						contentSection := sectionFinder.FindString(row)
						if contentSection != "" {
							if section == "" {
								nodeData += "[" + strings.Trim(strings.Replace(contentSection, nodeLastName+".", "", 1), "[] ") + "]\n"
							} else {
								nodeData += "[" + strings.Trim(strings.Replace(contentSection, nodeLastName+".", section+".", 1), "[] ") + "]\n"
							}

							continue
						}

						nodeData += row + "\n"
					}
				} else {
					baseMatcher := regexp.MustCompile(nodeLastName + "=(?:(\"[^\n]*\")|([^ #\\[\t\n]+)|(\\[(.|\n|\r)*\\]))")
					configContent := baseMatcher.FindString(nodeContent)
					if configContent == "" {
						// errMsg = append(errMsg, fmt.Sprintf("Invalid configuration content[%s].", nodeName))
						// 从config.debug.toml拿到的值并没有 "node_name=" 这个前缀.
						nodeData = nodeContent
					} else {
						nodeData = strings.Replace(configContent, nodeLastName+"=", "", 1)
					}
				}

				lineStr = strings.Replace(lineStr, "\"#{"+nodeName+"}\"", nodeData, 1000)
			}

			lineStr += "\n"
			configContent = append(configContent, []byte(lineStr)...)
			continue
		}

		// 原样写入到目标文件
		line = append(line, byte('\n'))
		configContent = append(configContent, line...)
	}

	fd, err := os.OpenFile(strings.Replace(filePath, ".schema.", ".", 1), os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0666)
	if err != nil {
		errMsg = append(errMsg, err.Error())
	}

	defer fd.Close()
	fd.Write(configContent)

	if len(errMsg) > 0 {
		return errors.New(strings.Join(errMsg, "|"))
	}

	return nil
}

// 使用最新的配置值编译配置模板，如果新值不存在则使用旧值，否则对应的配置项不被替换，而保持占位符的形式。
// newValueContainer中的item为json字节码
func compileSchemaFile(newValueContainer *map[string][]byte, filePath string) error {
	configItems, err := configschema.ParseItems(filePath)
	if err != nil {
		return err
	}
	fileContent, err := configschema.GetSchemaFileContent(filePath)
	if err != nil {
		return err
	}
	var errMsg []string
	// 调用php进程解析出错时的重试次数.
	var retry int = 2
	for _, configItem := range configItems {
		jsonB, exists := (*newValueContainer)[configItem]
		if !exists {
			jsonB, err = projconfig.GetConfig(configItem, nil, "", true)
			if err != nil {
				errMsg = append(errMsg, fmt.Sprintf("Cannot find value for config[%s].", configItem))
				continue
			}
		}
		jsonV := string(jsonB)
		jsonV = strings.Replace(jsonV, "\\", "\\\\", -1)
		jsonV = strings.Replace(jsonV, "'", "\\'", -1)
		phpContent := "<?php\n"
		phpContent += "ini_set('serialize_precision', 14);$data=var_export(json_decode('" + jsonV + "',true), true);"
		phpContent += "file_put_contents(__FILE__, $data);"
		tempFile, rErr := ioutil.TempFile("", "doveclienttemp")
		if rErr != nil {
			errMsg = append(errMsg, fmt.Sprintf("Failed to create temp file for deconding config content of [%s]: %v", configItem, rErr))
		}
		_, rErr = tempFile.WriteString(phpContent)
		if rErr != nil {
			errMsg = append(errMsg, fmt.Sprintf("Failed to write to temp file for deconding config content of [%s]: %v", configItem, rErr))
			os.Remove(tempFile.Name())
			continue
		}

		// retry: PHP Json decode error: signal: hangup 问题发生的原因暂时不清楚.
		var out bytes.Buffer
		for i := 0; i < retry; i++ {
			cmd := exec.Command("php", tempFile.Name())
			out = bytes.Buffer{}
			cmd.Stdout = &out
			cmd.Stderr = &out
			rErr = cmd.Run()
			if rErr == nil {
				break
			}

			// 最后一次没必要sleep.
			if i != retry-1 {
				time.Sleep(1 * time.Second)
			}
		}

		if rErr != nil {
			errMsg = append(errMsg, fmt.Sprintf("Failed to decode content for [%s]: PHP Json decode error: %s: %s, %s", configItem, rErr, out.String(), jsonV))
			continue
		}

		phpV, rErr := ioutil.ReadFile(tempFile.Name())
		tempFile.Close()
		os.Remove(tempFile.Name())
		if rErr != nil {
			errMsg = append(errMsg, fmt.Sprintf("Failed to read decoded content for [%s] : %v", configItem, rErr))
			continue
		}
		fileContent = bytes.Replace(fileContent, []byte("\"#{"+configItem+"}\""), phpV, -1)
	}
	phpOpenTag := []byte("<?php")
	phpOpentagPos := bytes.Index(fileContent, phpOpenTag)
	var beginingEmpty bool
	if phpOpentagPos >= 0 {
		beginingEmpty = bytes.Index(bytes.TrimSpace(fileContent), phpOpenTag) == 0 && bytes.Index(fileContent, phpOpenTag) > 0
		if !beginingEmpty {
			fileContentTmp := append([]byte{}, fileContent[:phpOpentagPos+5]...)
			fileContentTmp = append(fileContentTmp, configschema.GetCompilationCaution(false)...)
			fileContentTmp = append(fileContentTmp, fileContent[phpOpentagPos+5:]...)
			fileContent = fileContentTmp
		}
	}

	if phpOpentagPos < 0 || beginingEmpty {
		fileContent = append(configschema.GetCompilationCaution(true), fileContent...)
	}
	filePathB := []byte(filePath)
	filePath = string(filePathB[:len(filePathB)-11]) + ".php"
	fd, err := os.OpenFile(filePath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0666)

	if err != nil {
		errMsg = append(errMsg, err.Error())
	} else {
		fd.Chmod(0666)
		_, err = fd.Write(fileContent)
		fd.Close()
		if err != nil {
			errMsg = append(errMsg, err.Error())

		}
	}
	if len(errMsg) > 0 {
		return errors.New(strings.Join(errMsg, "|"))
	}
	return nil
}

func invokeCallbacks() (result map[string]string) {
	callbackSets := config.GetConfig().POST_UPDATE_CALLBACKS
	validCallbacks := callbacks.GetValidCallbacks()
	if len(validCallbacks) > 0 {
		result = make(map[string]string, 2)
	}
	for _, callback := range callbackSets {
		if cb, exists := validCallbacks[callback]; exists {
			logger.GetLogger("INFO").Printf("Run callback[%s]...", callback)
			re, err := cb.Run()
			var errStr string
			if err == nil {
				errStr = ""
			} else {
				errStr = err.Error()
			}
			result[callback] = fmt.Sprintf("%s\n%v", re, errStr)
			logger.GetLogger("INFO").Printf("Result of callback[%s]: %s. %s", callback, re, errStr)
		} else {
			result[callback] = fmt.Sprintf("Invalid callback[%s], available callbacks are[%v], please check your client configs.", callback, validCallbacks)
			continue
		}
	}
	return
}

func invokeCallbackScripts() (result map[string]string) {
	var scripts []string = []string{}
	scripts = append(scripts, config.GetConfig().POST_UPDATE_SCRIPTS...)
	// 被更新的节点也要通知主脚本
	for index, script := range scripts {
		scripts[index] = fmt.Sprintf("%s %s", script, strings.Join(callbacks.GetUpdatedNodes(), ","))
	}

	scripts = append(scripts, callbacks.GetCustomizeCallbacks()...)

	if len(scripts) > 0 {
		result = make(map[string]string, len(scripts))
	}

	for _, script := range scripts {
		logger.GetLogger("INFO").Printf("Run callback script[%s]...", script)
		params := strings.Fields(script)
		cmd := exec.Command(params[0], params[1:]...)
		out := bytes.Buffer{}
		cmd.Stderr = &out
		cmd.Stdout = &out
		err := cmd.Run()
		var errStr string
		if err == nil {
			errStr = ""
		} else {
			errStr = err.Error()
		}
		result[script] = fmt.Sprintf("%v:\n%v\nERR:%v", script, out.String(), errStr)
		logger.GetLogger("INFO").Printf("Result of callback script[%s]: %s. %s", script, out.String(), errStr)
	}
	return
}

// 保存更新结果
func saveUpdateResults(updateErr error, callbackResults, callbackScriptResults map[string]string) error {
	monitorNode, err := rs.MyMonitorNode()
	if err != nil {
		return fmt.Errorf("Failed to get monitor node when save update results: %s", err)
	}

	if exists, _ := zkStorage.Conn.Exists(monitorNode); !exists {
		if err = registerMonitor(); err != nil {
			return fmt.Errorf("Cannot get registeration node: %s", err)
		}
	}
	infoB, err := zkStorage.Conn.Get(monitorNode)
	if err != nil {
		return fmt.Errorf("Cannot get registeration info: %s", err)
	}

	var result map[string]interface{}
	errD := json.Unmarshal(infoB, &result)
	if errD != nil {
		result = make(map[string]interface{}, 10)
		logger.GetLogger("WARN").Printf("Failed to decode old client registeration info:%s", errD)
	}

	result["app_path"] = strings.Join(config.GetConfig().CONFIG_DIR, ":")
	result["config_last_update_time"] = time.Now().Unix()
	if updateErr != nil {
		result["config_last_update_result"] = "not-complete"
		result["config_last_update_error_msg"] = fmt.Sprintf("%v", updateErr)

	} else {
		result["config_last_update_result"] = "complete"
		result["config_last_update_error_msg"] = ""
	}

	result["callback_result"] = callbackResults
	result["callback_script"] = callbackScriptResults
	infoB, err = json.Marshal(result)
	if err != nil {
		err = fmt.Errorf("Failed to encode infomation when update results: %s", err)
	}

	// 继续使用在registerMonitor中创建的租约.
	_, err = zkStorage.Conn.Set(monitorNode, infoB, getMonitorNodeLease())
	if err != nil {
		err = fmt.Errorf("Failed to save update results: %s", err)
	}
	return err
}
