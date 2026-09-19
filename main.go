package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
)

func main() {
	listenAddr := flag.String("listen", "127.0.0.1:8080", "HTTP listen address")
	databasePath := flag.String("db", "data/proxy-subs.db", "SQLite database path")
	webDir := flag.String("web-dir", "web", "directory containing the web console")
	fileRoot := flag.String("file-root", ".", "root directory available to the server file browser")
	debugMode := flag.Bool("debug", false, "enable Gin debug mode and HTTP request logging")
	resetPassword := flag.Bool("reset-password", false, "list administrator accounts, reset a password and exit")
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

	if *resetPassword {
		if err := runResetPassword(context.Background(), store, os.Stdin, os.Stdout); err != nil {
			store.Close()
			fmt.Fprintf(os.Stderr, "reset password: %v\n", err)
			os.Exit(1)
		}
		return
	}

	server, err := NewSubsServer(store, *webDir, *fileRoot, *debugMode)
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
