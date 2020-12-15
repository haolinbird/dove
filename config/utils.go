package config

import (
	"bytes"
	"doveclient/util"
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"reflect"
	"runtime"
	"strconv"
	"strings"

	toml "git.oschina.net/chaos.su/go-toml"

	"encoding/json"
	"crypto/md5"
	"net/http"
	"time"

	"doveclient/encrypt"
	"encoding/base64"
	"net"
)

var secretConfig *toml.TomlTree

func GetLocalIp() string {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return "Unknown";
	}

	for _, address := range addrs {
		// 检查ip地址判断是否回环地址
		if ipnet, ok := address.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
			if ipnet.IP.To4() != nil {
				return ipnet.IP.String()
			}

		}
	}

	return "Unknown";
}

// 如果希望使用的环境没有被编译在dc中,那么会调用此函数从远程获取.
func initZkConfigByServer() (*EnvInfo, error) {
	if config.DOVE_SERVER == "" {
		return nil, errors.New("Unconfigured address(DOVE_SERVER)")
	}

	var env string = strings.ToLower(strings.Replace(config.ENV, "-", "", -1));
	var timestamp string = strconv.FormatInt(time.Now().Unix(), 10)
	var ip = GetLocalIp();
	var sign string = fmt.Sprintf("%x", md5.Sum([]byte(ip + env + timestamp + secretConfig.Get("Env.AuthKey").(string))))

	url := strings.TrimRight(config.DOVE_SERVER, "/") + "/Api/Env?env=" + env + "&timestamp=" +
		timestamp + "&sign=" + sign + "&ip=" + ip

	res, err := http.Get(url)
	defer res.Body.Close()
	if err != nil {
		return nil, err
	}

	response, err := ioutil.ReadAll(res.Body)
	if err != nil {
		return nil, err
	}

	var result struct {
		Status string
		Data string
		Message string
	}

	err = json.Unmarshal(response, &result)
	if err != nil {
		return nil, err
	}

	if result.Status != "ok" {
		return nil, errors.New(result.Message)
	}

	decData, err := base64.StdEncoding.DecodeString(result.Data)
	if err != nil {
		return nil, err
	}

	encryptor := &(encrypt.Encryptor{IV: []byte(secretConfig.Get("Env.AuthIv").(string)), Key: []byte(secretConfig.Get("Env.AuthKey").(string))})
	plaintext, err := encryptor.Decrypt([]byte(decData))
	if err != nil {
		return nil, err
	}

	var data EnvInfo
	err = json.Unmarshal(plaintext, &data)
	if err != nil {
		return nil, err
	}

	return &data, nil
}

func initZkConfig(envInfo *EnvInfo) {
	zkHosts := map[string][]string{}
	zkConfig = &map[string]interface{}{}
	if envInfo == nil {
		hostsRaw, _ := util.TomlToMap(secretConfig.Get("Zookeeper.host").(*toml.TomlTree).ToString())
		for env, host := range hostsRaw {
			zkHosts[env] = strings.Split(host.(string), ",")
		}
	} else {
		zkHosts[config.ENV] = strings.Split(envInfo.Hosts, ",")
	}
	(*zkConfig)["hosts"] = zkHosts
	(*zkConfig)["root"] = secretConfig.Get("Zookeeper.root").(string)
}

func GetConfig() Config {
	if config == nil {
		panic("config not intialized, you should call config.Setup() first!")
	}
	return *config
}

func GetZkConfig() map[string]interface{} {
	if zkConfig == nil {
		panic("config not intialized, you should call config.Setup() first!")
	}
	return *zkConfig
}

func GetLocalStorageDir() string {
	var configStoreDir string
	if GetConfig().ENCRYPT_ENABLE {
		configStoreDir = "/configs_crypt/"
	} else {
		configStoreDir = "/configs/"
	}
	return GetConfig().DATA_DIR + configStoreDir
}

func LookupOsUidGid(name string) (uid int, gid int, err error) {
	cmd := exec.Command("id", name)
	o := bytes.Buffer{}
	cmd.Stderr = &o
	cmd.Stdout = &o
	err = cmd.Run()
	if err != nil {
		err = errors.New(fmt.Sprintf("Failed to get uid and gid for [%s]: %s:%s", name, err, o.String()))
	} else {
		outstring := strings.Fields(o.String())[:2]
		uid, err = strconv.Atoi(string([]byte(outstring[1])[4:strings.Index(outstring[0], "(")]))
		if err != nil {
			err = errors.New(fmt.Sprintf("Failed to get uid for[%s]: %s", name, err))
		}
		gid, err = strconv.Atoi(string([]byte(outstring[1])[4:strings.Index(outstring[1], "(")]))
		if err != nil {
			err = errors.New(fmt.Sprintf("Failed to get gid for[%s]: %s", name, err))
		}
	}
	return
}

func ParseSecretSettings() (*toml.TomlTree, error) {
	t, err := toml.Load(string(DecodeSetting()))
	if err != nil {
		err = errors.New(fmt.Sprintf("Failed to load secret settings. You should fix configs in [settings.pack.toml.go] before compile the client! Error: %s", err))
	}
	if err != nil {
		t = nil
	}
	return t, err
}


// POST_UPDATE_SCRIPTS_RULE
func ParsePostUpdateRule(ruleFile string) ([][]string, error) {
	t, err := toml.LoadFile(ruleFile)
	if err != nil {
		return nil, err
	}

	var result [][]string = [][]string{}
	rules := t.Get("rules").(*toml.TomlTree)
	for _, key := range rules.Keys() {
		v, ok := rules.Get(key).([]interface{})
		if ok {
			var row []string = []string{}
			for _, item := range v {
				row = append(row, item.(string))
			}

			result = append(result, row)
		}
	}

	return result, err
}

// 解析配置文件/etc/DoveClient.conf
func ParseExtraConfig() (*Config, error) {
	var err error
	if configFile == "" {
		if runtime.GOOS == "windows" {
			clientPath, err := filepath.Abs(os.Args[0])
			if err != nil {
				return nil, errors.New(fmt.Sprintf("Failed to get client ABS path: %v", err))
			}
			configFile = path.Dir(clientPath) + string(os.PathSeparator) + "config.ini"
		} else {
			configFile = "/etc/DoveClient.conf"
		}
	}
	configStringByte, err := ioutil.ReadFile(configFile)
	if err != nil {
		return nil, err
	}

	configString := strings.Trim(string(configStringByte), " \n\r\n")
	configLines := strings.Split(configString, "\n")
	parsedConfigs := *config
	cr := reflect.ValueOf(&parsedConfigs)
	for lineNo, line := range configLines {
		if strings.Index(line, "#") == 0 {
			continue
		}
		kv := strings.SplitN(line, "=", 2)
		if len(kv) < 2 {
			return nil, errors.New(fmt.Sprintf("Failed to parse config file '%s' on line %d", configFile, lineNo))
		}
		kv[0] = strings.Trim(kv[0], " ")
		kv[1] = strings.Trim(kv[1], " \"")

		if cr.Elem().FieldByName(kv[0]).IsValid() {
			if kv[0] == "CONFIG_DIR" || kv[0] == "POST_UPDATE_CALLBACKS" || kv[0] == "POST_UPDATE_SCRIPTS" {
				cr.Elem().FieldByName(kv[0]).Set(reflect.ValueOf(util.StringSplit(kv[1], ",")))

			} else if kv[0] == "ENCRYPT_ENABLE" {
				kv[1] = strings.ToLower(kv[1])
				cr.Elem().FieldByName(kv[0]).SetBool(kv[1] == "1" || kv[1] == "true")
			} else if kv[0] == "ENCRYPT_VER" {
				encVer, err := strconv.ParseFloat(kv[1], 64)
				if err != nil {
					return nil, errors.New(fmt.Sprintf("Failed to parse ENCRYPT_VER: %s", err))
				} else {
					cr.Elem().FieldByName(kv[0]).SetFloat(encVer)
				}
			} else {
				cr.Elem().FieldByName(kv[0]).SetString(kv[1])
			}
		}
	}
	return &parsedConfigs, nil
}

// 解码编码后的配置
func DecodeSetting() []byte {
	settings := make([]byte, len(SecretSettings))
	for i, _ := range SecretSettings {
		settings[i] = ^SecretSettings[i]
	}
	return settings
}

// RChmod 递归地修改指定目录及其文件的mode
func Rchmod(dir string, mode os.FileMode) error {
	err := filepath.Walk(dir, filepath.WalkFunc(func(path string, info os.FileInfo, err error) error {
		return os.Chmod(path, mode)
	}))
	if err == nil {
		err = os.Chmod(dir, mode)
	}
	return err
}

// RChown 递归地修改指定目录及其文件的所有者
func Rchown(dir string, uid int, gid int) error {
	if runtime.GOOS == "windows" {
		return nil
	}
	err := filepath.Walk(dir, filepath.WalkFunc(func(path string, info os.FileInfo, err error) error {
		return os.Chown(path, uid, gid)
	}))
	if err == nil {
		err = os.Chown(dir, uid, gid)
	}
	return err
}
