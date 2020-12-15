package main

import (
	"bytes"
	"doveclient/encrypt"
	"fmt"
	"io/ioutil"
	"runtime"
	"time"
)

func main() {
	c := encrypt.Encryptor{IV: []byte("11111111"), Key: []byte("example-encrypt-key")}
	str, _ := ioutil.ReadFile("data/enc.data")
	runtime.GOMAXPROCS(runtime.NumCPU() - 1)
	n := 100000
	tn := n
	var e, d []byte

	// p := 3000
	// pc := p
	// cn := n / p
	st := time.Now()
	for ; n > 0; n-- {
		e, _ = c.Encrypt([]byte(str))
		d, _ = c.Decrypt(e)
	}
	/*

		chs := make([]chan bool, p)

		for p > 0 {
			p--
			chs[p] = make(chan bool)
			go func(ch *chan bool, n int) {
				for ; n > 0; n-- {
					e, _ = c.Encrypt([]byte(str))
					d, _ = c.Decrypt(e)
				}
				*ch <- true
			}(&chs[p], cn)
		}
		var f bool
		// for p < pc {
		// 	f = <-chs[p]
		// 	p++
		// }

		f = <-chs[pc-1]
	*/
	et := time.Now()
	// fmt.Printf("%v", f)
	fmt.Printf("\n加密前长度:%d,加密后长度：%d,加密前后字符是否相等:%v\n", len(str), len(d), bytes.Compare(str, d) == 0)
	td := et.Sub(st).Seconds() / 2

	fmt.Printf("\n总耗时%v秒, %f秒每次,%f次每秒,秒加密数据量：%fk", td, td/float64(tn), float64(tn)/td, (float64(tn)/td)*(float64(len(str))/1024.0))
	return
}
