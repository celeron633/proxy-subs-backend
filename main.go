package main

import (
	"flag"
	"fmt"
	"os"
)

func main() {
	fmt.Println("proxy-subs-backend")

	// parse flags
	configPath := flag.String("config", "proxy-subs-backend.json", "path to proxy subs config file")
	flag.Parse()

	// Config
	serverConfig := new(SubsServerConfig)
	err := serverConfig.LoadJsonConfig(*configPath)
	if err != nil {
		fmt.Printf("Error parsing config file: %s\n", err)
		os.Exit(1)
	}
	if serverConfig.DebugMode {
		serverConfig.ShowConfig()
	}

	// TokenValidator
	tokenManager := new(TokenManager)
	err = tokenManager.LoadTokenFromFile(serverConfig.TokenFilePath)
	if err != nil {
		fmt.Printf("Error parsing token file: %s\n", err)
		os.Exit(1)
	}

	// API switch
	apiSwitch := NewApiSwitch(serverConfig.EnableApiWhenStart)

	// Server
	subsServer := NewSubsServer(serverConfig, tokenManager, apiSwitch)
	subsServer.StartServer()

}
