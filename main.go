package main

import (
	"flag"
	"fmt"
	"log"
	"os"
)

func main() {
	listenAddr := flag.String("listen", "0.0.0.0:8080", "HTTP listen address")
	databasePath := flag.String("db", "data/proxy-subs.db", "SQLite database path")
	webDir := flag.String("web-dir", "web", "directory containing the web console")
	debugMode := flag.Bool("debug", false, "enable Gin debug mode and HTTP request logging")
	flag.Parse()

	if !*debugMode {
		setReleaseMode()
	}

	store, err := OpenStore(*databasePath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "open database: %v\n", err)
		os.Exit(1)
	}
	defer store.Close()

	server, err := NewSubsServer(store, *webDir, *debugMode)
	if err != nil {
		fmt.Fprintf(os.Stderr, "initialize server: %v\n", err)
		os.Exit(1)
	}

	log.Printf("proxy-subs-backend listening on %s (database: %s)", *listenAddr, *databasePath)
	if err := server.StartServer(*listenAddr); err != nil {
		fmt.Fprintf(os.Stderr, "start server: %v\n", err)
		os.Exit(1)
	}
}
