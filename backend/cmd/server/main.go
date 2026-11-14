package main

import (
	"bufio"
	"flag"
	"fmt"
	"log"
	"os"

	"github.com/noosphere/backend/internal/config"
	"github.com/noosphere/backend/internal/logging"
)

// Version is the canonical build version. Overridden by -ldflags at build time.
var Version = "0.7.0" // canonical source: root VERSION file

func main() {
	configPath := flag.String("config", "config.yaml", "path to config file")
	flag.Parse()

	managed := os.Getenv("NOOSPHERE_DESKTOP_MANAGED") == "1"
	input := bufio.NewReader(os.Stdin)
	if managed {
		// The parent releases startup only after it owns the process tree.
		line, err := input.ReadString('\n')
		if err != nil || line != "start\n" || os.Getenv("NOOSPHERE_BACKEND_TOKEN") == "" {
			fmt.Fprintln(os.Stderr, "managed backend launch authorization failed")
			os.Exit(1)
		}
	}

	cfg, err := config.Load(*configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "config error: %v\n", err)
		os.Exit(1)
	}

	if managed {
		upgrade, err := config.UpgradeNeeded(*configPath, Version)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		if upgrade {
			fmt.Fprintln(os.Stdout, "NOOSPHERE_UPGRADE_START")
		}
		if err := config.PrepareUpgrade(*configPath, cfg, Version); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		if upgrade {
			fmt.Fprintln(os.Stdout, "NOOSPHERE_UPGRADE_DONE")
		}
	}

	closer, err := logging.Init(cfg.Logging.Dir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "logging init error: %v\n", err)
	}
	if closer != nil {
		defer closer.Close()
	}

	log.Printf("=== Noosphere Backend v%s ===", Version)

	app := bootstrap(cfg)
	if err := serve(app, managed, input, os.Stdout); err != nil {
		log.Printf("server error: %v", err)
		if closer != nil {
			_ = closer.Close()
		}
		os.Exit(1)
	}
}
