package configwatcher

import (
	"doveclient/configschema"
	"doveclient/logger"
	"doveclient/util"
	"strings"
	"time"
)

const (
	// 在此时间(单位: 秒)内没有被应用访问过的配置项将不再被监控.(schema文件中定义的配置项不受此限制)
	THRESHOLD_GC_ITEM = 600
	// 运行回收不再需要的watcher的时间间隔(单位: 秒).
	GC_UNNEEDED_NODE_WATCHER_INTERVAL = 610
)

func gcWatcher() {
	for {
		time.Sleep(time.Second * GC_UNNEEDED_NODE_WATCHER_INTERVAL)
		schemaConfigItems := configschema.GetAllSchemaItemRootNodes()
		t := time.Now().Unix()
		rootsLastAccessTime := make(map[string]int64, 10)
		for _, info := range getAllRegisteredNodeInfo() {
			dRoot := strings.Split(info.Name, ".")[0]
			if _, exists := rootsLastAccessTime[dRoot]; exists {
				if info.LastAccessTime > rootsLastAccessTime[dRoot] {
					rootsLastAccessTime[dRoot] = info.LastAccessTime
				}
			} else {
				rootsLastAccessTime[dRoot] = info.LastAccessTime
			}
		}
		var nodesToBeKickedFromWatches []string
		// 回收通过api调用时注册的watcher
		for dRoot, lTime := range rootsLastAccessTime {
			if watchList[dRoot].State != int8(1) {
				continue
			}
			if t-lTime >= THRESHOLD_GC_ITEM {
				if util.InArrayString(&schemaConfigItems, dRoot) {
					continue
				}
				logger.GetLogger("INFO").Printf("Dynamically registered (via Api) config item root node[%v] expired...has not been accessed for about %v seconds. Removing watcher on it....", dRoot, THRESHOLD_GC_ITEM)
				nodesToBeKickedFromWatches = append(nodesToBeKickedFromWatches, dRoot)
			}
		}

		// 回收通过解析schema注册的watcher
		for dRoot, _ := range watchList {
			if watchList[dRoot].State != int8(1) {
				continue
			}
			if _, exists := rootsLastAccessTime[dRoot]; !exists && !util.InArrayString(&nodesToBeKickedFromWatches, dRoot) && !util.InArrayString(&schemaConfigItems, dRoot) {
				logger.GetLogger("INFO").Printf("Config root[%v] not used by app anymore. Removing watcher on it....", dRoot)
				nodesToBeKickedFromWatches = append(nodesToBeKickedFromWatches, dRoot)
			}
		}

		for _, dRoot := range nodesToBeKickedFromWatches {
			logger.GetLogger("INFO").Printf("Kicking [%s] from watches...", dRoot)
			*(watchList[dRoot].Chan) <- 1
		}
	}
}
