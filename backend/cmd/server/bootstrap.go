package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/noosphere/backend/internal/agent"
	"github.com/noosphere/backend/internal/codelens"
	"github.com/noosphere/backend/internal/config"
	"github.com/noosphere/backend/internal/core"
	devopsAgent "github.com/noosphere/backend/internal/devops/agent"
	devopsAPI "github.com/noosphere/backend/internal/devops/api"
	"github.com/noosphere/backend/internal/errors"
	"github.com/noosphere/backend/internal/gateway"
	"github.com/noosphere/backend/internal/knowledge"
	"github.com/noosphere/backend/internal/modellab"
	"github.com/noosphere/backend/internal/modelops"
	"github.com/noosphere/backend/internal/modelstore"
	nebulaAPI "github.com/noosphere/backend/internal/nebula/api"
	"github.com/noosphere/backend/internal/nebula/engine"
	"github.com/noosphere/backend/internal/nebula/engine/embed"
	"github.com/noosphere/backend/internal/workspaces"
)

// App holds all runtime dependencies wired during bootstrap.
type App struct {
	Config        *config.AppConfig
	Lifecycle     *core.Lifecycle
	Gateway       *gateway.Gateway
	Engine        *engine.Engine
	CodeLens      atomic.Pointer[codelens.Adapter]
	Mux           *http.ServeMux
	Workspaces    *workspaces.Manager
	Actions       *agent.Actions
	ModelLab      *modellab.Service
	ModelOps      *modelops.Service
	ModelStore    *modelstore.Store
	startupCtx    context.Context
	startupCancel context.CancelFunc
	startupMu     sync.Mutex
	startupWG     sync.WaitGroup
	closeOnce     sync.Once
	closeErr      error
}

// bootstrap initializes all modules and wires routes onto the mux.
func bootstrap(cfg *config.AppConfig) *App {
	lc := core.New()
	mux := http.NewServeMux()

	// 统一 404/405 出口: 未匹配路由与错误方法均返回 AppError JSON (而非默认纯文本)
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		errors.WriteHTTP(w, r, errors.NotFound(errors.CatSystem, "route not found: "+r.Method+" "+r.URL.Path))
	})

	// Gateway (AI LLM gateway)
	gw := gateway.New(cfg.Gateway.ConfigPath)
	lc.SetState("gateway", core.StateRunning)

	// Nebula (memory engine)
	eng := initNebula(cfg, lc, mux, gw)

	// DevOps (system agent)
	initDevOps(cfg, lc, mux, eng, gw)

	// CodeLens (code analysis) — non-blocking, worker starts in background
	app := &App{
		Config:    cfg,
		Lifecycle: lc,
		Gateway:   gw,
		Engine:    eng,
		Mux:       mux,
	}
	app.startupCtx, app.startupCancel = context.WithCancel(context.Background())
	initCodeLens(cfg, lc, mux, &app.CodeLens, eng, app)
	dataDir := cfg.App.DataDir
	if dataDir == "" {
		dataDir = filepath.Dir(cfg.Modules.Nebula.DataDir)
	}
	manager, err := workspaces.Open(filepath.Join(dataDir, "workspace.sqlite"))
	if err != nil {
		lc.SetError("workspaces", err)
		unavailable := func(w http.ResponseWriter, r *http.Request) {
			errors.WriteHTTP(w, r, errors.New(errors.ErrUnavailable, errors.CatSystem, "workspace storage unavailable"))
		}
		mux.HandleFunc("/api/workspaces", unavailable)
		mux.HandleFunc("/api/workspaces/", unavailable)
		mux.HandleFunc("/api/runs/", unavailable)
	} else {
		app.Workspaces = manager
		gw.SetWorkspaceManager(manager)
		manager.RegisterRoutes(mux)
		knowledgeService := knowledge.New(manager, eng)
		knowledgeService.RegisterRoutes(mux)
		actions, actionErr := agent.OpenActions(filepath.Join(dataDir, "agent-actions"), manager, agent.NewNativeExecutor(cfg.Modules.CodeLens.PythonPath, cfg.Modules.DevOps.Enabled))
		if actionErr != nil {
			lc.SetError("agent_actions", actionErr)
		} else {
			app.Actions = actions
			actions.RegisterRoutes(mux)
			lc.SetState("agent_actions", core.StateRunning)
		}
		registerAgentWorker(manager, gw, knowledgeService, cfg.Modules.DevOps.Enabled, actions)
		registerWorkspaceWorkers(manager, mux, eng, gw, cfg.Modules.CodeLens.Enabled, cfg.Modules.DevOps.Enabled)
		initModelLab(app, dataDir)
		lc.SetState("workspaces", core.StateRunning)
	}

	// 路由总表 (gateway + health) — 全部端点见 router.go 注释
	registerRoutes(mux, lc, gw)

	return app
}

func initNebula(cfg *config.AppConfig, lc *core.Lifecycle, mux *http.ServeMux, gw *gateway.Gateway) *engine.Engine {
	if !cfg.Modules.Nebula.Enabled {
		return nil
	}

	lc.SetState("nebula", core.StateStarting)
	eng, err := engine.Open(engine.DefaultOptions(cfg.Modules.Nebula.DataDir))
	if err != nil {
		log.Printf("[nebula] engine open error: %v", err)
		lc.SetError("nebula", err)
		return nil
	}

	ns := nebulaAPI.NewServer(eng)
	ns.RegisterRoutes(mux)
	lc.SetState("nebula", core.StateRunning)

	if cfg.Modules.Nebula.Embedding.Provider == "openai" {
		// The lexical engine is already usable; remote cache rebuild must not
		// delay HTTP readiness past the desktop watchdog's startup deadline.
		go initNebulaEmbedding(cfg, eng, gw)
	}

	return eng
}

func initNebulaEmbedding(cfg *config.AppConfig, eng *engine.Engine, gw *gateway.Gateway) {
	apiKey := cfg.Modules.Nebula.Embedding.APIKey
	baseURL := cfg.Modules.Nebula.Embedding.BaseURL
	model := cfg.Modules.Nebula.Embedding.Model

	if baseURL == "" {
		baseURL = "https://api.openai.com/v1"
	}
	if model == "" {
		model = "text-embedding-3-small"
	}

	// Auto-resolve API key from Gateway if not set in config
	if apiKey == "" && gw != nil {
		if gwKey, gwBase, ok := gw.ResolveEmbeddingKey("openai"); ok {
			if strings.Contains(gwBase, "api.openai.com") {
				apiKey = gwKey
				baseURL = gwBase
				log.Printf("[nebula] embedding: auto-resolved OpenAI API key from Gateway")
			} else {
				log.Printf("[nebula] embedding: Gateway has model but base_url is not OpenAI (%s). "+
					"Embeddings may not be supported by this provider. Keeping mock embedder.", gwBase)
			}
		}
	}

	if apiKey != "" {
		emb := embed.NewOpenAIEmbedder(baseURL, apiKey, model)
		if err := eng.SetEmbedder(emb); err != nil {
			log.Printf("[nebula] embedding rebuild failed; retaining lexical embedder: %v", err)
			return
		}
		log.Printf("[nebula] embedding: openai/%s ready (base=%s)", model, baseURL)
	} else {
		log.Printf("[nebula] WARNING: embedding=openai but no OpenAI API key. " +
			"Add an OpenAI model in models.yaml or set api_key in config.yaml. Using mock embedder.")
	}
}

func initDevOps(cfg *config.AppConfig, lc *core.Lifecycle, mux *http.ServeMux, eng *engine.Engine, gw *gateway.Gateway) {
	if !cfg.Modules.DevOps.Enabled {
		return
	}

	lc.SetState("devops", core.StateStarting)

	// Bridge function: DevOps Agent calls Gateway for AI diagnosis
	chatFn := func(ctx context.Context, messages []map[string]string) (string, error) {
		if gw == nil {
			return "", fmt.Errorf("gateway not ready")
		}
		m := gw.GetFirstModel()
		if m == nil {
			return "", fmt.Errorf("no model configured")
		}
		content, _, _, err := gw.ChatContext(ctx, m, messages, "", 0.1, 1024)
		return content, err
	}

	dAgent := devopsAgent.New(eng, chatFn)
	dHandler := devopsAPI.NewHandler(dAgent)
	dHandler.RegisterRoutes(mux)
	lc.SetState("devops", core.StateRunning)
}

func initCodeLens(cfg *config.AppConfig, lc *core.Lifecycle, mux *http.ServeMux, clPtr *atomic.Pointer[codelens.Adapter], eng *engine.Engine, app *App) {
	if !cfg.Modules.CodeLens.Enabled {
		return
	}

	lc.SetState("codelens", core.StateStarting)

	// Register lazy proxy route immediately — returns 404 until the Python
	// worker is ready. Server startup is never blocked by Python detection.
	mux.HandleFunc("/api/v1/codelens/", func(w http.ResponseWriter, r *http.Request) {
		cl := clPtr.Load()
		if cl == nil || cl.Proxy() == nil {
			http.NotFound(w, r)
			return
		}
		cl.Proxy().ServeHTTP(w, r)
	})

	// The desktop keeps writable data separate from read-only application resources.
	workingDir, _ := os.Getwd()
	executable, _ := os.Executable()
	scriptCandidates, err := resolveCodeLensScripts(os.Getenv("NOOSPHERE_CODELENS_SCRIPT"), workingDir, executable)
	if err != nil {
		lc.SetError("codelens", err)
		return
	}
	pythonCmds := resolveCodeLensPython(cfg.Modules.CodeLens.PythonPath, scriptCandidates)

	// Detect Python in background — CodeLens comes online when found.
	app.startupWG.Add(1)
	go func() {
		defer app.startupWG.Done()
		var lastError error
		for _, pyCmd := range pythonCmds {
			for _, sp := range scriptCandidates {
				if app.startupCtx.Err() != nil {
					return
				}
				clAdapter := codelens.NewAdapter(append([]string{}, pyCmd...), sp, cfg.Modules.CodeLens.Port)
				clAdapter.ModifyResponse = codelens.PersistIndexResponse(eng)
				lastError = clAdapter.Start()
				if lastError == nil {
					app.startupMu.Lock()
					if app.startupCtx.Err() != nil {
						app.startupMu.Unlock()
						clAdapter.Stop()
						return
					}
					clPtr.Store(clAdapter)
					app.startupMu.Unlock()
					lc.SetState("codelens", core.StateRunning)
					log.Printf("[codelens] worker ready: %v %s", pyCmd, sp)
					return
				}
				log.Printf("[codelens] retry failed: %v %s: %v", pyCmd, sp, lastError)
			}
		}
		lc.SetError("codelens", fmt.Errorf("CodeLens worker failed: %v; check the configured Python/port, or run setup.bat to install dependencies", lastError))
	}()
}
