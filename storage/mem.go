// 存储在进程内存中的配置处理
package storage

import (
	"fmt"
)

// json形式保存在内存中的值
var memCachedProjectConfigs = map[string][]byte{}

var memUpdateLock = make(chan int, 1)

// 返回json字节码或者nil
func GetFromMem(name string) []byte {
	memUpdateLock <- 1
	d := memCachedProjectConfigs[name]
	<-memUpdateLock
	return d
}

// data json字节码
func StoreToMem(name string, data []byte) {
	memUpdateLock <- 1
	memCachedProjectConfigs[name] = data
	<-memUpdateLock
}

var rawConfig map[string]string = make(map[string]string, 100)

// 保存配置原文
func SaveRawConfig(name, value string) {
	rawConfig[name] = value
}

// 获取配置原文
func GetRawConfig(name string) (string, error) {
	value, ok := rawConfig[name]
	if ok {
		return value, nil
	}

	return "", fmt.Errorf("Cannot find value for config[%s].", name)
}
