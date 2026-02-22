package main

import (
	"flag"
	"fmt"
	"log"
	"os"

	"kiro-go-gw/internal/config"
	"kiro-go-gw/internal/server"
)

var version = "dev"

func main() {
	showVersion := flag.Bool("version", false, "Show version information")
	flag.Parse()

	if *showVersion {
		fmt.Printf("Kiro Gateway %s\n", version)
		os.Exit(0)
	}

	cfg := config.Load()

	hasCreds := cfg.RefreshToken != "" || cfg.CredsFile != "" || cfg.CliDbFile != ""
	if !hasCreds {
		fmt.Fprintf(os.Stderr, "Error: No Kiro credentials configured.\n")
		fmt.Fprintf(os.Stderr, "Set one of: REFRESH_TOKEN, KIRO_CREDS_FILE, or KIRO_CLI_DB_FILE\n")
		fmt.Fprintf(os.Stderr, "See .env.example for configuration options.\n")
		os.Exit(1)
	}

	srv, err := server.New(cfg)
	if err != nil {
		log.Fatalf("Failed to create server: %v", err)
	}

	addr := fmt.Sprintf("%s:%d", cfg.ServerHost, cfg.ServerPort)
	log.Printf("Starting Kiro Gateway %s on %s", version, addr)

	if err := srv.Start(addr); err != nil {
		log.Fatal(err)
	}
}
