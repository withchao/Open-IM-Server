package main

import (
	"flag"
	"fmt"
	"github.com/openimsdk/open-im-server/v3/tools/readseqtomongo/internal"
)

func main() {
	var config string
	flag.StringVar(&config, "c", "", "config directory")
	flag.Parse()
	if err := internal.Start(config); err == nil {
		fmt.Println("success")
	} else {
		fmt.Println("failed", err)
	}
}
