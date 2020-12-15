package configschema

import (
	"doveclient/config"
	"doveclient/logger"
	"doveclient/util"
	"io/ioutil"
	"os"
	"regexp"
	"strings"
)

const SCHEMAFILE_SUFFIX = ".schema.php"
const TOML_SUFFIX = ".schema.toml"

type SchemaFile struct {
	Md5  string
	Path string
}

func IsSchemaFile(filename string) bool {
	/*filenameB := []byte(filename)
	filenameBLen := len(filenameB)
	schemaFileSuffixB := []byte(SCHEMAFILE_SUFFIX)
	schemaFileSuffixBLen := len(schemaFileSuffixB)
	if filenameBLen > len(schemaFileSuffixB) {
		isSchemafile := true
		for i, b := range schemaFileSuffixB {
			if filenameB[filenameBLen-(schemaFileSuffixBLen-i)] != b {
				isSchemafile = false
				break
			}
		}
		return isSchemafile
	}
	return false*/

	if strings.HasSuffix(filename, SCHEMAFILE_SUFFIX) == true || strings.HasSuffix(filename, TOML_SUFFIX) == true {
		return true
	}

	return false
}
func LoadSchemaFiles() []SchemaFile {
	var files []SchemaFile
	for _, dir := range config.GetConfig().CONFIG_DIR {
		files = append(files, GetSchemaFilesByDir(dir)...)
	}
	return files
}

func GetSchemaFilesByDir(dir string) []SchemaFile {
	/*suffixBytes := []byte(".schema.php")
	suffixLen := len(suffixBytes)*/
	pathSeparator := string(os.PathSeparator)
	files := []SchemaFile{}
	df, err := os.Open(dir)
	if err != nil {
		logger.GetLogger("ERROR").Printf("Failed to open config dir[%s] : %s\n", dir, err)
		return files
	}
	dFiles, err := df.Readdir(0)
	if err != nil {
		logger.GetLogger("ERROR").Printf("Failed to read config files in dir[%s] : %s\n", dir, err)
		return files
	}
	for _, f := range dFiles {
		if f.IsDir() {
			continue
		}
		/*fName := []byte(f.Name())
		fNameLenth := len(fName)
		if fNameLenth <= suffixLen {
			continue
		}
		var i int
		for i = 1; i <= suffixLen; i++ {
			if fName[fNameLenth-i] != suffixBytes[suffixLen-i] {
				break
			}
		}*/
		// if i-1 == suffixLen {
		if strings.HasSuffix(f.Name(), ".schema.php") || strings.HasSuffix(f.Name(), ".schema.toml") {
			filePath := strings.TrimRight(dir, pathSeparator) + pathSeparator + f.Name()
			md5sum, mErr := SchemaFileMd5(filePath)
			if mErr != nil {
				md5sum = "error_of_md5sum"
				logger.GetLogger("ERROR").Printf("Failed to md5sum config schema file [%s] :\n", filePath, mErr)
			}
			files = append(files, SchemaFile{Path: filePath, Md5: md5sum})
		}
	}
	return files
}

func GetSchemaFileContent(filePath string) ([]byte, error) {
	return ioutil.ReadFile(filePath)
}

func SchemaFileMd5(filePath string) (string, error) {
	return util.Md5File(filePath)
}

func ParseItemPlaceholders(filePath string) (items []string, err error) {
	b, err := ioutil.ReadFile(filePath)
	if err != nil {
		return
	}
	reg := regexp.MustCompile("\"#\\{(\\w+|\\w+[\\-\\d\\w\\.]*[\\w\\d])\\}\"")
	return reg.FindAllString(string(b), -1), nil
}

// 获取 filePath的定义的不重复的配置项
func ParseItems(filePath string) (items []string, err error) {
	items, err = ParseItemPlaceholders(filePath)
	if err != nil {
		return
	}
	tempItems := []string{}
	var exists bool
	for _, item := range items {
		exists = false
		item = strings.Trim(item, "\"#{}")
		for _, tItem := range tempItems {
			if tItem == item {
				exists = true
				break
			}
		}
		if !exists {
			tempItems = append(tempItems, item)
		}
	}

	return tempItems, nil
}

func GetAllSchemaItems() []string {
	schemafiles := LoadSchemaFiles()
	var items []string
	for _, f := range schemafiles {
		tItems, _ := ParseItemPlaceholders(f.Path)
		items = append(items, tItems...)
	}
	return items
}

func GetAllSchemaItemRootNodes() []string {
	items := GetAllSchemaItems()
	var itemRoots []string
	var exists bool
	for _, item := range items {
		exists = false
		item = strings.Trim(item, "\"#{}")
		portions := strings.Split(item, ".")
		for _, r := range itemRoots {
			if r == portions[0] {
				exists = true
				break
			}
		}
		if !exists {
			itemRoots = append(itemRoots, portions[0])
		}
	}
	return itemRoots
}

func GetCompilationCaution(withTag bool) []byte {
	caution := "\n/**\n * This file is generated automatically by ConfigurationSystem.\n" +
		" * Do not change it manually in production, unless you know what you're doing and can take responsibilities for the consequences of changes you make.\n" +
		" */" +
		"\n"
	if withTag {
		caution = "<?php " + caution + "?>\n"
	}
	return []byte(caution)
}
