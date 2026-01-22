package main

import (
	"flag"
	"fmt"
	"os"
)

func main() {
	fmt.Println("proxy-subs-backend")

	// 解析入参
	configPath := flag.String("config", "proxy-subs-backend.json", "path to proxy subs config file")
	flag.Parse()

	serverConfig := new(SubsServerConfig)
	err := serverConfig.LoadJsonConfig(*configPath)
	if err != nil {
		fmt.Printf("Error parsing config file: %s\n", err)
		os.Exit(1)
	}
	if serverConfig.DebugMode {
		serverConfig.ShowConfig()
	}

}
