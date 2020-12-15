package main

import (
	"doveclient/config"
	"fmt"
	"github.com/samuel/go-zookeeper/zk"
	"time"
)

func main() {
	config.Setup()
	conn, _, err := zk.Connect(config.GetZkConfig()["hosts"].(map[string][]string)[config.GetConfig().ENV], time.Second)
	d, _, evCh, err := conn.ExistsW("/test2")
	fmt.Printf("%v\n%s,[%v]\n-------\n", err, d, evCh)
	for {
		ev := <-evCh

		fmt.Printf("(%s)\n[%s]\n", ev, evCh)
		break
	}
}
