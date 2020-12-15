// 更新失败，重新进行检查和更新的相关处理。
package configwatcher

import (
	"time"
)

var queuedRechecks = make(chan string, 10)
var queuedSchemaRechecks = make(chan string, 5)

// 更新失败的节点信息
type failedItemInfo struct {
	Name          string
	LastCheckTime int64
}

// 根据schema进行更新时的失败信息
type failedSchemaInfo struct {
	Path          string
	LastCheckTime int64
}

var failedConfigItemRoots = make(map[string]failedItemInfo, 10)

var failedConfigSchemas = make(map[string]failedSchemaInfo, 5)

var failedConfigItemsRootsRWLock = make(chan int, 1)
var failedConfigSchemasRWLock = make(chan int, 1)

// 重新检查上次不完全获取成功的配置根节点或schema文件
func receiveRecheck() {
	for {
		select {
		case root := <-queuedRechecks:
			failedConfigItemsRootsRWLock <- 1
			_, exists := failedConfigItemRoots[root]
			if !exists {
				failedConfigItemRoots[root] = failedItemInfo{root, time.Now().Unix()}
				go func(name string) {
					time.Sleep(DELAY_OF_RECHECK_FAILED_CONFIG_ITEM * time.Second)
					failedConfigItemsRootsRWLock <- 1
					delete(failedConfigItemRoots, name)
					<-failedConfigItemsRootsRWLock
					updateProjectConfigByRootNode(name, false)
				}(root)
			}
			<-failedConfigItemsRootsRWLock
		case schemafile := <-queuedSchemaRechecks:
			failedConfigSchemasRWLock <- 1
			_, exists := failedConfigSchemas[schemafile]
			if !exists {
				failedConfigSchemas[schemafile] = failedSchemaInfo{schemafile, time.Now().Unix()}
				go func(name string) {
					time.Sleep(DELAY_OF_RECHECK_FAILED_CONFIG_ITEM * time.Second)
					failedConfigSchemasRWLock <- 1
					delete(failedConfigSchemas, name)
					<-failedConfigSchemasRWLock
					updateProjectConfigBySchemafile(name)
				}(schemafile)
			}
			<-failedConfigSchemasRWLock
		}
	}
}

func addToRecheckQueue(nodeName string) {
	failedConfigItemsRootsRWLock <- 1
	defer func() {
		<-failedConfigItemsRootsRWLock
	}()
	// 当存在为"*"的根节点时，不再将nodeName添加至重试队列，因为"*"的存在已经会更新所有节点
	if _, exists := failedConfigItemRoots["*"]; exists {
		return
	}
	if _, exists := failedConfigItemRoots[nodeName]; exists {
		return
	}
	queuedRechecks <- nodeName
}

func addToSchemaRecheckQueue(schemafile string) {
	failedConfigSchemasRWLock <- 1
	if _, exists := failedConfigSchemas[schemafile]; !exists {
		queuedSchemaRechecks <- schemafile
	}
	<-failedConfigSchemasRWLock

}
