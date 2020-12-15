// 项目配置管理相关操作
package projconfig

import (
	"doveclient/config"
	"doveclient/encrypt"
	"doveclient/logger"
	"doveclient/storage"
	diskStorage "doveclient/storage/leveldb"
	remoteStorage "doveclient/storage/zk"
	"doveclient/util"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

var encryptor *encrypt.Encryptor

// 从服务器获取同一个配置的最小间隔时间
const MIN_INTTERVAL_OF_FETCH_FROM_SERVER = int64(1)

// 当能够从chan中写入数据(int8(0))时，表示其它协程已经获取过数据.
// 当lastAccessTime与当前时间差大于一定值时，表示可以重新从配置服务器获取。
type serverAccessStatusOfConfigItem struct {
	stateChan      chan int8
	lastUpdateTime int64
}

// 从服务器获取同一个配置项的队列表.
var queuedItemsTeBeFetchedFromConfigServer = map[string]*serverAccessStatusOfConfigItem{}

func getLocalStorage() (db *diskStorage.Db, err error) {
	if !diskStorage.IsIntialized() {
		diskStorage.InitDb(diskStorage.Config{Dsn: config.GetLocalStorageDir()})
	}
	db, err = diskStorage.GetDb()
	return
}

// queuedItemsTeBeFetchedFromConfigServer 读写锁
var lockOfQueuedSrvCh = make(chan int8, 1)

// 对于同一个配置，至允许发起一个到服务器端的请求.(针对queuedItemsTeBeFetchedFromConfigServer)
// 返回１表示还未有协程在向服务器获取数据(或者需要重新从服务器端获取),并需要在获取和保存到本地成功后调用releaseLockOfSrv
// 返回0 表示其它协程已经向服务器获取过数据，无需再次获取从服务器获取
func getLockOfSrv(configName string) (fetchState int8) {
	lockOfQueuedSrvCh <- int8(1)
	// 如果已有其它协程在获取配置，则尝试等待
	if info, exists := queuedItemsTeBeFetchedFromConfigServer[configName]; exists {
		<-lockOfQueuedSrvCh
		fetchState = <-info.stateChan
		// 已过期，过期则返回１，需重新从服务器获取，之后再释放锁(releaseLockOfSrv)。
		if time.Now().Unix()-info.lastUpdateTime > MIN_INTTERVAL_OF_FETCH_FROM_SERVER {
			fetchState = int8(1)
		} else {
			// 未过期，告诉其它协程也不必从服务器获取.
			info.stateChan <- int8(0)
		}
	} else {
		queuedItemsTeBeFetchedFromConfigServer[configName] = &serverAccessStatusOfConfigItem{make(chan int8, 1), time.Now().Unix()}
		<-lockOfQueuedSrvCh
		fetchState = 1
	}

	return
}

func releaseLockOfSrv(configName string) {
	lockOfQueuedSrvCh <- int8(1)
	info := queuedItemsTeBeFetchedFromConfigServer[configName]
	info.lastUpdateTime = time.Now().Unix() + int64(3)
	info.stateChan <- int8(0)
	<-lockOfQueuedSrvCh
}

func GetEncryptor() *encrypt.Encryptor {
	if encryptor == nil {
		encryptor = &(encrypt.Encryptor{IV: []byte(config.GetConfig().ENCRYPT_IV), Key: []byte(config.GetConfig().ENCRYPT_KEY)})
	}
	return encryptor
}

// 获取本地存储的明文TOML格式,
func GetFromDisk(name string) (re []byte, err error) {
	db, err := getLocalStorage()
	if err != nil {
		return
	}
	re, err = db.Conn.Get([]byte(name), nil)
	if err == nil && config.GetConfig().ENCRYPT_ENABLE {
		encryptor := GetEncryptor()
		re, err = encryptor.Decrypt(re)
		if err != nil {
			re = nil
			err = errors.New(fmt.Sprintf("Failed to decrypt config[%s]: %s", name, err))
			return
		}
	} else if err != nil {
		err = errors.New(fmt.Sprintf("Failed to read config [%s] from db: %s", name, err))
	}
	return
}

// 将配置保存至本地磁盘。
// content: 明文的toml
func SaveToDisk(name string, content []byte) (err error) {
	db, err := getLocalStorage()
	if err != nil {
		return
	}
	defer func() {
		//db.Conn.Close()
	}()
	if config.GetConfig().ENCRYPT_ENABLE {
		encryptor := GetEncryptor()
		content, err = encryptor.Encrypt(content)
		if err != nil {
			err = errors.New(fmt.Sprintf("Failed to enctypt config[%s] when saving to disk: %s", name, err))
			return
		}
	}
	err = db.Conn.Put([]byte(name), content, nil)
	return
}

// 从服务端获取明文的toml
func GetFromServer(name string) (re []byte, err error) {
	rsEngine, err := remoteStorage.GetConn(nil)
	if err != nil {
		err = errors.New(fmt.Sprint("Failed to get remote storage engine: %s", err))
		return
	}
	defer func() {
		rsEngine.Conn.Close()
	}()
	path := remoteStorage.CN2P(name)
	rawContent, err := rsEngine.Conn.Get(path)
	if err != nil {
		return
	}

	var rawConfig map[string]interface{}
	err = json.Unmarshal(rawContent, &rawConfig)
	if err != nil {
		err = errors.New(fmt.Sprintf("Failed to decode config content from config server: %s", err))
	} else {
		metadata := rawConfig["metadata"].(map[string]interface{})
		// 旧版本无加密特性
		if metadata["encrypted"] == nil {
			metadata["encrypted"] = 0
		} else {
			metadata["encrypted"] = int(metadata["encrypted"].(float64))
		}
		metadata["state"] = int(metadata["state"].(float64))

		if metadata["state"] != 1 {
			err = errors.New(fmt.Sprintf("config[%s] is marked as un-published on config server. Skipped this version of the content", name))
		} else if metadata["encrypted"] == 1 {
			// 加密设置的版本。 当更换加密的key之后，版本号应当进行匹配。如果没有版本号，则表示加密的key从未被修改过。
			if metadata["encrypt_ver"] != nil && config.GetConfig().ENCRYPT_VER != metadata["encrypt_ver"] {
				err = errors.New(fmt.Sprintf("config [%s] is an encrypted version on config server, but your encryption settings is ver(%f)  while the config server is ver(%f). ", name, config.GetConfig().ENCRYPT_VER, metadata["encrypt_ver"]))
			} else {
				re, err = base64.StdEncoding.DecodeString(rawConfig["content"].(string))
				if err != nil {
					err = errors.New(fmt.Sprintf("Failed to decode content of [%s] config from server: %s", name, err))
				} else {
					encryptor := GetEncryptor()
					re, err = encryptor.Decrypt(re)
					if err != nil {
						err = errors.New(fmt.Sprintf("Failed to decrypt of [%s] config from server: %s", name, err))
						re = nil
					}
				}
			}
		} else {
			switch rawConfig["content"].(type) {
			case nil:
				re = []byte("")
			case string:
				re = []byte(rawConfig["content"].(string))
			default:
				err = errors.New(fmt.Sprintf("config content of [%v] on config server is broken: %v", name, rawConfig["content"]))
			}
		}
	}
	return
}

func GetFromMem(name string) []byte {
	return storage.GetFromMem(name)
}

// 将配置保存至内存.
// data: json字节码
func StoreToMem(name string, data []byte) {
	storage.StoreToMem(name, data)
}

func SaveRawToml(name, value string) {
	storage.SaveRawConfig(name, value)
}

func GetRawToml(name string) (string, error) {
	return storage.GetRawConfig(name)
}

// 通过各种方式获取配置。
// 优先顺序：(debug文件>>) 内存 >> 本地磁盘 >> 配置服务器 (如果为DEV环境(当dbc不为nil时),则debug文件中的配置有最高优先级)
// 如果在配置服务器上获取到，则将值保存到内存及本地磁盘
// lockSrvCon 是否禁止并发地从服务器取同一个配置
// RETURN: re josn字节码
func GetConfig(name string, dbc *DebugConfigs, schemafilePath string, lockSrvCocur bool) (re []byte, err error) {
	if dbc != nil {
		re, _ = dbc.GetConfigBySchemafile(name, schemafilePath, false)
		if re != nil {
			return
		}
	}
	
	re = GetFromMem(name)
	if re != nil {
		return
	}

	if lockSrvCocur {
		lastFetchLockState := getLockOfSrv(name)
		// 已有其它协程在从服务器端获取, 则尝试再次从内存获取
		if lastFetchLockState == int8(0) {
			re = GetFromMem(name)
			if re == nil {
				err = errors.New(fmt.Sprintf("Failed to get config[%s] by any means after a waiting!", name))
			}
			return
		}
		defer releaseLockOfSrv(name)
	}

	re, err = GetFromDisk(name)
	if err != nil {
		logger.GetLogger("INFO").Printf("Failed to get config [%s] from local storage: %s, now try to fetch from config server...\n", name, err)
		re, err = GetFromServer(name)
		if err == nil {
			logger.GetLogger("INFO").Printf("Successfully fetched config [%s] from config server...Saving to disk...\n", name)
			sErr := SaveToDisk(name, re)
			if sErr != nil {
				logger.GetLogger("ERROR").Printf("Failed to save config[%s] to disk: %s", name, sErr)
			}
		} else {
			logger.GetLogger("ERROR").Printf("Failed to fetch config [%s] from config server: %s\n", name, err)
		}
	} else {
		logger.GetLogger("INFO").Printf("Successfully recovered config [%s] from disk.\n", name)
	}
	if err == nil {
		re, err = util.TomlConfigValueToJson(string(re))
		if err != nil {
			logger.GetLogger("ERROR").Printf("Failed to decode config[%s] from toml: %s\n", name, err)
		} else {
			StoreToMem(name, re)
		}
	} else {
		err = errors.New(fmt.Sprintf("Failed to get config[%s] by any means!", name))
	}
	return
}
