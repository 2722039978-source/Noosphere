package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/noosphere/backend/internal/attachmentfile"
	"github.com/noosphere/backend/internal/config"
	"github.com/noosphere/backend/internal/core"
	"github.com/noosphere/backend/internal/logging"
	"github.com/noosphere/backend/internal/ownedprocess"
)

// Version is the canonical build version. Overridden by -ldflags at build time.
var Version = "1.0.0" // canonical source: root VERSION file

func main() {
	ownedprocess.RunSupervisor()
	if len(os.Args) >= 2 && os.Args[1] == attachmentfile.PDFWorkerFlag {
		var err error
		switch len(os.Args) {
		case 2:
			err = attachmentfile.RunPDFWorker(os.Stdin, os.Stdout)
		case 3:
			err = attachmentfile.RunPDFFileWorker(os.Args[2], os.Stdout)
		default:
			os.Exit(2)
		}
		if err != nil {
			os.Exit(1)
		}
		return
	}
	configPath := flag.String("config", "config.yaml", "path to config file")
	backupData := flag.Bool("backup-data", false, "back up closed Noosphere backend data")
	restoreData := flag.String("restore-data-from", "", "absolute path to a verified backend data snapshot")
	restoreTo := flag.String("restore-data-to", "", "absolute path to a new directory for restored data")
	flag.Parse()

	if *restoreData != "" || *restoreTo != "" {
		if *backupData || *restoreData == "" || *restoreTo == "" {
			exitWithError("supply both -restore-data-from and -restore-data-to for a restore")
		}
		if err := config.RestoreData(*restoreData, *restoreTo); err != nil {
			exitWithError(err.Error())
		}
		fmt.Fprintln(os.Stdout, filepath.Clean(*restoreTo))
		return
	}
	if *backupData {
		absolute, err := filepath.Abs(*configPath)
		if err != nil {
			exitWithError(err.Error())
		}
		if info, err := os.Stat(absolute); err != nil || !info.Mode().IsRegular() {
			exitWithError("backup requires the installed application's config.yaml")
		}
		if err := os.Chdir(filepath.Dir(absolute)); err != nil {
			exitWithError(err.Error())
		}
		*configPath = absolute
	}
	managed := os.Getenv("NOOSPHERE_DESKTOP_MANAGED") == "1"
	input := bufio.NewReader(os.Stdin)
	parent := context.Background()
	if managed && !*backupData {
		// The parent releases startup only after it owns the process tree.
		line, err := input.ReadString('\n')
		if err != nil || line != "start\n" || os.Getenv("NOOSPHERE_BACKEND_TOKEN") == "" {
			fmt.Fprintln(os.Stderr, "managed backend launch authorization failed")
			os.Exit(1)
		}
		parent = watchDesktopParent(input)
	}

	cfg, err := config.Load(*configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "config error: %v\n", err)
		os.Exit(1)
	}
	cfg.App.Version = Version

	lock, err := config.LockWorkspaceData(cfg.WorkspaceDataDir())
	if err != nil {
		exitWithError(err.Error())
	}
	defer lock.Close()
	if *backupData {
		path, err := config.BackupData(*configPath, cfg, Version)
		if err != nil {
			exitWithError(err.Error())
		}
		fmt.Fprintln(os.Stdout, path)
		return
	}
	var upgrade bool
	if managed {
		upgrade, err = config.UpgradeNeeded(*configPath, Version)
		if err != nil {
			exitWithError(err.Error())
		}
		if upgrade {
			fmt.Fprintln(os.Stdout, "NOOSPHERE_UPGRADE_START")
		}
		if err := config.PrepareUpgrade(*configPath, cfg, Version); err != nil {
			exitWithError(err.Error())
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

	if parent.Err() != nil {
		return
	}
	app := bootstrapWithParent(cfg, parent)
	if upgrade {
		if err := completeStoreUpgrade(parent, app, *configPath); err != nil {
			_ = app.Close()
			exitWithError(err.Error())
		}
		fmt.Fprintln(os.Stdout, "NOOSPHERE_UPGRADE_DONE")
	}
	if err := serveWithParent(app, managed, parent, os.Stdout); err != nil {
		log.Printf("server error: %v", err)
		if closer != nil {
			_ = closer.Close()
		}
		os.Exit(1)
	}
}

func completeStoreUpgrade(ctx context.Context, app *App, configPath string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	modules := app.Lifecycle.Snapshot()
	for _, name := range []string{"workspaces", "nebula", "agent_actions", "model_lab"} {
		if module := modules[name]; module != nil && module.State == core.StateError {
			return fmt.Errorf("data upgrade remains pending: %s failed to open: %s", name, module.Err)
		}
	}
	return config.CompleteUpgrade(configPath, Version)
}

func exitWithError(message string) {
	fmt.Fprintln(os.Stderr, message)
	os.Exit(1)
}
