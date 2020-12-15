package callbacks

import (
	"bytes"
	"doveclient/callbacks/fcgiclient"
	"doveclient/config"
	"errors"
	"fmt"
	"io/ioutil"
	"net"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"time"
	"doveclient/util"
)

type CallbackInterface interface {
	Run() (result []byte, err error)
}

type CbClearApc struct {
}

type CbReloadPHPServer struct {
}

type FcgiHost struct {
	Net  string
	Addr string
	Port int
}

var ReloadPHPServer = CbReloadPHPServer{}
var ClearApc = CbClearApc{}

var validCallbacks = map[string]CallbackInterface{"ClearApc": ClearApc, "ReloadPHPServer": ReloadPHPServer}

var apcClearScriptFile string

var fpmHost FcgiHost

func GetValidCallbacks() map[string]CallbackInterface {
	return validCallbacks
}

func InitCallbacks() (err error) {
	return initClearApc()
}
func initClearApc() (err error) {
	var cbIsSet bool
	// 只有定义了要使用ClearApc回调时才进行初始化
	for _, cb := range config.GetConfig().POST_UPDATE_CALLBACKS {
		if cb == "ClearApc" {
			cbIsSet = true
		}
	}
	if !cbIsSet {
		return
	}

	fileContent := []byte("<?php\nerror_reporting(E_ERROR);echo json_encode(function_exists('apc_clear_cache') ? apc_clear_cache() : True);")
	apcClearScriptFile = config.GetConfig().DATA_DIR + "apc_clear.php"
	err = ioutil.WriteFile(apcClearScriptFile, fileContent, 0666)
	if err == nil {
		fpmHost, err = GetFpmHost()
	}
	if err != nil {
		err = errors.New(fmt.Sprintf("Cannot initialize callback 'ClearApc': %s", err))
	}
	return
}

func (cb CbClearApc) Run() (result []byte, err error) {
	if fpmHost.Addr == "" {
		err = errors.New("Could not get Fpm Addr!")
		return
	}
	env := make(map[string]string)
	env["REQUEST_METHOD"] = "GET"
	env["SCRIPT_FILENAME"] = apcClearScriptFile
	env["REMOTE_ADDR"] = "127.0.0.1"
	env["SERVER_PROTOCOL"] = "HTTP/1.1"
	var fcgi *fcgiclient.FCGIClient
	switch fpmHost.Net {
	case "tcp":
		fcgi, err = fcgiclient.New(fpmHost.Addr, fpmHost.Port)
	case "unix":
		fcgi, err = fcgiclient.New("unix", fpmHost.Addr)
	}
	if err != nil {
		err = errors.New(fmt.Sprintf("Could not connect to fastcgi: %s.", err))
		return
	}
	resultRaw, err := fcgi.Request(env, "")
	if err != nil {
		err = errors.New(fmt.Sprintf("Fastcgi request: %s.", err))
		return
	} else {
		rLen := len(resultRaw)
		// 正常的返回
		if rLen > 12 && strings.ToLower(string(resultRaw[0:12])) == strings.ToLower("content-type") {
			start := 0
			sepCount := 0
			for ; start < rLen; start += 1 {
				if resultRaw[start] == '\n' {
					sepCount += 1
				}
				if sepCount >= 2 {
					break
				}
			}
			if start+1 < rLen {
				result = resultRaw[start+1:]
			} else {
				result = []byte{}
			}
		} else {
			result = resultRaw
		}

	}
	return
}

// 暂时只支持linux
func GetFpmHost() (fcgiHost FcgiHost, err error) {
	out := bytes.Buffer{}
	var cmdStr []string
	switch runtime.GOOS {
	case "linux":
		cmdStr = []string{"bash", "-c", "netstat -nlp | grep php-fpm"}
	default:
		err = errors.New("getFpmHost supports 'linux' only!")
		return
	}
	cmd := exec.Command(cmdStr[0], cmdStr[1:]...)
	cmd.Stderr = &out
	cmd.Stdout = &out
	err = cmd.Run()
	if err != nil {
		err = errors.New(fmt.Sprintf("Failed to detect php-fpm host info: %s : %s", err, out.String()))
		return
	}
	lines := strings.Split(out.String(), "\n")
	if len(lines) < 1 {
		err = errors.New("No php-fpm detected!")
		return
	}
	lines = strings.Fields(lines[0])
	fcgiHost = FcgiHost{Net: lines[0]}
	switch fcgiHost.Net {
	case "tcp":
		hostPorts := strings.Split(lines[3], ":")
		fcgiHost.Addr = hostPorts[0]
		fcgiHost.Port, _ = strconv.Atoi(hostPorts[1])
	case "unix":
		fcgiHost.Addr = lines[9]
	}
	return fcgiHost, nil
}

func (cb CbReloadPHPServer) Run() (result []byte, err error) {
	conn, err := net.Dial("tcp", "127.0.0.1:10101")
	if err != nil {
		err = errors.New(fmt.Sprintf("Failed to connect to phpserver : %s", err))
		return
	}
	conn.SetWriteDeadline(time.Now().Add(time.Second * 15))
	_, err = conn.Write([]byte("reload"))
	if err != nil {
		err = errors.New(fmt.Sprintf("Failed to execute 'reload' command on phpserver: %s", err))
		return
	}
	result = make([]byte, 500)
	start := 0
	var n int
	for start < 500 {
		conn.SetReadDeadline(time.Now().Add(time.Second * 5))
		n, err = conn.Read(result[start:])
		start += n
		if err != nil {
			if len(result) > 0 {
				err = nil
			}
			break
		}
	}
	conn.SetWriteDeadline(time.Now().Add(time.Second * 1))
	_, _ = conn.Write([]byte("quit"))
	conn.Close()
	result = bytes.TrimRight(result, string(byte(0)))
	return
}

// 被更新的配置文件
var updatedSchemaFiles []string = []string{}
// 被更新的节点
var updatedNodes []string = []string{}

var lk chan int = make(chan int, 1)

// 被更新的配置文件(不同的配置文件被更新需要调用不同的shell脚本)
func AddUpdatedSchemafiles(file string) {
	lk<-1
	defer func() {
		<-lk
	}()

	if ! util.InArrayString(&updatedSchemaFiles, file) {
		updatedSchemaFiles = append(updatedSchemaFiles, file)
	}
}

// 被更新的节点(被更新的node会作为参数传递给脚本)
func AddUpdatedNodes(nodes ...string) {
	if len(nodes) == 0 {
		return;
	}

	lk<-1
	defer func() {
		<-lk
	}()

	for _, node := range nodes {
		if ! util.InArrayString(&updatedNodes, node) {
			updatedNodes = append(updatedNodes, node)
		}
	}
}

func GetUpdatedNodes() []string {
	return updatedNodes
}

// 得到回调脚本(用户自己定义使用什么脚本处理什么配置文件的更新)
func GetCustomizeCallbacks() []string {
	defer func() {
		updatedSchemaFiles = []string{}
		updatedNodes = []string{}
	}()

	var scripts []string = []string{}

	// rule的结构： ["脚本文件路径"]
	// rule的结构： ["脚本文件路径", "schema文件1"， "schema文件2"，"schema文件3"]
 	for _, rule := range config.GetConfig().PostUpdateRule {
 		if len(rule) == 1 {
 			// 如果没有指定脚本对应的schema文件，则该脚本默认会执行
 			if ! util.InArrayString(&scripts, rule[0]) {
 				scripts = append(scripts, rule[0])
 			}
 		} else if len(rule) > 1 {
 			if util.InArrayString(&scripts, rule[0]) {
 				continue
 			}

 			// 如果指定了schema文件，则schema文件有更新才会执行对应的脚本
 			for _, v := range rule[1:] {
 				if util.InArrayString(&updatedSchemaFiles, v) {
 					scripts = append(scripts, rule[0])
 					break
 				}
 			}
 		}
	}

	params := strings.Join(updatedNodes, ",")
	for k, v := range scripts {
		scripts[k] = fmt.Sprintf("%s %s", v, params)
	}

	return scripts
}

// 记录schema文件拥有的配置项
var schemaToNodes map[string][]string = make(map[string][]string)
// 更新特定配置文件拥有的nodes并返回当前有且上一次没有的nodes(被更新的node会作为参数传递给脚本)
func UpdateSchemaNodesAndDiff(file string, nodes []string) []string {
	lk<-1
	defer func() {
		<-lk
	}()

	nodeList, ok := schemaToNodes[file]
	if ! ok {
		schemaToNodes[file] = nodes
		return nodes
	}

	var result []string = []string{}
	for _, node := range nodes {
		if ! util.InArrayString(&nodeList, node) && ! util.InArrayString(&result, node) {
			result = append(result, node)
		}
	}

	schemaToNodes[file] = nodes

	return result
}

