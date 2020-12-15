package main

import (
	"doveclient/callbacks"
	"doveclient/config"
	"fmt"
)

func main() {
	config.Setup()
	err := callbacks.InitClearApc()
	fmt.Printf("initClearApc:\n----\n%s\n", err)
	fpmHost, err := callbacks.GetFpmHost()
	fmt.Printf("fpmHost:\n----\n%s\n%s\n", fpmHost, err)

	r, err := callbacks.ClearApc()
	fmt.Printf("\nclearApc:\n----\n%s\n%s\n", r, err)

	r, err = callbacks.ReloadPHPServer()
	fmt.Printf("\n\nReloadPHPServer:\n----\n%s\n%s\n", r, err)
}
