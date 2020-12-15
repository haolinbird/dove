package main

import (
	"fmt"
	"time"
)

func main() {
	n := 100000
	ch := make(chan []byte)
	for ; n > 0; n-- {
		go func(c *chan []byte) {
			ch <- []byte("flwjelfj付款来我家\n")
			time.Sleep(time.Millisecond * 1)
			return
		}(&ch)
	}
	for {
		fmt.Printf("%s", <-ch)
	}
}
