package main

import (
	"doveclient/build"
	"fmt"
	toml "github.com/pelletier/go-toml"
	"io/ioutil"
	"os"
	"path"
	"strconv"
	"strings"
)

func main() {
	CreateSettingFile(path.Dir(os.Args[0]) + string(os.PathSeparator))
	os.Exit(0)
}

// 用于构建时生成setting文件
func CreateSettingFile(rootDir string) {
	configstringB := []byte(build.SecretSettingsRaw)
	_, err := toml.Load(string(configstringB))
	if err != nil {
		fmt.Printf("Failed to parse your setting file: %s", err)
		os.Exit(1)
	}
	configstring := []string{}
	for i, _ := range configstringB {
		configstring = append(configstring, strconv.Itoa(int(^configstringB[i])))
	}
	pathSep := string(os.PathSeparator)
	settingsCompilePath := rootDir + pathSep + ".." + pathSep + "config" + pathSep + "settings.go"
	saveString := "package config\nvar SecretSettings = []byte{" + strings.Join(configstring, ",") + "}"
	err = ioutil.WriteFile(settingsCompilePath, []byte(saveString), 0644)
	if err != nil {
		fmt.Printf("Settings file create error: %s", err)
		os.Exit(1)
	}
}
