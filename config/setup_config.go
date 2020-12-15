// 客户端运行所需的相关配置ke
package config

import (
	"doveclient/encrypt"
	"doveclient/util"
	"errors"
	"fmt"
	"os"
	"path"
	"reflect"
	"runtime"
	"strings"
	"time"
	"log"

	toml "git.oschina.net/chaos.su/go-toml"
)

const (
	MAX_CONNECTIONS = 15000
	// 客户端响应超时
	CLIENT_TIMEOUT  = time.Second * 5
	CLIENT_MAX_IDLE = time.Second * 120
	// 请求处理超时
	PROCESS_TIMEOUT = int64(10)
)

var configFile string

type Config struct {
	ListenAddr       string
	ListenAddrTransi string
	RunAsBi          bool
	InstallDir       string
	PidFile          string

	DATA_ROOT             string
	DATA_DIR              string
	LOG_DIR               string
	CONFIG_DIR            []string
	DAEMON_USER           string
	DAEMON_UID            int
	DAEMON_GID            int
	POST_UPDATE_CALLBACKS []string
	POST_UPDATE_SCRIPTS   []string
	ENV                   string
	ClientVer             string
	ENCRYPT_ENABLE        bool
	ENCRYPT_KEY           string
	ENCRYPT_IV            string
	ENCRYPT_VER           float64
	DEVMODE               bool
	DEBUG_FILE            string

	ETCD_CONNECTION_TIMEOUT      int64
	ETCD_DIAL_KEEP_ALIVE_TIME    int64
	ETCD_DIAL_KEEP_ALIVE_TIMEOUT int64

	ETCD_HEARTBEAT_KEY        string
	ETCD_REQUEST_TIMEOUT      int64
	ETCD_MIN_RETRY_INTERVAL   int64
	ETCD_RETRY_INTERVAL_RANGE int64
	ETCD_AUTO_SYNC_INTERVAL   int64

	POST_UPDATE_SCRIPTS_RULE string
	PostUpdateRule           [][]string

	// 性能分析的侦听设置(未设置则不侦听).
	PPROF_LISTEN string
	// 当一个环境没有编译在dc中时,需要从远程服务器获取其信息.
	DOVE_SERVER string
}

// 远程服务器返回的环境信息.
type EnvInfo struct {
	Hosts string
	Key   string
	Iv    string
}

var config = &Config{}

//zk的配置暂单独设置，不由运维修改
var zkConfig *map[string]interface{}

var debug bool

//	TODO: not coroutine safe
func DebugEnabled() bool {
	return debug
}

//	TODO: not coroutine safe
func SetDebug(enable bool) {
	debug = enable
}

func SetConfigFile(f string) {
	configFile = f
}

//	TODO: not coroutine safe
func SetConfig(name string, val interface{}) {
	reflect.ValueOf(config).Elem().FieldByName(name).Set(reflect.ValueOf(val))
}

func GetBiPidFile() string {
	return config.DATA_ROOT + "bi.pid"
}
func Setup() error {
	var err error
	// 安全相关的配置文件
	secretConfig, err = ParseSecretSettings()
	if err != nil {
		return errors.New(fmt.Sprintf("Failed to parse secret configs: %s ", err))
	}

	// 获取并解析配置文件/etc/DoveClient.conf,其中的配置具有优先级
	config, err = ParseExtraConfig()
	if err != nil {
		return errors.New(fmt.Sprintf("Failed to initialize configs: %s ", err))
	}

	// 更新特定的schema文件后，需要调用特定的脚本执行配置更新后的回调操作
	if config.POST_UPDATE_SCRIPTS_RULE != "" {
		config.PostUpdateRule, err = ParsePostUpdateRule(config.POST_UPDATE_SCRIPTS_RULE)
		if err != nil {
			return errors.New(fmt.Sprintf("Callback configuration error: %s", err))
		}
	}

	config.ClientVer = secretConfig.Get("Ver").(string)

	var envInfo *EnvInfo = nil
	// 检查配置文件里设置的环境标识是否正确
	if err = func() error {
		var err error = nil

		// 需要从服务器获取环境信息.
		envInfo, err = initZkConfigByServer()
		if err == nil {
			return nil
		}

		err = nil

		// 说明环境信息是编译在dc中.
		if secretConfig.Get("Zookeeper.host."+config.ENV) == nil {
			err = fmt.Errorf("Zookeeper.host." + config.ENV + "not found.")
		}
		if err == nil && secretConfig.Get("Encrypt."+config.ENV) == nil {
			err = errors.New("Encrypt." + config.ENV + "not found.")
		}

		return err
	}(); err != nil {
		envs := secretConfig.Get("Zookeeper.host").(*toml.TomlTree).Keys()
		return fmt.Errorf("env [%s] has not been suppored: %v, valid envs are %+v", config.ENV, err, envs)
	}
	config.DEVMODE = strings.Index(config.ENV, "DEV") == 0

	var daemonUid, daemonGid int
	if config.DAEMON_USER == "" {
		daemonGid = os.Getegid()
		daemonUid = os.Getuid()
	} else {
		daemonUid, daemonGid, err = LookupOsUidGid(config.DAEMON_USER)
	}

	if err != nil {
		return err
	}

	config.DAEMON_UID = daemonUid
	config.DAEMON_GID = daemonGid

	// 默认配置检测及设置
	switch runtime.GOOS {
	case "linux", "darwin":
		if config.DATA_DIR == "" {
			config.DATA_ROOT = "/var/lib/doveclient/"
		}

	case "windows":
		if config.DATA_DIR == "" {
			config.DATA_ROOT = os.Getenv("APPDATA") + "\\doveclient\\"
		}
	}

	config.DATA_ROOT = strings.TrimRight(config.DATA_DIR, string(os.PathSeparator)) + string(os.PathSeparator)

	if !util.FileExists(config.DATA_ROOT) {
		err = os.MkdirAll(config.DATA_ROOT, 0755)
		if err != nil {
			return errors.New(fmt.Sprintf("Failed to initialize data root dir[%s]: %s", config.DATA_ROOT, err))
		}
	}
	err = Rchown(config.DATA_ROOT, daemonUid, daemonGid)

	if err != nil {
		log.Printf("Failed to change data root dir owner: %s", err)
	}

	if config.ListenAddr == "" {
		switch runtime.GOOS {
		case "windows":
			config.ListenAddr = "127.0.0.1:4510"
			config.ListenAddrTransi = "127.0.0.1:4511"
		default:
			config.ListenAddr = config.DATA_DIR + "doveclient.sock"
			config.ListenAddrTransi = config.DATA_DIR + "doveclient.transi.sock"
		}
	}

	config.InstallDir = path.Dir(os.Args[0]) + string(os.PathListSeparator)
	if !config.RunAsBi {
		config.PidFile = config.DATA_ROOT + "pid"
	} else {
		config.PidFile = GetBiPidFile()
	}
	config.DATA_DIR = config.DATA_ROOT + string(os.PathSeparator) + config.ENV + string(os.PathSeparator)

	if envInfo != nil {
		config.ENCRYPT_IV = envInfo.Iv
		config.ENCRYPT_KEY = envInfo.Key
	} else {
		config.ENCRYPT_IV = secretConfig.Get("Encrypt." + config.ENV + ".iv").(string)
		config.ENCRYPT_KEY = secretConfig.Get("Encrypt." + config.ENV + ".key").(string)
	}

	if config.ENCRYPT_VER == 0 {
		config.ENCRYPT_VER = secretConfig.Get("Encrypt.Ver").(float64)
	}

	if len(config.ENCRYPT_IV) != encrypt.ENC_IV_SIZE {
		return errors.New(fmt.Sprintf("Config encryption enabled, but ENCRYPT_IV  defined in config is not valid. Expecting a %d-byte IV, but %d bytes detected!", encrypt.ENC_IV_SIZE, len(config.ENCRYPT_IV)))
	}

	if len(config.ENCRYPT_KEY) < 1 {
		return errors.New("Config encryption enabled, but ENCRYPT_KEY length is shorter than 1")
	}
	if !util.FileExists(config.DATA_DIR) {
		err = os.MkdirAll(config.DATA_DIR, 0755)
		if err != nil {
			return errors.New(fmt.Sprintf("Failed to initialize data dir[%s]: %s", config.DATA_DIR, err))
		}
	}

	err = Rchown(config.DATA_DIR, daemonUid, daemonGid)

	if err != nil {
		log.Printf("Failed to change data dir owner: %s", err)
	}

	if !util.FileExists(config.LOG_DIR) {
		err = os.MkdirAll(config.LOG_DIR, 0755)
		if err != nil {
			log.Printf("Failed to initialize log dir[%s]: %s", config.LOG_DIR, err)
		}
	}

	// 从环境变量中获取配置并覆盖文件配置.
	cfgRef := reflect.TypeOf(config)
	var i int = 0
	var key string
	for i = 0; i < cfgRef.Elem().NumField(); i++ {
		key = "DOVE_" + cfgRef.Elem().Field(i).Name
		if os.Getenv(key) != "" {
			switch cfgRef.Elem().Field(i).Type.String() {
			case "[]string":
				SetConfig(cfgRef.Elem().Field(i).Name, strings.Split(os.Getenv(key), ","))
			case "[][]string":
			default:
				SetConfig(cfgRef.Elem().Field(i).Name, os.Getenv(key))
			}
		}
	}

	// 如果用户指定使用特定的debug文件，则指定debug文件所在的目录也必须加入监听列表(env: DOVE_DEBUG_FILE).
	// DOVE_DEBUG_FILE 对任何环境都生效.
	if config.DEBUG_FILE != "" && util.FileExists(config.DEBUG_FILE) {
		lastpos := strings.LastIndex(config.DEBUG_FILE, string(os.PathSeparator))
		if lastpos != -1 {
			config.CONFIG_DIR = append(config.CONFIG_DIR, config.DEBUG_FILE[0:lastpos])
			config.DEVMODE = true
		}
	}

	err = Rchown(config.LOG_DIR, daemonUid, daemonGid)
	if err != nil {
		log.Printf("Failed to change log dir owner: %s", err)
	}
	initZkConfig(envInfo)

	// etcd
	config.ETCD_CONNECTION_TIMEOUT = secretConfig.Get("Etcd.CONNECTION_TIMEOUT").(int64)
	config.ETCD_DIAL_KEEP_ALIVE_TIME = secretConfig.Get("Etcd.DIAL_KEEP_ALIVE_TIME").(int64)
	config.ETCD_DIAL_KEEP_ALIVE_TIMEOUT = secretConfig.Get("Etcd.DIAL_KEEP_ALIVE_TIMEOUT").(int64)

	config.ETCD_HEARTBEAT_KEY = secretConfig.Get("Etcd.HEARTBEAT_KEY").(string)
	config.ETCD_REQUEST_TIMEOUT = secretConfig.Get("Etcd.REQUEST_TIMEOUT").(int64)
	config.ETCD_MIN_RETRY_INTERVAL = secretConfig.Get("Etcd.MIN_RETRY_INTERVAL").(int64)
	config.ETCD_RETRY_INTERVAL_RANGE = secretConfig.Get("Etcd.RETRY_INTERVAL_RANGE").(int64)
	config.ETCD_AUTO_SYNC_INTERVAL = secretConfig.Get("Etcd.AUTO_SYNC_INTERVAL").(int64)

	return nil
}
