package gateway

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"github.com/noosphere/backend/internal/errors"
	"github.com/noosphere/backend/internal/gateway/providers"
	"github.com/noosphere/backend/internal/workspaces"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/noosphere/backend/internal/gateway/secure"
	"gopkg.in/yaml.v3"
)

type ModelMetadata struct {
	Vendor        string `json:"vendor,omitempty" yaml:"vendor,omitempty"`
	Family        string `json:"family,omitempty" yaml:"family,omitempty"`
	Architecture  string `json:"architecture,omitempty" yaml:"architecture,omitempty"`
	ParameterSize string `json:"parameter_size,omitempty" yaml:"parameter_size,omitempty"`
	Quantization  string `json:"quantization,omitempty" yaml:"quantization,omitempty"`
	ContextLength int    `json:"context_length,omitempty" yaml:"context_length,omitempty"`
	Source        string `json:"source,omitempty" yaml:"source,omitempty"`
}
type ModelConfig struct {
	ID            string         `json:"id" yaml:"id"`
	Name          string         `json:"name" yaml:"name"`
	Provider      string         `json:"provider" yaml:"type"`
	BaseURL       string         `json:"base_url" yaml:"base_url"`
	APIKey        string         `json:"-" yaml:"api_key"`
	ModelName     string         `json:"model_name" yaml:"model"`
	Vendor        string         `json:"vendor,omitempty" yaml:"vendor,omitempty"`
	ModelFamily   string         `json:"model_family,omitempty" yaml:"model_family,omitempty"`
	Deployment    string         `json:"deployment,omitempty" yaml:"deployment,omitempty"`
	ContextLength int            `json:"context_length,omitempty" yaml:"context_length,omitempty"`
	Metadata      *ModelMetadata `json:"metadata,omitempty" yaml:"metadata,omitempty"`
	Enabled       bool           `json:"enabled" yaml:"enabled"`
	IsDefault     bool           `json:"is_default" yaml:"is_default"`
	Temperature   float64        `json:"temperature,omitempty" yaml:"temperature,omitempty"`
	MaxTokens     int            `json:"max_tokens,omitempty" yaml:"max_tokens,omitempty"`
}

type GatewayConfig struct {
	DefaultModel string        `yaml:"default_model"`
	Models       []ModelConfig `yaml:"providers"`
}

type CallLog struct {
	ID              string  `json:"id"`
	ModelID         string  `json:"model_id"`
	Provider        string  `json:"provider"`
	Project         string  `json:"project"`
	RequestPreview  string  `json:"request_preview"`
	ResponsePreview string  `json:"response_preview"`
	TokensIn        int     `json:"tokens_in"`
	TokensOut       int     `json:"tokens_out"`
	LatencyMs       float64 `json:"latency_ms"`
	Status          string  `json:"status"`
	Error           string  `json:"error,omitempty"`
	Timestamp       string  `json:"timestamp"`
}

type Gateway struct {
	admission        inferenceAdmission
	workspaceManager *workspaces.Manager
	mu               sync.RWMutex
	providerMu       sync.Mutex
	sem              chan struct{}
	configPath       string
	config           *GatewayConfig
	encKey           []byte
	logs             []CallLog
	stats            map[string]*ModelStats
	providers        map[string]providers.LLMProvider
}

type ModelStats struct {
	Calls  int `json:"calls"`
	Tokens int `json:"tokens"`
}

func New(configPath string) *Gateway {
	key, err := secure.GetOrCreateKey()
	if err != nil {
		log.Printf("[gateway] WARNING: encryption key unavailable: %v", err)
	}
	g := &Gateway{
		configPath: configPath,
		encKey:     key,
		sem:        make(chan struct{}, 10),
		providers:  make(map[string]providers.LLMProvider),
		logs:       make([]CallLog, 0),
		stats:      make(map[string]*ModelStats),
	}
	g.loadConfig()
	return g
}

func (g *Gateway) loadConfig() {
	g.mu.Lock()
	defer g.mu.Unlock()
	data, err := os.ReadFile(g.configPath)
	if err != nil {
		log.Printf("[gateway] config read error: %v (using empty)", err)
		g.config = &GatewayConfig{DefaultModel: "", Models: []ModelConfig{}}
		return
	}
	var raw struct {
		DefaultModel string `yaml:"default_model"`
		Providers    []struct {
			ID            string         `yaml:"id"`
			Name          string         `yaml:"name"`
			Type          string         `yaml:"type"`
			APIKey        string         `yaml:"api_key"`
			BaseURL       string         `yaml:"base_url"`
			Model         string         `yaml:"model"`
			Vendor        string         `yaml:"vendor"`
			ModelFamily   string         `yaml:"model_family"`
			Deployment    string         `yaml:"deployment"`
			ContextLength int            `yaml:"context_length"`
			Metadata      *ModelMetadata `yaml:"metadata"`
			Enabled       bool           `yaml:"enabled"`
			IsDefault     bool           `yaml:"is_default"`
			Temperature   float64        `yaml:"temperature"`
			MaxTokens     int            `yaml:"max_tokens"`
		} `yaml:"providers"`
	}
	if err := yaml.Unmarshal(data, &raw); err != nil {
		log.Printf("[gateway] config parse error: %v", err)
		g.config = &GatewayConfig{DefaultModel: "", Models: []ModelConfig{}}
		return
	}
	g.config = &GatewayConfig{DefaultModel: raw.DefaultModel}
	for _, p := range raw.Providers {
		apiKey := p.APIKey
		if apiKey != "" && g.encKey != nil {
			if dec, err := secure.Decrypt(g.encKey, apiKey); err == nil {
				apiKey = dec
			}
		}
		g.config.Models = append(g.config.Models, ModelConfig{
			ID: p.ID, Name: p.Name, Provider: p.Type,
			BaseURL: p.BaseURL, APIKey: apiKey, ModelName: p.Model,
			Vendor: p.Vendor, ModelFamily: p.ModelFamily, Deployment: p.Deployment,
			ContextLength: p.ContextLength, Metadata: p.Metadata,
			Enabled: p.Enabled, IsDefault: p.ID == raw.DefaultModel,
			Temperature: p.Temperature, MaxTokens: p.MaxTokens,
		})
	}
}

func (g *Gateway) saveConfig() error {
	// Create encrypted copy for persistence
	encrypted := *g.config
	encrypted.Models = make([]ModelConfig, len(g.config.Models))
	for i, m := range g.config.Models {
		encrypted.Models[i] = m
		if m.APIKey != "" && g.encKey != nil {
			if enc, err := secure.Encrypt(g.encKey, m.APIKey); err == nil {
				encrypted.Models[i].APIKey = enc
			} else {
				return fmt.Errorf("encrypt model credential: %w", err)
			}
		} else if m.APIKey != "" {
			return fmt.Errorf("credential encryption unavailable; configuration was not saved")
		}
	}
	data, err := yaml.Marshal(&encrypted)
	if err != nil {
		return err
	}
	// Replace only after a complete write; a failed save must leave the last
	// usable configuration on disk intact.
	file, err := os.CreateTemp(filepath.Dir(g.configPath), ".models-*.yaml")
	if err != nil {
		return err
	}
	name := file.Name()
	defer os.Remove(name)
	if _, err = file.Write(data); err != nil {
		file.Close()
		return err
	}
	if err = file.Sync(); err != nil {
		file.Close()
		return err
	}
	if err = file.Close(); err != nil {
		return err
	}
	return os.Rename(name, g.configPath)
}

func maskModels(models []ModelConfig) []ModelConfig {
	result := make([]ModelConfig, len(models))
	copy(result, models)
	for i := range result {
		result[i].APIKey = ""
	}
	return result
}

// ReloadConfig reloads models.yaml at runtime.
func (g *Gateway) ReloadConfig() {
	log.Printf("[gateway] reloading config: %s", g.configPath)
	g.loadConfig()
}

// HandleReload handles POST /api/gateway/reload
func (g *Gateway) HandleReload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusMethodNotAllowed)
		w.Write([]byte(`{"error":"method not allowed"}`))
		return
	}
	g.ReloadConfig()
	g.mu.RLock()
	defer g.mu.RUnlock()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status": "ok",
		"models": len(g.config.Models),
	})
}

func (g *Gateway) HandleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"status": "ok", "service": "ai-gateway", "embedded": "true",
	})
}

// HandleListModels handles GET /api/gateway/models — returns all configured
// models with API keys masked.
func (g *Gateway) HandleListModels(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	g.mu.RLock()
	defer g.mu.RUnlock()
	json.NewEncoder(w).Encode(map[string]interface{}{
		"models":        maskModels(g.config.Models),
		"default_model": g.config.DefaultModel,
		"count":         len(g.config.Models),
	})
}

// HandleDeleteModel handles DELETE /api/gateway/models/{id}.
func (g *Gateway) HandleDeleteModel(w http.ResponseWriter, r *http.Request) {
	modelID := r.PathValue("id")
	w.Header().Set("Content-Type", "application/json")

	g.mu.Lock()
	defer g.mu.Unlock()
	previous := g.config
	next := *previous
	g.config = &next
	filtered := make([]ModelConfig, 0)
	for _, m := range g.config.Models {
		if m.ID != modelID {
			filtered = append(filtered, m)
		}
	}
	g.config.Models = filtered
	if g.config.DefaultModel == modelID {
		g.config.DefaultModel = ""
	}
	if err := g.saveConfig(); err != nil {
		g.config = previous
		errors.WriteHTTP(w, r, errors.Wrap(err, errors.ErrInternal, errors.CatGateway, "failed to save model configuration"))
		return
	}
	json.NewEncoder(w).Encode(map[string]interface{}{"deleted": modelID})
}

// HandleAddModel handles POST /api/gateway/models — adds or updates a model
// (matched by ID).
func (g *Gateway) HandleAddModel(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ID            string         `json:"id"`
		Name          string         `json:"name"`
		Provider      string         `json:"provider"`
		BaseURL       string         `json:"base_url"`
		APIKey        string         `json:"api_key"`
		ModelName     string         `json:"model_name"`
		Vendor        string         `json:"vendor"`
		ModelFamily   string         `json:"model_family"`
		Deployment    string         `json:"deployment"`
		ContextLength int            `json:"context_length"`
		Metadata      *ModelMetadata `json:"metadata"`
		IsDefault     bool           `json:"is_default"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		errors.WriteHTTP(w, r, errors.InvalidInput(errors.CatGateway, "invalid request body"))
		return
	}
	if body.ID == "" {
		body.ID = body.Name
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	previous := g.config
	next := *previous
	next.Models = append([]ModelConfig(nil), previous.Models...)
	g.config = &next
	found := false
	for i, m := range g.config.Models {
		if m.ID == body.ID {
			if body.APIKey == "" {
				body.APIKey = m.APIKey
			}
			identityChanged := body.Provider != m.Provider || strings.TrimRight(body.BaseURL, "/") != strings.TrimRight(m.BaseURL, "/") || body.ModelName != m.ModelName
			if identityChanged && (body.Metadata == nil || (m.Metadata != nil && *body.Metadata == *m.Metadata)) {
				// Metadata belongs to its deployment. An ordinary edit must not
				// carry the previous model's discovered identity to a new target.
				body.Metadata = nil
			} else if body.Metadata == nil {
				body.Metadata = m.Metadata
			}
			if identityChanged && body.ContextLength == m.ContextLength {
				body.ContextLength = 0
			} else if !identityChanged && body.ContextLength == 0 {
				body.ContextLength = m.ContextLength
			}
			g.config.Models[i] = ModelConfig{
				ID: body.ID, Name: body.Name, Provider: body.Provider,
				BaseURL: body.BaseURL, APIKey: body.APIKey, ModelName: body.ModelName,
				Vendor: body.Vendor, ModelFamily: body.ModelFamily, Deployment: body.Deployment,
				ContextLength: body.ContextLength, Metadata: body.Metadata,
				Enabled: m.Enabled, IsDefault: body.IsDefault,
				Temperature: m.Temperature, MaxTokens: m.MaxTokens,
			}
			found = true
			break
		}
	}
	if !found {
		g.config.Models = append(g.config.Models, ModelConfig{
			ID: body.ID, Name: body.Name, Provider: body.Provider,
			BaseURL: body.BaseURL, APIKey: body.APIKey, ModelName: body.ModelName,
			Vendor: body.Vendor, ModelFamily: body.ModelFamily, Deployment: body.Deployment,
			ContextLength: body.ContextLength, Metadata: body.Metadata,
			Enabled: true, IsDefault: body.IsDefault,
		})
	}
	if body.IsDefault {
		g.config.DefaultModel = body.ID
	}
	for i := range g.config.Models {
		g.config.Models[i].IsDefault = g.config.Models[i].ID == g.config.DefaultModel
	}
	if err := g.saveConfig(); err != nil {
		g.config = previous
		errors.WriteHTTP(w, r, errors.Wrap(err, errors.ErrInternal, errors.CatGateway, "failed to save model configuration"))
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	for _, saved := range g.config.Models {
		if saved.ID == body.ID {
			json.NewEncoder(w).Encode(map[string]interface{}{"added": saved, "status": "ok"})
			return
		}
	}
}

func (g *Gateway) HandleStats(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	g.mu.RLock()
	defer g.mu.RUnlock()
	projectStats := make(map[string]int)
	for _, l := range g.logs {
		projectStats[l.Project]++
	}
	json.NewEncoder(w).Encode(map[string]interface{}{
		"total_models": len(g.config.Models), "total_calls": len(g.logs),
		"total_tokens": g.totalTokens(), "total_logs": len(g.logs),
		"by_project": projectStats,
	})
}

func (g *Gateway) HandleLogs(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	g.mu.RLock()
	defer g.mu.RUnlock()
	json.NewEncoder(w).Encode(map[string]interface{}{"logs": g.logs, "count": len(g.logs)})
}

func (g *Gateway) testModel(model *ModelConfig) map[string]interface{} {
	return g.testModelContext(context.Background(), model)
}

func (g *Gateway) testModelContext(ctx context.Context, model *ModelConfig) map[string]interface{} {
	start := time.Now()
	content, _, _, err := g.callLLMContext(ctx, model, []map[string]string{{"role": "user", "content": "Reply with OK only."}}, "", 0, 128)
	latency := time.Since(start).Seconds() * 1000
	if err != nil {
		return map[string]interface{}{"status": "error", "model": model.ModelName, "latency_ms": latency, "error": err.Error()}
	}
	if strings.TrimSpace(content) == "" {
		return map[string]interface{}{"status": "error", "model": model.ModelName, "error": "模型未返回正文，请检查模型与输出设置"}
	}
	return map[string]interface{}{"status": "ok", "model": model.ModelName, "provider": model.Provider, "latency_ms": latency}
}

func (g *Gateway) callLLM(model *ModelConfig, messages []map[string]string, sysPrompt string, temp float64, maxTokens int) (string, int, int, error) {
	return g.callLLMContext(context.Background(), model, messages, sysPrompt, temp, maxTokens)
}

func (g *Gateway) callLLMContext(parent context.Context, model *ModelConfig, messages []map[string]string, sysPrompt string, temp float64, maxTokens int) (string, int, int, error) {
	ctx, cancel := context.WithTimeout(parent, 120*time.Second)
	defer cancel()
	foregroundDone, err := g.beginInteractive(ctx)
	if err != nil {
		return "", 0, 0, err
	}
	defer foregroundDone()
	if err := g.acquireCall(ctx); err != nil {
		return "", 0, 0, err
	}
	defer func() { <-g.sem }()

	const maxRetries = 2
	var lastErr error
	for attempt := 0; attempt <= maxRetries; attempt++ {
		if err := ctx.Err(); err != nil {
			return "", 0, 0, err
		}
		if attempt > 0 {
			backoff := time.Duration(1<<uint(attempt-1)) * time.Second
			log.Printf("[gateway] retry %d/%d for %s", attempt, maxRetries, model.ID)
			select {
			case <-time.After(backoff):
			case <-ctx.Done():
				return "", 0, 0, ctx.Err()
			}
		}
		content, tokensIn, tokensOut, err := g.callLLMOnce(ctx, model, messages, sysPrompt, temp, maxTokens)
		if err == nil {
			return content, tokensIn, tokensOut, nil
		}
		lastErr = err
		if ctx.Err() != nil {
			return "", 0, 0, ctx.Err()
		}
		if !isRetryable(err) {
			break
		}
	}
	return "", 0, 0, errors.Wrap(lastErr, errors.ErrUnavailable, errors.CatGateway, "LLM call failed after retries")
}

func (g *Gateway) callLLMOnce(ctx context.Context, model *ModelConfig, messages []map[string]string, sysPrompt string, temp float64, maxTokens int) (string, int, int, error) {
	provider, err := g.getOrCreateProvider(model)
	if err != nil {
		return "", 0, 0, errors.Wrap(err, errors.ErrInternal, errors.CatGateway, "provider init failed")
	}
	msgs := make([]providers.Message, len(messages))
	for i, m := range messages {
		msgs[i] = providers.Message{Role: m["role"], Content: m["content"]}
	}
	if maxTokens == 0 {
		maxTokens = 4096
	}
	if model.MaxTokens > 0 {
		maxTokens = model.MaxTokens
	}
	finishInference := g.countInference(false)
	defer finishInference()
	resp, err := provider.Chat(ctx, model.ModelName, msgs, providers.ChatOptions{
		SystemPrompt: sysPrompt,
		Temperature:  temp,
		MaxTokens:    maxTokens,
	})
	if err != nil {
		return "", 0, 0, errors.Wrap(err, errors.ErrUnavailable, errors.CatGateway, "provider chat failed")
	}
	return resp.Content, resp.TokensIn, resp.TokensOut, nil
}

func (g *Gateway) totalTokens() int {
	total := 0
	for _, l := range g.logs {
		total += l.TokensIn + l.TokensOut
	}
	return total
}

func (g *Gateway) preview(msgs []map[string]string) string {
	if len(msgs) == 0 {
		return ""
	}
	return previewText(msgs[len(msgs)-1]["content"])
}

// GetFirstModel returns the first enabled model, or nil.
func (g *Gateway) GetFirstModel() *ModelConfig {
	return g.findModel("")
}

// Chat sends a chat request and returns the content.
func (g *Gateway) Chat(model *ModelConfig, messages []map[string]string, sysPrompt string, temp float64, maxT int) (string, int, int, error) {
	return g.callLLM(model, messages, sysPrompt, temp, maxT)
}

func (g *Gateway) ChatContext(ctx context.Context, model *ModelConfig, messages []map[string]string, sysPrompt string, temp float64, maxT int) (string, int, int, error) {
	return g.callLLMContext(ctx, model, messages, sysPrompt, temp, maxT)
}

// ResolveEmbeddingKey looks up an API key for embedding from the first enabled
// model matching the given provider type (openai, openai_compatible, etc.).
// Returns apiKey, baseURL, and a found flag.
func (g *Gateway) ResolveEmbeddingKey(providerType string) (string, string, bool) {
	g.mu.RLock()
	defer g.mu.RUnlock()
	for _, m := range g.config.Models {
		if !m.Enabled {
			continue
		}
		if strings.EqualFold(m.Provider, providerType) ||
			(providerType == "openai" && strings.EqualFold(m.Provider, "openai_compatible")) {
			if m.APIKey != "" {
				return m.APIKey, m.BaseURL, true
			}
		}
	}
	return "", "", false
}

// getOrCreateProvider returns a cached provider or creates a new one from model config.
func providerCacheKey(model *ModelConfig) string {
	digest := sha256.Sum256([]byte(model.Provider + "\x00" + model.BaseURL + "\x00" + model.APIKey))
	return fmt.Sprintf("%s:%x", model.ID, digest)
}

func (g *Gateway) getOrCreateProvider(model *ModelConfig) (providers.LLMProvider, error) {
	g.providerMu.Lock()
	defer g.providerMu.Unlock()

	// Cache per configuration and credential revision. Two accounts can share an
	// endpoint; switching or rotating a key must never reuse the old Authorization.
	key := providerCacheKey(model)
	if p, ok := g.providers[key]; ok {
		return p, nil
	}

	factory := providers.Get(model.Provider)
	if factory == nil {
		// Fallback to OpenAI-compatible for unknown providers
		factory = providers.Get("openai_compatible")
	}
	if factory == nil {
		return nil, fmt.Errorf("no provider factory for: %s", model.Provider)
	}

	p, err := factory(model.BaseURL, model.APIKey)
	if err != nil {
		return nil, err
	}

	for cached := range g.providers {
		if strings.HasPrefix(cached, model.ID+":") {
			delete(g.providers, cached)
		}
	}
	g.providers[key] = p
	return p, nil
}

// isRetryable returns true for transient errors.
func isRetryable(err error) bool {
	if err == nil {
		return false
	}
	s := err.Error()
	if strings.Contains(s, "connection refused") || strings.Contains(s, "timeout") || strings.Contains(s, "EOF") || strings.Contains(s, "connection reset") || strings.Contains(s, "TLS handshake") {
		return true
	}
	if strings.Contains(s, "provider returned 5") || strings.Contains(s, "429") {
		return true
	}
	return false
}
