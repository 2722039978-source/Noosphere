package main

import (
	"bytes"
	"context"
	"encoding/json"
	stderrors "errors"
	"fmt"
	"io"
	"log"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/noosphere/backend/internal/codelens"
	"github.com/noosphere/backend/internal/core"
	"github.com/noosphere/backend/internal/devops/metrics"
	"github.com/noosphere/backend/internal/evalrunner"
	"github.com/noosphere/backend/internal/hostinfo"
	"github.com/noosphere/backend/internal/modellab"
	"github.com/noosphere/backend/internal/modelops"
	"github.com/noosphere/backend/internal/modelstore"
	"github.com/noosphere/backend/internal/workspaces"
)

func registerModelWorkers(manager *workspaces.Manager, lab *modellab.Service) {
	manager.RegisterReferenced("evaluation", "background", func(ctx context.Context, ws workspaces.Workspace, raw json.RawMessage, emit func(string, string, any)) (any, error) {
		var input struct {
			EvaluationID string `json:"evaluation_id"`
		}
		dec := json.NewDecoder(bytes.NewReader(raw))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&input); err != nil || dec.Decode(new(any)) != io.EOF || input.EvaluationID == "" || len(raw) > 512 {
			return nil, workspaces.ErrInvalid
		}
		if workspaces.Reference(ctx).ID != input.EvaluationID {
			return nil, modelstore.ErrStaleLease
		}
		err := lab.Execute(ctx, input.EvaluationID, workspaces.RunID(ctx), emit)
		ref, readErr := lab.ResultReference(context.WithoutCancel(ctx), input.EvaluationID)
		if readErr != nil {
			return nil, stderrors.Join(err, readErr)
		}
		return ref, err
	}, lab.CancelRun)
}
func initModelLab(app *App, dataDir string) {
	if !app.Config.ModelLab.Enabled || app.Workspaces == nil {
		return
	}
	store, err := modelstore.Open(filepath.Join(dataDir, "model-engineering.sqlite"))
	if err != nil {
		app.Lifecycle.SetError("model_lab", err)
		return
	}
	binary := app.Config.ModelLab.GoBinary
	if binary == "" {
		binary, _ = exec.LookPath("go")
	}
	if binary != "" && !filepath.IsAbs(binary) {
		binary = ""
	}
	var runner *evalrunner.Runner
	if binary != "" {
		runner, err = evalrunner.New(evalrunner.Config{GoBinary: binary, TempRoot: dataDir})
		if err != nil {
			log.Printf("[model_lab] trusted runner unavailable: %v", err)
		}
	}
	lab := modellab.New(store, app.Gateway)
	cfg := app.Config.ModelLab
	err = lab.ConfigureRuntime(modellab.RuntimeConfig{Workspaces: app.Workspaces, Runner: runner, TempParent: dataDir, ContextAdapter: func() *codelens.Adapter { return app.CodeLens.Load() }, Budget: modellab.Budget{MaxContext: cfg.MaxContext, MaxOutput: cfg.MaxOutput, InferenceTimeout: time.Duration(cfg.InferenceTimeoutSeconds) * time.Second, CleanupTimeout: time.Duration(cfg.CleanupTimeoutSeconds) * time.Second}, CollectEnvironment: func(ctx context.Context) (modelstore.EnvironmentSnapshot, error) {
		return collectModelEnvironment(ctx, binary)
	}})
	if err != nil {
		if runner != nil {
			runner.Close()
		}
		store.Close()
		app.Lifecycle.SetError("model_lab", err)
		return
	}
	if err = lab.ReconcileCancellations(context.Background()); err != nil {
		lab.Close()
		store.Close()
		app.Lifecycle.SetError("model_lab", err)
		return
	}
	ops, err := modelops.New(context.Background(), store)
	if err == nil {
		err = ops.Recover(context.Background())
	}
	if err != nil {
		lab.Close()
		store.Close()
		app.Lifecycle.SetError("model_lab", err)
		return
	}
	// The default is an immutable saved profile with explicit zero semantics.
	_, err = lab.SaveProfile(context.Background(), modelstore.InferenceProfile{Requested: modelstore.Options{Temperature: modelstore.Float(0), Seed: modelstore.Int(7), NumCtx: modelstore.Int(4096), MaxOutput: modelstore.Int(1024), RetryLimit: modelstore.Int(0), Parallelism: modelstore.Int(1)}})
	if err != nil {
		lab.Close()
		store.Close()
		app.Lifecycle.SetError("model_lab", err)
		return
	}
	app.ModelStore = store
	app.ModelLab = lab
	app.ModelOps = ops
	registerModelWorkers(app.Workspaces, lab)
	ops.RegisterWorkspace(app.Workspaces)
	if app.Mux != nil {
		registerModelEngineeringRoutes(app.Mux, lab, ops, app.Workspaces)
	}
	if runner == nil {
		app.Lifecycle.SetError("model_lab", fmt.Errorf("trusted Go toolchain unavailable; set model_lab.go_binary"))
	} else {
		app.Lifecycle.SetState("model_lab", core.StateRunning)
	}
}

func collectModelEnvironment(ctx context.Context, binary string) (modelstore.EnvironmentSnapshot, error) {
	e := modelstore.EnvironmentSnapshot{OS: runtime.GOOS, Arch: runtime.GOARCH, Cores: modelstore.Int(int64(runtime.NumCPU())), CollectorVersion: "modellab-devops-host-v1", VRAM: modelstore.Metric{Unit: "bytes", Source: "unavailable", Quality: "unknown", MissingReason: "physical available GPU memory collection unavailable; total adapter VRAM is recorded separately in hardware inventory"}}
	// Hardware inspection is useful even without the optional Go toolchain.
	// Only static facts enter the frozen environment; free VRAM remains unknown.
	hardware, hardwareErr := hostinfo.Collect(ctx)
	if hardwareErr != nil {
		return e, hardwareErr
	}
	e.Hardware = &hardware
	var cpus, gpus, drivers []string
	for _, cpu := range hardware.CPUs {
		if cpu.Model != "" {
			cpus = append(cpus, cpu.Model)
		}
	}
	for _, gpu := range hardware.GPUs {
		if gpu.Model != "" {
			gpus = append(gpus, gpu.Model)
		}
		if gpu.DriverVersion != "" {
			drivers = append(drivers, gpu.Model+": "+gpu.DriverVersion)
		}
	}
	e.CPU = strings.Join(cpus, "; ")
	e.GPU = strings.Join(gpus, "; ")
	e.Driver = strings.Join(drivers, "; ")
	// Keep the previous OS-usable total, rather than switching this legacy
	// field to the distinct installed DIMM capacity returned by CIM.
	if snapshot, err := metrics.NewCollector().Snapshot(); err == nil && snapshot.Memory.TotalGB > 0 {
		e.RAMBytes = modelstore.Int(int64(snapshot.Memory.TotalGB * (1 << 30)))
	}
	if binary == "" {
		return e, fmt.Errorf("trusted Go toolchain unavailable")
	}
	child, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(child, binary, "version")
	cmd.Env = []string{"GOTOOLCHAIN=local", "GOWORK=off", "GOENV=off", "CGO_ENABLED=0", "GOPROXY=off", "GOSUMDB=off"}
	raw, err := cmd.Output()
	if err != nil || len(raw) > 1024 {
		return e, fmt.Errorf("trusted Go toolchain version unavailable")
	}
	e.ToolchainVersion = strings.TrimSpace(string(raw))
	return e, nil
}

// Close cancels admission first, drains Manager-owned handlers with dependencies
// alive, then closes their evidence/toolchain and analysis resources once.
func (app *App) Close() error {
	app.closeOnce.Do(func() {
		app.startupMu.Lock()
		if app.startupCancel != nil {
			app.startupCancel()
		}
		app.startupMu.Unlock()
		if app.ModelLab != nil {
			app.ModelLab.StopAdmission()
		}
		if app.ModelOps != nil {
			app.ModelOps.StopAdmission()
		}
		if app.ModelLab != nil {
			app.ModelLab.DrainHTTP()
		}
		if app.ModelOps != nil {
			app.ModelOps.DrainHTTP()
		}
		if app.Workspaces != nil {
			app.closeErr = stderrors.Join(app.closeErr, app.Workspaces.Close())
		}
		if app.ModelOps != nil {
			app.closeErr = stderrors.Join(app.closeErr, app.ModelOps.Recover(context.Background()))
		}
		if app.ModelLab != nil {
			app.closeErr = stderrors.Join(app.closeErr, app.ModelLab.Close())
		}
		if app.ModelStore != nil {
			app.closeErr = stderrors.Join(app.closeErr, app.ModelStore.Close())
		}
		if app.Actions != nil {
			app.Actions.Close()
		}
		app.startupWG.Wait()
		if cl := app.CodeLens.Swap(nil); cl != nil {
			cl.Stop()
		}
	})
	return app.closeErr
}
