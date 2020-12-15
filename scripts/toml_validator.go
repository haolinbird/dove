package main

import (
	"os"

	"git.oschina.net/chaos.su/go-toml"
)

func main() {
	for _, arg := range os.Args {
		if arg == "-h" || arg == "--help" {
			println("Usage:")
			println(os.Args[0], "/path/to/TOMLFILE")
			println("Options:")
			println("-h,--help show this help message.")
			os.Exit(0)
		}
	}
	if len(os.Args) < 2 {
		println("Usage:", os.Args[0], "/path/to/TOMLFILE")
		os.Exit(1)
	}
	_, err := toml.LoadFile(os.Args[1])
	if err != nil {
		println(err.Error())
		os.Exit(1)
	}
	os.Exit(0)
}
