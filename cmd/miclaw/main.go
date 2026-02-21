package main

import (
	"flag"
	"fmt"
	"os"
)

func main() {
	configPath := flag.String("config", "~/.miclaw/config.json", "path to config file")
	flag.Parse()

	if flag.NArg() != 0 {
		panic("unexpected positional arguments")
	}
	if *configPath == "" {
		panic("config path must not be empty")
	}

	fmt.Fprintln(os.Stderr, "miclaw v0.0.1")
	os.Exit(0)
}
