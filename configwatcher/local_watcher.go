package configwatcher

import (
	"doveclient/config"
	"doveclient/configschema"
	"doveclient/logger"
	"doveclient/projconfig"
	"doveclient/util"
	"os"
	"path/filepath"
	"time"

	"path"
	"strings"

	"github.com/fsnotify"
)

// 当本地(schema、debug)文件监控失败后，重启监控的事件间隔.(单位: 秒)
const INTERVAL_RESTART_LOCAL_FILE_WATCHER = 2

type DebugFile struct {
	Md5  string
	Path string
}

var failedDirs []string
var fDirLock chan int = make(chan int, 1)

func appendFaildDirs(fDir string) {
	fDirLock <- 1
	defer func() {
		<-fDirLock
	}()

	if util.InArrayString(&failedDirs, fDir) == false {
		failedDirs = append(failedDirs, fDir)
	}
}

func retryAddFaildDirs(wc *fsnotify.Watcher) {
	for {
		fDirLock <- 1
		for n, fDir := range failedDirs {
			_, sErr := os.Stat(fDir)
			if sErr == nil {
				sErr = wc.Add(fDir)
				if sErr == nil {
					failedDirs = util.DelInArrayString(&failedDirs, n)
					logger.GetLogger("INFO").Printf("Successfully re-added [%s] to fs watcher.", fDir)
					newSchemafiles := configschema.GetSchemaFilesByDir(fDir)
					for _, schemafile := range newSchemafiles {
						go updateProjectConfigBySchemafile(schemafile.Path)
					}
				}
			}
		}
		<-fDirLock

		time.Sleep(time.Second * INTERVAL_RESTART_LOCAL_FILE_WATCHER)
	}
}

func watchDebugOrSchemafiles() {
	var err error
	var wc *fsnotify.Watcher
	for {
		wc, err = fsnotify.NewWatcher()
		if err != nil {
			logger.GetLogger("ERROR").Printf("Failed to create fs watcher: %s", err)
			time.Sleep(time.Second * INTERVAL_RESTART_LOCAL_FILE_WATCHER)
		} else {
			break
		}
	}
	for _, dir := range config.GetConfig().CONFIG_DIR {
		err = wc.Add(dir)
		if err != nil {
			logger.GetLogger("ERROR").Printf("Failed to add dir  to fs watcher: %s. Try again later.", err)
			appendFaildDirs(dir)
		}
	}
	go loopWatch(wc)
	go retryAddFaildDirs(wc)
}

// loopWatch 监听schema及debug文件的更改
func loopWatch(wc *fsnotify.Watcher) {
	for {
		select {
		case ev := <-wc.Events:
			baseName := filepath.Base(ev.Name)

			// 因为部分业务方发布流程是先删除原来的目录再重新创建而不是覆盖，所以一个配置目录被删除的时候要重新加入inotify监控.
			_, sErr := os.Stat(ev.Name)
			if sErr != nil && (strings.HasSuffix(ev.Name, ".php") || strings.HasSuffix(ev.Name, ".toml")) {
				fDir := path.Dir(ev.Name)

				for _, dir := range config.GetConfig().CONFIG_DIR {
					if strings.TrimSuffix(dir, "\\/") == strings.TrimSuffix(fDir, "\\/") {
						logger.GetLogger("ERROR").Printf("File or directory is deleted: %s. Try again later.", sErr)
						wc.Remove(fDir)
						appendFaildDirs(fDir)
					}
				}
			}

			var shouldUpdate bool
			// 调试文件有更新
			if config.GetConfig().DEVMODE && baseName == projconfig.GetDebugFilename() {
				switch ev.Op {
				case fsnotify.Create:
					logger.GetLogger("INFO").Printf("New debug file [%s] detected.", ev.Name)
					shouldUpdate = true
				case fsnotify.Write, fsnotify.Rename:
					logger.GetLogger("INFO").Printf("Writtings on debug file [%s] detected.", ev.Name)
					shouldUpdate = true
				}
				if shouldUpdate {
					logger.GetLogger("INFO").Print("Updating all configs...")
					updateProjectConfigByRootNode("*", true)
				}
			} else if configschema.IsSchemaFile(ev.Name) {
				// 配置文件schema有更新
				switch ev.Op {
				case fsnotify.Create:
					logger.GetLogger("INFO").Printf("New config schema file [%s] detected.", ev.Name)
					shouldUpdate = true
				case fsnotify.Write:
					logger.GetLogger("INFO").Printf("Writtings on config schema file [%s] detected.", ev.Name)
					shouldUpdate = true
				}
				if shouldUpdate {
					logger.GetLogger("INFO").Print("Begin updating configs by schemafile...")
					go updateProjectConfigBySchemafile(ev.Name)
				}
			}

		case err := <-wc.Errors:
			logger.GetLogger("ERROR").Printf("local watcher encountered an error: %s", err)
		}
	}
}
