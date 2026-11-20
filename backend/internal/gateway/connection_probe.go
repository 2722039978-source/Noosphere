package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"syscall"
	"time"
	"unicode"

	"github.com/noosphere/backend/internal/gateway/providers"
)

const probeTimeout = 100 * time.Second

type ProbeModel struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Loaded *bool  `json:"loaded,omitempty"`
}

type ProbeCheck struct {
	Stage      string `json:"stage"`
	Method     string `json:"method"`
	Endpoint   string `json:"endpoint"`
	Status     string `json:"status"`
	Code       string `json:"code,omitempty"`
	HTTPStatus int    `json:"http_status,omitempty"`
}

type ModelDiagnostic struct {
	Status         string       `json:"status"`
	Provider       string       `json:"provider"`
	Model          string       `json:"model"`
	ReportedModel  string       `json:"reported_model,omitempty"`
	Endpoint       string       `json:"endpoint"`
	Service        string       `json:"service"`
	ModelDiscovery string       `json:"model_discovery"`
	ModelLoaded    string       `json:"model_loaded"`
	Inference      string       `json:"inference"`
	Stage          string       `json:"stage"`
	Code           string       `json:"code"`
	Error          string       `json:"error,omitempty"`
	HTTPStatus     int          `json:"http_status,omitempty"`
	LatencyMS      float64      `json:"latency_ms"`
	Models         []ProbeModel `json:"models"`
	Checks         []ProbeCheck `json:"checks"`
}

type probeEndpoints struct {
	health, catalog, residency, inference string
	legacyResidency                       string
	format                                string
	local, authoritative                  bool
}

func connectionEndpoints(model *ModelConfig) (probeEndpoints, string) {
	base := strings.TrimRight(model.BaseURL, "/")
	switch model.Provider {
	case "claude", "anthropic":
		if base == "" {
			base = "https://api.anthropic.com/v1"
		}
	case "gemini", "google":
		if base == "" {
			base = "https://generativelanguage.googleapis.com/v1beta"
		}
	}
	if providers.Get(model.Provider) == nil {
		return probeEndpoints{}, "PROVIDER_UNSUPPORTED"
	}
	u, err := url.Parse(base)
	if err != nil || u.Hostname() == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.RawQuery != "" || u.ForceQuery || strings.Contains(base, "#") {
		return probeEndpoints{}, "ADDRESS_INVALID"
	}
	if strings.HasSuffix(u.Path, "/chat/completions") || strings.HasSuffix(u.Path, "/generateContent") || strings.HasSuffix(u.Path, "/messages") {
		return probeEndpoints{}, "BASE_URL_REQUIRED"
	}
	origin := u.Scheme + "://" + u.Host
	ip := net.ParseIP(u.Hostname())
	local := strings.EqualFold(u.Hostname(), "localhost") || (ip != nil && ip.IsLoopback())
	endpoints := probeEndpoints{catalog: base + "/models", inference: base + "/chat/completions", format: "openai", local: local}
	switch model.Provider {
	case "ollama":
		apiBase := base
		if strings.HasSuffix(u.Path, "/v1") || strings.HasSuffix(u.Path, "/api") {
			apiBase = base[:strings.LastIndex(base, "/")]
		}
		endpoints.health = apiBase + "/api/version"
		endpoints.catalog = apiBase + "/api/tags"
		endpoints.residency = apiBase + "/api/ps"
		endpoints.inference = providers.OllamaCompatibilityBase(base) + "/chat/completions"
		endpoints.format, endpoints.authoritative = "ollama", true
	case "claude", "anthropic":
		endpoints.inference = base + "/messages"
		endpoints.format = "claude"
	case "gemini", "google":
		endpoints.inference = providers.GeminiGenerateURL(base, model.ModelName)
		endpoints.format = "gemini"
	case "openvino":
		native, err := providers.NormalizeEvaluationEndpoint(base)
		if err != nil {
			return probeEndpoints{}, "LOCAL_ADDRESS_REQUIRED"
		}
		endpoints.health, endpoints.catalog = native+"/health", native+"/v1/models"
		endpoints.inference = native + "/noosphere/openvino/evaluate"
		endpoints.authoritative = true
	case "openai_compatible":
		if local && u.Port() == "1234" && (u.Path == "" || u.Path == "/v1") {
			endpoints.residency = origin + "/api/v1/models"
			endpoints.legacyResidency = origin + "/api/v0/models"
		}
		if local && (u.Port() == "18787" || u.Port() == "18788") && (u.Path == "" || u.Path == "/v1") {
			endpoints.health = origin + "/health"
			endpoints.authoritative = true
		}
	}
	return endpoints, ""
}

func sanitizeProbeField(value, apiKey string) string {
	if apiKey != "" {
		value = strings.ReplaceAll(value, apiKey, "[redacted]")
	}
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, value)
}

var probeTransport = http.DefaultTransport.(*http.Transport).Clone()

func probeJSON(ctx context.Context, endpoint string, model *ModelConfig, local bool, payload []byte) (json.RawMessage, int, error) {
	transport := probeTransport
	if local {
		transport = localProbeTransport
	}
	method, timeout := http.MethodGet, 4*time.Second
	if payload != nil {
		method, timeout = http.MethodPost, 80*time.Second
	}
	client := http.Client{Transport: transport, Timeout: timeout, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, bytes.NewReader(payload))
	if err != nil {
		return nil, 0, err
	}
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	switch model.Provider {
	case "claude", "anthropic":
		req.Header.Set("anthropic-version", "2023-06-01")
		req.Header.Set("x-api-key", model.APIKey)
	case "gemini", "google":
		req.Header.Set("x-goog-api-key", model.APIKey)
	default:
		if model.APIKey != "" {
			req.Header.Set("Authorization", "Bearer "+model.APIKey)
		}
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	const limit = 1024 * 1024
	body, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil || len(body) > limit {
		return nil, resp.StatusCode, errors.New("invalid provider response")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, resp.StatusCode, &providers.UpstreamHTTPError{Status: resp.StatusCode, Detail: string(body)}
	}
	if !json.Valid(body) {
		return nil, resp.StatusCode, errors.New("invalid provider JSON response")
	}
	return body, resp.StatusCode, nil
}

var localProbeTransport = func() *http.Transport {
	transport := probeTransport.Clone()
	transport.Proxy = nil
	return transport
}()

func probeModelID(provider, name string) string {
	switch provider {
	case "ollama":
		if !strings.Contains(name[strings.LastIndex(name, "/")+1:], ":") {
			return name + ":latest"
		}
	case "gemini", "google":
		return strings.TrimPrefix(name, "models/")
	}
	return name
}

func probeCatalog(body json.RawMessage, format string) ([]ProbeModel, bool) {
	var catalog struct {
		Data []struct {
			ID, Name, Type, State string
			DisplayName           string `json:"display_name"`
		} `json:"data"`
		Models []struct {
			Name, Model                string
			DisplayName                string   `json:"displayName"`
			SupportedGenerationMethods []string `json:"supportedGenerationMethods"`
			Type, Key                  string
			LMDisplayName              string                `json:"display_name"`
			LoadedInstances            []struct{ ID string } `json:"loaded_instances"`
		} `json:"models"`
	}
	if json.Unmarshal(body, &catalog) != nil {
		return nil, false
	}
	models := make([]ProbeModel, 0)
	switch format {
	case "lm_studio_v1":
		if catalog.Models == nil {
			return nil, false
		}
		for _, row := range catalog.Models {
			if row.Type != "llm" || row.LoadedInstances == nil || row.Key == "" {
				continue
			}
			if len(row.LoadedInstances) == 0 {
				loaded := false
				models = append(models, ProbeModel{ID: row.Key, Name: row.LMDisplayName, Loaded: &loaded})
				continue
			}
			for _, instance := range row.LoadedInstances {
				loaded := true
				models = append(models, ProbeModel{ID: instance.ID, Name: row.LMDisplayName, Loaded: &loaded})
			}
		}
	case "ollama", "gemini":
		if catalog.Models == nil {
			return nil, false
		}
		for _, model := range catalog.Models {
			id := model.Name
			if id == "" {
				id = model.Model
			}
			if format == "gemini" {
				if len(model.SupportedGenerationMethods) > 0 && !slices.Contains(model.SupportedGenerationMethods, "generateContent") {
					continue
				}
				id = strings.TrimPrefix(id, "models/")
			}
			models = append(models, ProbeModel{ID: id, Name: model.DisplayName})
		}
	default:
		if catalog.Data == nil {
			return nil, false
		}
		for _, model := range catalog.Data {
			if format == "lm_studio" && model.Type == "embeddings" {
				continue
			}
			name := model.DisplayName
			if name == "" {
				name = model.Name
			}
			row := ProbeModel{ID: model.ID, Name: name}
			if model.State == "loaded" || model.State == "not-loaded" {
				loaded := model.State == "loaded"
				row.Loaded = &loaded
			}
			models = append(models, row)
		}
	}
	filtered := models[:0]
	seen := make(map[string]bool)
	for _, model := range models {
		if model.ID == "" || len(model.ID) > 512 || strings.IndexFunc(model.ID, unicode.IsControl) >= 0 || seen[model.ID] {
			continue
		}
		seen[model.ID] = true
		if model.Name == "" || len(model.Name) > 512 {
			model.Name = model.ID
		}
		filtered = append(filtered, model)
	}
	return filtered, true
}

type probeProblem struct {
	stage, code, message string
	status               int
}

func problem(stage, code, message string) probeProblem {
	return probeProblem{stage: stage, code: code, message: message}
}

func connectionProblem(err error, listMissing bool) probeProblem {
	var upstream *providers.UpstreamHTTPError
	var network net.Error
	var dns *net.DNSError
	status := 0
	detail := strings.ToLower(err.Error())
	if errors.As(err, &upstream) {
		status = upstream.Status
		detail = strings.ToLower(upstream.Detail)
	}
	var native *providers.EvaluationError
	if errors.As(err, &native) {
		status = native.HTTPStatus
	}
	var out probeProblem
	switch {
	case errors.Is(err, context.Canceled):
		out = problem("inference", "PROBE_CANCELLED", "验证已取消，可重新验证。")
	case status == 401:
		out = problem("authentication", "AUTHENTICATION_FAILED", "服务拒绝鉴权。请更新 API 密钥或本地服务令牌后重试。")
	case status == 403:
		out = problem("authentication", "PERMISSION_DENIED", "此凭据没有访问该接口或模型的权限，请核对服务端授权。")
	case status == 402:
		out = problem("authentication", "PAYMENT_REQUIRED", "服务拒绝请求，需检查账号余额或付费权限。")
	case status == 429:
		out = problem("inference", "RATE_LIMITED", "服务达到请求或额度限制，稍后重试或核对账号额度。")
	case errors.Is(err, context.DeadlineExceeded) || (errors.As(err, &network) && network.Timeout()):
		out = problem("inference", "REQUEST_TIMEOUT", "服务未在限时内响应。首次加载较慢时可等待模型就绪后再验证。")
	case errors.As(err, &dns):
		out = problem("service", "HOST_NOT_FOUND", "无法解析服务主机名，请检查地址和网络。")
	case errors.Is(err, syscall.ECONNREFUSED) || strings.Contains(detail, "connection refused") || strings.Contains(detail, "actively refused"):
		out = problem("service", "CONNECTION_REFUSED", "该地址和端口没有可连接的服务。请启动本地引擎或检查端口。")
	case strings.Contains(detail, "certificate") || strings.Contains(detail, "tls handshake"):
		out = problem("service", "TLS_ERROR", "无法建立安全连接，请检查 HTTPS 地址、证书和代理。")
	case status >= 300 && status < 400:
		out = problem("service", "ENDPOINT_REDIRECTS", "服务把请求转向了其他地址，请直接填写最终 API 地址。")
	case strings.Contains(detail, "out of memory") || strings.Contains(detail, "requires more system memory") || strings.Contains(detail, "cuda error: memory allocation") || strings.Contains(detail, "out_of_memory"):
		out = problem("loading", "OUT_OF_MEMORY", "服务报告内存或显存不足。请释放资源或选用更小的模型，再重新验证。")
	case strings.Contains(detail, "loading model") || strings.Contains(detail, "model is loading") || strings.Contains(detail, "model is currently loading"):
		out = problem("loading", "MODEL_LOADING", "模型正在加载，待引擎报告就绪后重试。")
	case strings.Contains(detail, "no models loaded") || strings.Contains(detail, "model is not loaded"):
		out = problem("loading", "MODEL_NOT_LOADED", "服务已在线。请在引擎中加载所选模型或开启按需加载。")
	case strings.Contains(detail, "model_not_found") || (strings.Contains(detail, "model") && (strings.Contains(detail, "not found") || strings.Contains(detail, "does not exist"))) || (status == 404 && listMissing):
		out = problem("enumeration", "MODEL_NOT_FOUND", "服务找不到指定模型 ID，请重新扫描并选择服务列出的模型。")
	case status == 404 || status == 405 || status == 415 || strings.Contains(detail, "parse:") || strings.Contains(detail, "invalid provider json"):
		out = problem("inference", "PROTOCOL_MISMATCH", "该地址返回的接口或内容不符合所选协议。请核对服务类型和 API 路径。")
	case strings.Contains(detail, "no answer content") || strings.Contains(detail, "no final answer"):
		out = problem("inference", "EMPTY_ANSWER", "服务已处理请求，模型没有生成正文。可检查推理设置后重试。")
	case (status == 400 || status == 422) && strings.Contains(detail, "stream") && strings.Contains(detail, "true"):
		out = problem("inference", "STREAM_REQUIRED", "此接口要求流式推理。")
	case status == 400 || status == 422:
		out = problem("inference", "REQUEST_UNSUPPORTED", "服务拒绝了推理请求格式、参数或提示词。请检查服务协议及模型输入能力。")
	case status >= 500:
		out = problem("inference", "UPSTREAM_ERROR", "服务内部出错，请查看引擎日志后重试。")
	default:
		out = problem("service", "CONNECTION_FAILED", "连接中断或目标不可达，请检查服务状态、地址及网络。")
	}
	out.status = status
	return out
}

func (result *ModelDiagnostic) metadata(ctx context.Context, endpoint, stage, format string, model *ModelConfig, local bool) (json.RawMessage, probeProblem) {
	body, status, err := probeJSON(ctx, endpoint, model, local, nil)
	check := ProbeCheck{Stage: stage, Method: http.MethodGet, Endpoint: sanitizeProbeField(endpoint, model.APIKey), Status: "ok", HTTPStatus: status}
	if status != 0 {
		result.Service = "online"
	}
	var issue probeProblem
	if err != nil {
		issue = connectionProblem(err, false)
		check.Status, check.Code = "error", issue.code
	} else if format != "" {
		if _, valid := probeCatalog(body, format); !valid {
			issue = problem(stage, "PROTOCOL_MISMATCH", "服务没有返回此协议的模型列表，请检查地址与协议。")
			issue.status = status
			check.Status, check.Code = "error", issue.code
			body = nil
		}
	}
	result.Checks = append(result.Checks, check)
	return body, issue
}

func (result *ModelDiagnostic) applyCatalog(models []ProbeModel, model *ModelConfig) {
	result.ModelDiscovery = "unknown"
	if model.ModelName != "" {
		result.ModelDiscovery = "missing"
	}
	requested := probeModelID(model.Provider, model.ModelName)
	for _, entry := range models {
		if probeModelID(model.Provider, entry.ID) == requested {
			result.ModelDiscovery = "found"
			if entry.Loaded != nil {
				if *entry.Loaded {
					result.ModelLoaded = "loaded"
				} else {
					result.ModelLoaded = "not_loaded"
				}
			}
		}
		if len(result.Models) < 512 {
			entry.ID = sanitizeProbeField(entry.ID, model.APIKey)
			entry.Name = sanitizeProbeField(entry.Name, model.APIKey)
			result.Models = append(result.Models, entry)
		}
	}
}

func (result *ModelDiagnostic) updateResidency(ctx context.Context, endpoints probeEndpoints, model *ModelConfig, format string) {
	if endpoints.residency == "" || result.ModelDiscovery != "found" {
		return
	}
	endpoint := endpoints.residency
	if format == "lm_studio" {
		endpoint = endpoints.legacyResidency
	}
	if endpoint == "" || format == "openai" {
		return
	}
	body, _ := result.metadata(ctx, endpoint, "loading", format, model, endpoints.local)
	rows, valid := probeCatalog(body, format)
	if !valid {
		return
	}
	var loaded *bool
	if format == "ollama" {
		value := false
		loaded = &value
	}
	for _, row := range rows {
		if probeModelID(model.Provider, row.ID) == probeModelID(model.Provider, model.ModelName) {
			if format == "ollama" {
				*loaded = true
			} else {
				loaded = row.Loaded
			}
			break
		}
	}
	if loaded == nil {
		return
	}
	if *loaded {
		result.ModelLoaded = "loaded"
	} else {
		result.ModelLoaded = "not_loaded"
	}
	for i := range result.Models {
		if probeModelID(model.Provider, result.Models[i].ID) == probeModelID(model.Provider, model.ModelName) {
			result.Models[i].Loaded = loaded
		}
	}
}

func (result *ModelDiagnostic) fail(issue probeProblem) {
	result.Status, result.Stage, result.Code, result.Error, result.HTTPStatus = "error", issue.stage, issue.code, issue.message, issue.status
	if issue.code == "MODEL_NOT_FOUND" {
		result.ModelDiscovery = "missing"
	}
	if issue.code == "MODEL_LOADING" || issue.code == "MODEL_NOT_LOADED" {
		result.ModelLoaded = "not_loaded"
	}
}

// Inference itself can auto-load a model on some LM Studio setups. Verify the
// discovered deployment explicitly when the native list says it is unloaded.
// Infer=false is a read-only scan and never uses this endpoint.
func (result *ModelDiagnostic) loadLMStudio(ctx context.Context, model *ModelConfig, endpoint string) error {
	payload, err := json.Marshal(map[string]string{"model": model.ModelName})
	if err != nil {
		return err
	}
	body, status, err := probeJSON(ctx, endpoint, model, true, payload)
	check := ProbeCheck{Stage: "loading", Method: http.MethodPost, Endpoint: sanitizeProbeField(endpoint, model.APIKey), Status: "ok", HTTPStatus: status}
	var loaded struct {
		ID     string `json:"instance_id"`
		Status string `json:"status"`
	}
	defer func() { result.Checks = append(result.Checks, check) }()
	if err != nil {
		issue := connectionProblem(err, false)
		issue.stage = "loading"
		if issue.code == "PROTOCOL_MISMATCH" {
			issue.message = "当前 LM Studio 版本没有加载接口。请在 LM Studio 打开按需加载，或先在该应用加载所选模型。"
		}
		check.Status, check.Code = "error", issue.code
		result.fail(issue)
		return err
	}
	if json.Unmarshal(body, &loaded) != nil || loaded.Status != "loaded" || loaded.ID != model.ModelName {
		result.ReportedModel = sanitizeProbeField(loaded.ID, model.APIKey)
		check.Status, check.Code = "error", "LOAD_ID_UNCONFIRMED"
		result.fail(problem("loading", check.Code, "无法确认已加载的实例与所选 ID 一致，请重新扫描并选择引擎报告的实例。"))
		return errors.New("LM Studio loaded instance not confirmed")
	}
	result.ModelLoaded = "loaded"
	return nil
}

func (g *Gateway) testModelContext(ctx context.Context, model *ModelConfig) ModelDiagnostic {
	return g.probeModel(ctx, model, true, false)
}

func (g *Gateway) probeModel(parent context.Context, model *ModelConfig, infer, draft bool) (result ModelDiagnostic) {
	start := time.Now()
	result = ModelDiagnostic{
		Status: "ok", Stage: "ready", Code: "OK", Model: sanitizeProbeField(model.ModelName, model.APIKey), Provider: sanitizeProbeField(model.Provider, model.APIKey),
		Service: "unknown", ModelDiscovery: "unknown", ModelLoaded: "unknown", Inference: "skipped", Models: []ProbeModel{}, Checks: []ProbeCheck{},
	}
	defer func() {
		result.LatencyMS = float64(time.Since(start)) / float64(time.Millisecond)
		if infer {
			log.Printf("[gateway] connection probe provider=%q model=%q endpoint=%q stage=%q code=%q status=%d duration_ms=%.0f", result.Provider, result.Model, result.Endpoint, result.Stage, result.Code, result.HTTPStatus, result.LatencyMS)
		}
	}()
	if model.Credential != nil {
		result.fail(problem("authentication", string(credentialCode(model.Credential.Status)), model.Credential.Reason))
		return
	}
	endpoints, code := connectionEndpoints(model)
	result.Endpoint = sanitizeProbeField(endpoints.inference, model.APIKey)
	if code != "" {
		message := "请填写有效的 HTTP 或 HTTPS API 地址，地址中不要携带凭据、查询或片段。"
		switch code {
		case "PROVIDER_UNSUPPORTED":
			message = "请选择 Noosphere 支持的模型协议。"
		case "BASE_URL_REQUIRED":
			message = "请填写 API 基础地址，移除末尾的推理接口路径。"
		case "LOCAL_ADDRESS_REQUIRED":
			message = "此协议要求本机回环 API 地址。"
		}
		result.fail(problem("configuration", code, message))
		return
	}
	if infer && strings.TrimSpace(model.ModelName) == "" {
		result.fail(problem("configuration", "MODEL_ID_REQUIRED", "请从列表选择模型，或填写服务提供的模型 ID。"))
		return
	}
	ctx, cancel := context.WithTimeout(parent, probeTimeout)
	defer cancel()
	var healthProblem probeProblem
	var health json.RawMessage
	if endpoints.health != "" {
		health, healthProblem = result.metadata(ctx, endpoints.health, "service", "", model, endpoints.local)
	}
	var catalog json.RawMessage
	var catalogProblem probeProblem
	format := endpoints.format
	if format == "openai" && endpoints.residency != "" {
		catalog, catalogProblem = result.metadata(ctx, endpoints.residency, "enumeration", "lm_studio_v1", model, endpoints.local)
		if catalog != nil {
			format = "lm_studio_v1"
		} else if endpoints.legacyResidency != "" {
			catalog, catalogProblem = result.metadata(ctx, endpoints.legacyResidency, "enumeration", "lm_studio", model, endpoints.local)
			if catalog != nil {
				format = "lm_studio"
			}
		}
	}
	if catalog == nil {
		catalog, catalogProblem = result.metadata(ctx, endpoints.catalog, "enumeration", endpoints.format, model, endpoints.local)
	}
	if models, valid := probeCatalog(catalog, format); valid {
		result.applyCatalog(models, model)
	}
	if (endpoints.authoritative || !infer) && result.ModelDiscovery == "missing" {
		result.fail(problem("enumeration", "MODEL_NOT_FOUND", "此服务的模型列表中没有该 ID，请重新扫描并选择已下载的模型。"))
		return
	}
	if endpoints.format == "ollama" {
		result.updateResidency(ctx, endpoints, model, format)
	} else if health != nil && result.ModelDiscovery == "found" {
		var service struct{ Status string }
		if json.Unmarshal(health, &service) == nil && service.Status == "ok" {
			result.ModelLoaded = "loaded"
		}
	}
	if healthProblem.code == "MODEL_LOADING" || healthProblem.code == "OUT_OF_MEMORY" {
		result.fail(healthProblem)
		return
	}
	if !infer {
		if catalogProblem.code != "" {
			if result.Service == "unknown" {
				result.Service = "offline"
				catalogProblem.stage = "service"
			} else if catalogProblem.stage == "inference" {
				catalogProblem.stage = "enumeration"
			}
			result.fail(catalogProblem)
		} else {
			result.Stage, result.Code = "enumeration", "MODELS_DISCOVERED"
		}
		return
	}

	var prepare func(context.Context) error
	if format == "lm_studio_v1" && result.ModelLoaded == "not_loaded" {
		endpoint := strings.TrimSuffix(endpoints.residency, "/") + "/load"
		prepare = func(loadCtx context.Context) error { return result.loadLMStudio(loadCtx, model, endpoint) }
	}
	response, err := g.probeInference(ctx, model, endpoints.local, draft, prepare)
	if result.Status == "error" { // The model loader reported a precise failure.
		return
	}
	check := ProbeCheck{Stage: "inference", Method: http.MethodPost, Endpoint: result.Endpoint, Status: "ok"}
	if err != nil {
		issue := connectionProblem(err, result.ModelDiscovery == "missing")
		if issue.stage == "service" && result.Service == "unknown" && issue.code != "ENDPOINT_REDIRECTS" {
			result.Service = "offline"
		} else if issue.status > 0 {
			result.Service = "online"
		}
		if issue.code == "REQUEST_TIMEOUT" && result.Service == "unknown" {
			result.Service = "offline"
			issue.stage, issue.code, issue.message = "service", "SERVICE_TIMEOUT", "没有收到服务响应，请检查地址、端口及网络。"
		}
		check.Status, check.Code, check.HTTPStatus = "error", issue.code, issue.status
		result.Checks = append(result.Checks, check)
		result.Inference = "failed"
		result.fail(issue)
		return
	}
	result.ReportedModel = sanitizeProbeField(response.Model, model.APIKey)
	result.Service = "online"
	if endpoints.authoritative && response.Model != "" && probeModelID(model.Provider, response.Model) != probeModelID(model.Provider, model.ModelName) {
		check.Status, check.Code = "error", "MODEL_ID_MISMATCH"
		result.Checks = append(result.Checks, check)
		result.Inference = "failed"
		result.fail(problem("inference", "MODEL_ID_MISMATCH", "服务返回了不同的模型 ID。请在引擎中确认加载的模型并重新验证。"))
		return
	}
	result.Inference = "succeeded"
	if result.ModelDiscovery == "missing" {
		// Some APIs accept aliases that they omit from the model list.
		result.ModelDiscovery = "unknown"
	}
	result.Checks = append(result.Checks, check)
	result.updateResidency(ctx, endpoints, model, format)
	return
}

func (g *Gateway) probeInference(ctx context.Context, model *ModelConfig, local, draft bool, prepare func(context.Context) error) (*providers.ChatResponse, error) {
	releaseForeground, err := g.beginInteractive(ctx)
	if err != nil {
		return nil, err
	}
	defer releaseForeground()
	if err = g.acquireCall(ctx); err != nil {
		return nil, err
	}
	defer func() { <-g.sem }()
	if prepare != nil {
		if err := prepare(ctx); err != nil {
			return nil, err
		}
	}
	var provider providers.LLMProvider
	if draft {
		provider, err = providers.Get(model.Provider)(model.BaseURL, model.APIKey)
	} else {
		provider, err = g.getOrCreateProvider(model)
	}
	if err != nil {
		return nil, err
	}
	maxTokens := 256
	if local || strings.HasPrefix(model.ModelName, "o1") || strings.HasPrefix(model.ModelName, "o3") || strings.HasPrefix(model.ModelName, "o4") || strings.HasPrefix(model.ModelName, "gpt-5") || model.Provider == "gemini" || model.Provider == "google" {
		maxTokens = 1024 // Allow a reasoning budget before the short visible answer.
	}
	if model.ContextLength > 0 && maxTokens > model.ContextLength/2 {
		maxTokens = max(32, model.ContextLength/2)
	}
	opts := providers.ChatOptions{
		MaxTokens: maxTokens, MaxTokensField: model.MaxTokensKey(),
		OmitTemperature: !model.TemperatureEnabled(),
	}
	messages := []providers.Message{{Role: "user", Content: "What is 2 + 2? Please answer briefly."}}
	ctx = providers.ConnectionProbeContext(ctx)
	finish := g.countInference(false)
	response, err := provider.Chat(ctx, model.ModelName, messages, opts)
	finish()
	if err != nil && connectionProblem(err, false).code == "STREAM_REQUIRED" {
		defer g.countInference(false)()
		return probeStreamingInference(ctx, provider, model.ModelName, messages, opts)
	}
	if err == nil && (response == nil || strings.TrimSpace(response.Content) == "") {
		err = errors.New("upstream returned no answer content")
	}
	return response, err
}

func probeStreamingInference(ctx context.Context, provider providers.LLMProvider, model string, messages []providers.Message, opts providers.ChatOptions) (*providers.ChatResponse, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	stream, err := provider.ChatStream(ctx, model, messages, opts)
	if err != nil {
		return nil, err
	}
	defer func() {
		cancel()
		for range stream {
		}
	}()
	for chunk := range stream {
		if chunk.Error != nil {
			return nil, chunk.Error
		}
		if chunk.Done && strings.TrimSpace(chunk.Content) != "" {
			return &providers.ChatResponse{Content: chunk.Content}, nil
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return nil, errors.New("upstream returned no answer content")
}

func reuseModelCredential(previous *ModelConfig, next *ModelConfig, legacyLocalEdit bool) bool {
	if previous == nil {
		return false
	}
	if next.Provider == previous.Provider && next.BaseURL == previous.BaseURL {
		return true
	}
	oldURL, oldErr := url.Parse(previous.BaseURL)
	newURL, newErr := url.Parse(next.BaseURL)
	if oldErr != nil || newErr != nil || oldURL.Host == "" {
		return false
	}
	// Preserve existing local engine edits without forwarding a saved remote
	// credential to an unrelated authority when the draft address changes.
	loopback := func(host string) bool {
		ip := net.ParseIP(host)
		return strings.EqualFold(host, "localhost") || (ip != nil && ip.IsLoopback())
	}
	if legacyLocalEdit && loopback(oldURL.Hostname()) && loopback(newURL.Hostname()) {
		return true
	}
	return next.Provider == previous.Provider && strings.EqualFold(oldURL.Host, newURL.Host) && oldURL.Scheme == newURL.Scheme
}

// Reads the edited connection without changing the saved config or any binding.
func (g *Gateway) HandleProbeModel(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Model struct {
			ID        string `json:"id"`
			Provider  string `json:"provider"`
			BaseURL   string `json:"base_url"`
			ModelName string `json:"model_name"`
			APIKey    string `json:"api_key"`
		} `json:"model"`
		Infer *bool `json:"infer"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 32*1024)
	decoder := json.NewDecoder(r.Body)
	if decoder.Decode(&body) != nil || decoder.Decode(new(any)) != io.EOF || len(body.Model.ModelName) > 512 || strings.IndexFunc(body.Model.ModelName, unicode.IsControl) >= 0 {
		selectionReply(w, http.StatusBadRequest, map[string]string{"error": "模型验证参数无效。"})
		return
	}
	model := ModelConfig{ID: body.Model.ID, Provider: body.Model.Provider, BaseURL: body.Model.BaseURL, ModelName: body.Model.ModelName, APIKey: body.Model.APIKey}
	if model.APIKey == "" && model.ID != "" {
		saved := g.findModelByID(model.ID)
		if reuseModelCredential(saved, &model, false) {
			model.APIKey = saved.APIKey
			model.Credential = saved.Credential
		}
	}
	selectionReply(w, http.StatusOK, g.probeModel(r.Context(), &model, body.Infer == nil || *body.Infer, true))
}
