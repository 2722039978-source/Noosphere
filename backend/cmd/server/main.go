package main

import (
	"flag"
	"fmt"
	"log"
	"os"

	"github.com/noosphere/backend/internal/config"
	"github.com/noosphere/backend/internal/logging"
)

// Version is the canonical build version. Overridden by -ldflags at build time.
var Version = "0.5.0" // canonical source: root VERSION file

func main() {
	configPath := flag.String("config", "config.yaml", "path to config file")
	flag.Parse()

	cfg, err := config.Load(*configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "config error: %v\n", err)
		os.Exit(1)
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
	serve(app)
}
