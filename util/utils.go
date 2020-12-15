package util

import (
	"bytes"
	"crypto/md5"
	"fmt"
	toml "git.oschina.net/chaos.su/go-toml"
	"io/ioutil"
	"os"
	"os/exec"
	"reflect"
	"strings"
)

func StringSliceUnique(s *[]string) {
	temp := []string{}
	var exists bool
	for _, elm := range *s {
		exists = false
		for _, tElem := range temp {
			if elm == tElem {
				exists = true
				break
			}
		}
		if !exists {
			temp = append(temp, elm)
		}
	}
	*s = temp
}

func InArrayString(strArr *[]string, sep string) bool {
	for _, s := range *strArr {
		if s == sep {
			return true
		}
	}
	return false
}

func DelInArrayString(arr *[]string, index int) []string {
	l := len(*arr)
	if l < 1 || index >= l{
		return *arr
	}
	if index == 0 {
		return (*arr)[1:]
	}
	return append((*arr)[0:index], (*arr)[index+1:]...)
}

func ExistsInMap(sMap interface{}, index interface{}) bool {
	return reflect.ValueOf(sMap).MapIndex(reflect.ValueOf(index)).IsValid()
}

func TomlToJson(content string) ([]byte, error) {
	t, err := toml.Load(content)
	if err != nil {
		return nil, err
	}
	return TraverseTomlAsJson(t), nil
}

// 解析配置系统中toml形式配置的值(即第一个key的值).
func TomlConfigValueToJson(content string) (re []byte, err error) {
	var t *toml.TomlTree
	t, err = toml.Load(content)
	if err != nil {
		return
	}
	for _, key := range t.Keys() {
		v := t.Get(key)
		re = JsonEncode(v)
		break
	}
	return
}

// 将tomlTree转成json字节码。只适用于doveclient内部使用，非通用的json解码
func TraverseTomlAsJson(t *toml.TomlTree) []byte {
	keys := t.Keys()
	kLen := len(keys)
	b := []byte{'{'}
	var valueB []byte
	for i, k := range keys {
		value := t.Get(k)
		switch tp := value.(type) {
		case *toml.TomlTree:
			valueB = TraverseTomlAsJson(tp)
		case nil:
			valueB = []byte("null")
		case string:
			valueB = []byte(fmt.Sprintf("%q", value))
		case []interface{}:
			valueB = ListToJson(value.([]interface{}))
		default:
			valueB = []byte(fmt.Sprintf("%v", value))
		}
		b = append(b, []byte(fmt.Sprintf("%q", k)+":")...)
		b = append(b, valueB...)
		if i < kLen-1 {
			b = append(b, ',')
		}
	}
	b = append(b, '}')
	return b
}

// JsonEncode json编码. 不适用于普遍的数据类型，主要用于doveclient中 toml的转换。
func JsonEncode(v interface{}) (re []byte) {
	switch v.(type) {
	case *toml.TomlTree:
		re = TraverseTomlAsJson(v.(*toml.TomlTree))
	case nil:
		re = []byte("null")
	case string:
		re = []byte(fmt.Sprintf("%q", v))
	case []interface{}:
		re = ListToJson(v.([]interface{}))
	default:
		re = []byte(fmt.Sprintf("%v", v))
	}
	return
}

func ListToJson(list []interface{}) []byte {
	b := []byte{'['}
	kLen := len(list)
	for i, v := range list {
		switch v.(type) {
		case []interface{}:
			b = append(b, ListToJson(v.([]interface{}))...)
		case string:
			b = append(b, []byte(fmt.Sprintf("%q", v))...)
		default:
			b = append(b, []byte(fmt.Sprintf("%v", v))...)
		}
		if i < kLen-1 {
			b = append(b, ',')
		}
	}
	b = append(b, ']')
	return b
}

func TomlToMap(content string) (map[string]interface{}, error) {
	t, err := toml.Load(content)
	if err != nil {
		return nil, err
	}
	return TraverseTomlTree(t), nil
}

func TraverseTomlTree(t *toml.TomlTree) map[string]interface{} {
	keys := t.Keys()
	mm := make(map[string]interface{}, 1)
	for _, k := range keys {
		elm := t.Get(k)
		switch tp := elm.(type) {
		case *toml.TomlTree:
			mm[k] = TraverseTomlTree(tp)
		case nil:
		default:
			mm[k] = elm
		}
	}
	return mm
}

func Md5File(filePath string) (string, error) {
	b, err := ioutil.ReadFile(filePath)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", md5.Sum(b)), nil
}

// 不包含空字符串的split
func StringSplit(str string, sep string) []string {
	raw := strings.Split(str, sep)
	if len(raw) == 1 && raw[0] == "" {
		return []string{}
	}
	return raw
}

func FileExists(name string) bool {
	_, err := os.Stat(name)
	return err == nil || os.IsExist(err)
}

// 运行操作系统命令
func ExtCall(filename string, args ...string) (output []byte, err error) {
	out := bytes.Buffer{}
	cmd := exec.Command(filename, args...)
	cmd.Stdout = &out
	cmd.Stderr = &out
	err = cmd.Run()
	output = out.Bytes()
	return
}
