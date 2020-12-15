package zk

import (
	"doveclient/config"
	"fmt"
	"net"
	"os"
	"strings"
	"time"
)

var monitorNodeRoot string

// ConfigNameToPath
func CN2P(name string) string {
	path := config.GetZkConfig()["root"].(string)
	if name != "" && name != "/" {
		path += "/" + strings.Replace(name, ".", "/", -1)
		path = strings.TrimRight(path, "/")
	}
	return path
}

// PathToConfigName
func P2CN(path string) string {
	name := strings.TrimPrefix(path, strings.TrimRight(config.GetZkConfig()["root"].(string), "/")+"/")
	name = strings.Replace(strings.Trim(name, "/"), "/", ".", -1)
	return name
}

func MyMonitorNode() (string, error) {
	if monitorNodeRoot != "" {
		return monitorNodeRoot, nil
	}

	hostName, err := os.Hostname()
	if err != nil {
		return "unkown hostname", err
	}
	conn, err := net.DialTimeout("tcp", GetOneHost(), time.Second*7)
	if err != nil {
		return "", err
	}
	localAddr := strings.Split(conn.LocalAddr().String(), ":")
	monitorNodeRoot = MonitorNodeRoot() + "/" + hostName + ":" + fmt.Sprintf("%v@%v:%v", localAddr[0], os.Getpid(), config.GetConfig().ClientVer)
	return monitorNodeRoot, nil
}

func MonitorNodeRoot() string {
	return config.GetZkConfig()["root"].(string) + "_monitor"
}
