package main

import (
	"doveclient/config"
	"fmt"
)

func main() {
	fmt.Printf("%s\n", config.DecodeSetting())
}
