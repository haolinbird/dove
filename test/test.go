package main

import (
	"doveclient/encrypt"
	"flag"
	"fmt"
	_ "log"
	_ "time"
)

func main() {
	c := encrypt.Encryptor{IV: []byte("12345678"), Key: []byte("aa飞aaa")}
	str := []byte("©发发😊234aaa2@1^^^^")
	fmt.Printf("%d\n", len(str))
	e, _ := c.Encrypt([]byte(str))
	d, _ := c.Decrypt(e)
	fmt.Printf("%s\n%s", e, d)
	flag.Parse()
	fmt.Printf("\n%v\n", flag.Args())
	return
}
