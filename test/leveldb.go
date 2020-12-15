package main

import (
	"doveclient/config"
	"doveclient/encrypt"
	"doveclient/storage/leveldb"
	"fmt"
)

func main() {
	config.Setup()
	err := leveldb.InitDb(leveldb.Config{Dsn: config.GetConfig().DATA_DIR + "/configs_crypt/"})
	if err != nil {
		fmt.Printf("error: %s\n", err)
		return
	}
	k := []byte("TestOnline.ttkey1")
	e := encrypt.Encryptor{IV: []byte(config.GetConfig().ENCRYPT_IV), Key: []byte(config.GetConfig().ENCRYPT_KEY)}
	data, err := e.Encrypt([]byte("老麻烦梅雷莱斯"))
	fmt.Printf("%s\n", err)
	// err = db.Conn.Put(k, []byte("老麻烦梅雷莱斯"), nil)
	err = db.Conn.Put(k, data, nil)
	fmt.Printf("%s, %s\n", err, data)
	v, err := db.Conn.Get(k, nil)
	fmt.Printf("%s, %v\n", v, err)
}
