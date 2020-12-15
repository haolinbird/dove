package handler

import (
	"doveclient/config"
	"doveclient/configwatcher"
	"doveclient/logger"
	"doveclient/projconfig"
)

func init() {
	handlers["GetProjectConfig"] = GetProjectConfig{}
}

type GetProjectConfig struct {
}

func (i GetProjectConfig) Process(args map[string]interface{}) (re []byte, err error) {
	name := args["name"].(string)
	var dbc *projconfig.DebugConfigs
	var dErr error
	if config.GetConfig().DEVMODE {
		dbc, dErr = projconfig.LoadDebugConfigs()
		if dErr != nil {
			logger.GetLogger("WARN").Printf("Failed to load debug config file: %s", dErr)
		}
	}
	hasBeenWatching := configwatcher.TouchDynamicNode(name)
	if hasBeenWatching {
		re, err = projconfig.GetConfig(name, dbc, "", true)
	} else {
		var hasUpdate, isDebugValue bool
		var valuePlain []byte
		_, valuePlain, re, _, hasUpdate, isDebugValue, err = configwatcher.GetNewValueOfConfig(name, dbc, "")
		go func() {
			if err == nil && !isDebugValue && hasUpdate {
				projconfig.StoreToMem(name, re)
				projconfig.SaveToDisk(name, valuePlain)
			}
		}()
	}
	return
}
