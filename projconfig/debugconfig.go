package projconfig

import (
	"doveclient/config"
	"doveclient/util"
	"fmt"
	"os"
	"strings"

	toml "git.oschina.net/chaos.su/go-toml"
)

var debugFilename = "config.debug.toml"

type DebugConfigs struct {
	debugConfigs map[string]*toml.TomlTree
}

// 获取DEV模式debug文件名
func GetDebugFilename() string {
	return debugFilename
}

func LoadDebugConfigs() (*DebugConfigs, error) {
	configs := map[string]*toml.TomlTree{}
	debugFiles := GetDebugFiles()
	var err error
	for _, debugFile := range debugFiles {
		configs[debugFile], err = GetDebugValuesByFile(debugFile)
		if err != nil {
			return nil, err
		}
	}
	return &DebugConfigs{debugConfigs: configs}, nil
}

func GetDebugValuesByFile(debugfilePath string) (*toml.TomlTree, error) {
	t, err := toml.LoadFile(debugfilePath)
	if err != nil {
		err = fmt.Errorf("Failed to parse config debug file: %s", err)
		return nil, err
	}
	return t, err
}

// GetConfigBySchemafile 根据schema文件来获取对应的debug值
// 因为可能存在多个debug文件，所以应优先获取与scehma文件在同一目录的debug文件中的值.
// schemaFile可为空，此时获取到的debug值debug文件无法预见。（此中情况发生在通过api来获取配置时）
// 返回json字节码形式的配置和配置的原始toml字符串
func (d *DebugConfigs) GetConfigBySchemafile(name string, schemaFile string, withToml bool) ([]byte, string) {
	if len(d.debugConfigs) < 1 {
		return nil, ""
	}
	var configs *toml.TomlTree
	var tomlV string
	pathSep := string(os.PathSeparator)
	pathPortions := strings.Split(schemaFile, pathSep)
	expectedDebugFile := strings.Join(pathPortions[:len(pathPortions)-1], pathSep) + pathSep + debugFilename
	expectedDebugFileB := []byte(expectedDebugFile)
	expectLen := len(expectedDebugFile)
	// 不存在的话则使用最靠近schemafile的debug文件
	var myExists bool
	if _, myExists = d.debugConfigs[expectedDebugFile]; !myExists {
		testPos := 0
		var nearestDebugFile string
		// 找到第一个不相同的字符的位置
		for dName, _ := range d.debugConfigs {
			dNameB := []byte(dName)
			for pos, _ := range dNameB {
				if pos >= expectLen || dNameB[pos] != expectedDebugFileB[pos] {
					if pos >= testPos {
						testPos = pos
						nearestDebugFile = dName
					}
					break
				}
			}
		}
		configs = d.debugConfigs[nearestDebugFile]
	} else {
		configs = d.debugConfigs[expectedDebugFile]
	}
	var re []byte
	v := configs.Get(name)
	switch v.(type) {
	case nil:
		return nil, ""
	}
	re = util.JsonEncode(v)
	if withToml {
		namePortions := strings.Split(name, ".")
		lastName := namePortions[len(namePortions)-1]
		switch v.(type) {
		case *toml.TomlTree:
			tomlV = v.(*toml.TomlTree).ToString()
		case nil:
			tomlV = ""
		case string:
			tomlV = fmt.Sprintf("%s=%q", lastName, v)
		default:
			tomlV = fmt.Sprintf("%s=%v", lastName, v)
		}
	}
	return re, tomlV
}

// 获取debug文件列表
func GetDebugFiles() []string {
	pathSep := string(os.PathSeparator)
	debugFiles := []string{}

	if config.GetConfig().DEBUG_FILE != "" && util.FileExists(config.GetConfig().DEBUG_FILE) {
		debugFiles = append(debugFiles, config.GetConfig().DEBUG_FILE)
		return debugFiles
	}

	for _, dir := range config.GetConfig().CONFIG_DIR {
		debugFile := strings.TrimRight(dir, pathSep) + pathSep + debugFilename
		if util.FileExists(debugFile) {
			debugFiles = append(debugFiles, debugFile)
		}
	}
	return debugFiles
}
