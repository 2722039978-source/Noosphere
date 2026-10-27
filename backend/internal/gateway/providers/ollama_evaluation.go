package providers

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"math"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

const evaluationResponseLimit = 4 * 1024 * 1024
const identityCaveat = "Identity is supplied by the configured daemon, not a cryptographic attestation of local execution."
const driftCaveat = "Tag checks before and after inference cannot atomically pin weights during inference."

// OllamaProvider delegates legacy OpenAI-compatible chat/stream/tools unchanged.
// Native evaluation validates the local boundary separately, at call time.
type OllamaProvider struct{ *OpenAIProvider }

func NewOllama(baseURL, apiKey string) (LLMProvider, error) {
	p, err := NewOpenAI(baseURL, apiKey)
	if err != nil {
		return nil, err
	}
	return &OllamaProvider{p.(*OpenAIProvider)}, nil
}

func evaluationError(code, message string) error {
	return &EvaluationError{Code: code, Message: message}
}

// NormalizeEvaluationEndpoint returns the native origin, pins localhost without
// DNS, and rejects any URL component capable of changing the local boundary.
func NormalizeEvaluationEndpoint(base string) (string, error) {
	u, err := url.Parse(base)
	if err != nil || u == nil {
		return "", evaluationError("UNSAFE_ENDPOINT", "invalid local endpoint")
	}
	if (u.Scheme != "http" && u.Scheme != "https") || u.Opaque != "" || u.User != nil || u.RawQuery != "" || u.ForceQuery || strings.Contains(base, "#") || u.RawPath != "" || strings.HasSuffix(u.Host, ":") {
		return "", evaluationError("UNSAFE_ENDPOINT", "local endpoint must have no credentials, query, fragment or escaped path")
	}
	path := strings.TrimSuffix(u.Path, "/")
	if path != "" && path != "/v1" && path != "/api" {
		return "", evaluationError("UNSAFE_ENDPOINT", "unsupported native endpoint base path")
	}
	host := strings.ToLower(u.Hostname())
	if host == "localhost" {
		host = "127.0.0.1"
	} else {
		ip := net.ParseIP(host)
		if ip == nil || !ip.IsLoopback() {
			return "", evaluationError("UNSAFE_ENDPOINT", "native evaluation requires a loopback literal or localhost")
		}
		host = ip.String()
	}
	port := u.Port()
	if port != "" {
		n, e := strconv.Atoi(port)
		if e != nil || n < 1 || n > 65535 {
			return "", evaluationError("UNSAFE_ENDPOINT", "invalid endpoint port")
		}
		port = strconv.Itoa(n)
	}
	if (u.Scheme == "http" && port == "80") || (u.Scheme == "https" && port == "443") {
		port = ""
	}
	if port != "" {
		u.Host = net.JoinHostPort(host, port)
	} else if strings.Contains(host, ":") {
		u.Host = "[" + host + "]"
	} else {
		u.Host = host
	}
	u.Path = ""
	return u.String(), nil
}

func (p *OllamaProvider) nativeClient() (*http.Client, string, error) {
	origin, err := NormalizeEvaluationEndpoint(p.baseURL)
	if err != nil {
		return nil, "", err
	}
	// No proxy environment and no redirect, including redirects to another local port.
	return &http.Client{Transport: &http.Transport{Proxy: nil, DialContext: (&net.Dialer{Timeout: 10 * time.Second}).DialContext, DisableCompression: true}, CheckRedirect: func(*http.Request, []*http.Request) error {
		return evaluationError("UNSAFE_ENDPOINT", "native endpoint redirect rejected")
	}}, origin, nil
}

func closeNativeClient(c *http.Client) { c.CloseIdleConnections() }

func (p *OllamaProvider) nativeJSON(ctx context.Context, c *http.Client, origin, path string, body any) (map[string]any, json.RawMessage, error) {
	if err := ctx.Err(); err != nil {
		return nil, nil, EvaluationContextError(err)
	}
	method := http.MethodGet
	var payload []byte
	if body != nil {
		method = http.MethodPost
		var err error
		payload, err = json.Marshal(body)
		if err != nil {
			return nil, nil, evaluationError("INVALID_REQUEST", "request cannot be encoded")
		}
		if len(payload) > 1024*1024 {
			return nil, nil, evaluationError("INVALID_REQUEST", "request exceeds one MiB")
		}
	}
	req, err := http.NewRequestWithContext(ctx, method, origin+"/api/"+path, bytes.NewReader(payload))
	if err != nil {
		return nil, nil, evaluationError("INVALID_REQUEST", "invalid native request")
	}
	req.Header.Set("Content-Type", "application/json")
	if p.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+p.apiKey)
	}
	resp, err := c.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, nil, EvaluationContextError(ctx.Err())
		}
		var typed *EvaluationError
		if errors.As(err, &typed) {
			return nil, nil, typed
		}
		return nil, nil, EvaluationContextError(err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, evaluationResponseLimit+1))
	if ctx.Err() != nil {
		return nil, nil, EvaluationContextError(ctx.Err())
	}
	if len(raw) > evaluationResponseLimit {
		return nil, nil, evaluationError("RESPONSE_TOO_LARGE", "native response exceeds four MiB")
	}
	if err != nil {
		return nil, nil, evaluationError("INVALID_OUTPUT", "native response body was interrupted")
	}
	var result map[string]any
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	decodeErr := dec.Decode(&result)
	var trailing any
	if decodeErr == nil && dec.Decode(&trailing) != io.EOF {
		decodeErr = errors.New("trailing content")
	}
	// Never copy arbitrary upstream error text into error messages or raw output.
	upstreamError, _ := result["error"].(string)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 || upstreamError != "" {
		code := "PROVIDER_UNAVAILABLE"
		message := "native provider rejected request"
		evidence := strings.ToLower(upstreamError)
		if strings.Contains(evidence, "out of memory") || strings.Contains(evidence, "requires more system memory") || strings.Contains(evidence, "cuda error: memory allocation") {
			code = "OUT_OF_MEMORY"
			message = "native provider reported out of memory"
		} else if resp.StatusCode >= 500 || resp.StatusCode == 429 {
			code = "RESOURCE_EXHAUSTION_UNKNOWN"
			message = "native runner failure; resource exhaustion is unconfirmed"
		}
		return nil, nil, &EvaluationError{Code: code, Message: message, HTTPStatus: resp.StatusCode}
	}
	if decodeErr != nil || result == nil {
		return nil, nil, evaluationError("INVALID_OUTPUT", "invalid native JSON response")
	}
	var safeResult map[string]any
	if p.apiKey != "" {
		// Keep identity/config hashing independent of transport credentials.
		decoder := json.NewDecoder(bytes.NewReader(raw))
		decoder.UseNumber()
		_ = decoder.Decode(&safeResult)
	}
	if p.apiKey != "" && redactNativeStrings(safeResult, p.apiKey) {
		// Re-encode only if redaction is necessary, preserving valid JSON even for
		// credentials containing quotes or non-ASCII characters.
		raw, _ = json.Marshal(safeResult)
	}
	return result, raw, nil
}

func redactNativeStrings(v any, key string) bool {
	changed := false
	switch node := v.(type) {
	case map[string]any:
		for k, value := range node {
			if strings.Contains(k, key) {
				delete(node, k)
				k = strings.ReplaceAll(k, key, "[redacted]")
				node[k] = value
				changed = true
			}
			if s, ok := value.(string); ok && strings.Contains(s, key) {
				node[k] = strings.ReplaceAll(s, key, "[redacted]")
				changed = true
			} else if redactNativeStrings(value, key) {
				changed = true
			}
		}
	case []any:
		for i, value := range node {
			if s, ok := value.(string); ok && strings.Contains(s, key) {
				node[i] = strings.ReplaceAll(s, key, "[redacted]")
				changed = true
			} else if redactNativeStrings(value, key) {
				changed = true
			}
		}
	}
	return changed
}

func str(m map[string]any, k string) string         { v, _ := m[k].(string); return v }
func obj(m map[string]any, k string) map[string]any { v, _ := m[k].(map[string]any); return v }
func hashValue(v any) string {
	b, _ := json.Marshal(v)
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}
func normalizeDigest(v string) string {
	v = strings.TrimPrefix(strings.ToLower(strings.TrimSpace(v)), "sha256:")
	if len(v) != 64 {
		return ""
	}
	if _, err := hex.DecodeString(v); err != nil {
		return ""
	}
	return v
}
func intValue(v any) (int64, bool) {
	n, ok := v.(json.Number)
	if !ok {
		return 0, false
	}
	i, err := n.Int64()
	return i, err == nil
}
func remoteMetadata(m map[string]any) bool {
	// The engine fields are top-level in official list/show/chat responses.
	return (m["remote_host"] != nil && m["remote_host"] != "") || (m["remote_model"] != nil && m["remote_model"] != "")
}
func cloudAlias(name string) bool {
	n := strings.ToLower(name)
	return strings.Contains(n, ":cloud") || strings.HasSuffix(n, "-cloud") || strings.Contains(n, "-cloud:") || strings.Contains(n, "ollama.com/")
}
func canonicalModel(name string) string {
	last := name[strings.LastIndex(name, "/")+1:]
	if !strings.Contains(last, ":") {
		return name + ":latest"
	}
	return name
}
func validModelName(name string) bool {
	return len(name) > 0 && len(name) <= 512 && strings.TrimSpace(name) == name && !strings.ContainsAny(name, "\x00\r\n\t ")
}

func identityFrom(version string, tag, show map[string]any) ModelIdentity {
	name := str(tag, "name")
	if name == "" {
		name = str(tag, "model")
	}
	id := ModelIdentity{Name: name, Digest: normalizeDigest(str(tag, "digest")), EngineVersion: version, Capabilities: []string{}, Limitations: []string{identityCaveat, driftCaveat}}
	id.SizeBytes, _ = intValue(tag["size"])
	detail := obj(show, "details")
	if detail == nil {
		detail = obj(tag, "details")
	}
	id.Format = str(detail, "format")
	id.Family = str(detail, "family")
	id.ParameterSize = str(detail, "parameter_size")
	id.Quantization = str(detail, "quantization_level")
	if caps, ok := show["capabilities"].([]any); ok {
		for _, v := range caps {
			if s, ok := v.(string); ok {
				id.Capabilities = append(id.Capabilities, s)
			}
		}
	}
	sort.Strings(id.Capabilities)
	safeShow := make(map[string]any, len(show))
	for k, v := range show {
		if k == "modelfile" {
			safeShow["modelfile_hash"] = hashValue(v)
		} else {
			safeShow[k] = v
		}
	}
	id.TemplateHash = hashValue(show["template"])
	id.ConfigHash = hashValue(safeShow)
	// Include all supplied tag/deployment metadata and version; Go's JSON map
	// encoding sorts keys and json.Number preserves exact server numeric values.
	id.MetadataHash = hashValue(map[string]any{"version": version, "tag": tag, "show": safeShow})
	switch {
	case !validModelName(name):
		id.IneligibilityReason = "missing or invalid installed model name"
	case cloudAlias(name) || remoteMetadata(tag) || remoteMetadata(show):
		id.IneligibilityReason = "cloud alias or remote model metadata is ineligible for local evaluation"
	case id.Digest == "":
		id.IneligibilityReason = "missing precise SHA256 model digest"
	case version == "":
		id.IneligibilityReason = "missing engine version"
	case id.SizeBytes <= 0:
		id.IneligibilityReason = "missing positive installed weight size"
	case strings.TrimSpace(id.Format) == "" || strings.TrimSpace(str(obj(show, "model_info"), "general.architecture")) == "":
		id.IneligibilityReason = "missing format or model_info architecture for local weight verification"
	default:
		id.LocalVerified = true
	}
	if id.Family == "" {
		id.Limitations = append(id.Limitations, "Model family was not reported.")
	}
	if id.ParameterSize == "" {
		id.Limitations = append(id.Limitations, "Parameter size was not reported.")
	}
	if id.Quantization == "" {
		id.Limitations = append(id.Limitations, "Quantization was not reported.")
	}
	if len(id.Capabilities) == 0 {
		id.Limitations = append(id.Limitations, "Capabilities were not reported.")
	}
	return id
}

func (p *OllamaProvider) discover(ctx context.Context, c *http.Client, origin, selected string) (ModelDiscovery, error) {
	d := ModelDiscovery{Models: []ModelIdentity{}}
	version, _, err := p.nativeJSON(ctx, c, origin, "version", nil)
	if err != nil {
		return d, err
	}
	d.EngineVersion = str(version, "version")
	if strings.TrimSpace(d.EngineVersion) == "" || len(d.EngineVersion) > 256 {
		return d, evaluationError("INVALID_OUTPUT", "native engine did not report a bounded version")
	}
	tags, _, err := p.nativeJSON(ctx, c, origin, "tags", nil)
	if err != nil {
		return d, err
	}
	models, ok := tags["models"].([]any)
	if !ok {
		return d, evaluationError("INVALID_OUTPUT", "native tags must include a model list")
	}
	if len(models) > 128 {
		return d, evaluationError("RESPONSE_TOO_LARGE", "native model list exceeds 128 entries")
	}
	seen := map[string]bool{}
	for _, entry := range models {
		tag, ok := entry.(map[string]any)
		if !ok {
			return d, evaluationError("INVALID_OUTPUT", "invalid native model entry")
		}
		name := str(tag, "name")
		if name == "" {
			name = str(tag, "model")
		}
		if selected != "" && canonicalModel(name) != canonicalModel(selected) {
			continue
		}
		key := canonicalModel(name)
		if seen[key] {
			return d, evaluationError("INVALID_OUTPUT", "ambiguous duplicate native model names")
		}
		seen[key] = true
		if !validModelName(name) {
			id := identityFrom(d.EngineVersion, tag, nil)
			d.Models = append(d.Models, id)
			continue
		}
		show, _, showErr := p.nativeJSON(ctx, c, origin, "show", map[string]any{"model": name})
		if showErr != nil {
			if ctx.Err() != nil {
				return d, showErr
			}
			// A selected model must preserve precise transport/parse failure codes.
			// During discovery another ineligible entry must not hide valid models.
			if selected != "" {
				return d, showErr
			}
			id := identityFrom(d.EngineVersion, tag, nil)
			id.LocalVerified = false
			var typed *EvaluationError
			id.IneligibilityReason = "native show metadata unavailable"
			if errors.As(showErr, &typed) {
				id.IneligibilityReason += " (" + typed.Code + ")"
			}
			d.Models = append(d.Models, id)
			continue
		}
		d.Models = append(d.Models, identityFrom(d.EngineVersion, tag, show))
	}
	if err := ctx.Err(); err != nil {
		return d, EvaluationContextError(err)
	}
	return d, nil
}

func (p *OllamaProvider) Discover(parent context.Context) (ModelDiscovery, error) {
	ctx, cancel := context.WithTimeout(parent, 2*time.Minute)
	defer cancel()
	c, origin, err := p.nativeClient()
	if err != nil {
		return ModelDiscovery{}, err
	}
	defer closeNativeClient(c)
	return p.discover(ctx, c, origin, "")
}

func validateEvaluationRequest(req EvaluationRequest) error {
	if !validModelName(req.ModelName) || normalizeDigest(req.ExpectedDigest) == "" || normalizeDigest(req.ExpectedMetadataHash) == "" || req.ExpectedEngineVersion == "" || len(req.ExpectedEngineVersion) > 256 {
		return evaluationError("INVALID_REQUEST", "model name and complete expected identity are required")
	}
	if len(req.AttemptID) > 256 || len(req.Messages) > 256 {
		return evaluationError("INVALID_REQUEST", "attempt or message count exceeds limit")
	}
	if req.Kind != "" && req.Kind != "evaluation" && req.Kind != "warmup" {
		return evaluationError("INVALID_REQUEST", "unsupported evaluation kind")
	}
	if req.Kind == "warmup" && len(req.Messages) > 0 {
		return evaluationError("INVALID_REQUEST", "warmup requires an empty prompt")
	}
	if req.Kind != "warmup" && len(req.Messages) == 0 {
		return evaluationError("INVALID_REQUEST", "evaluation requires messages")
	}
	size := 0
	for _, m := range req.Messages {
		if m.Role != "user" && m.Role != "system" && m.Role != "assistant" {
			return evaluationError("UNSUPPORTED_CAPABILITY", "evaluation accepts text messages without tools")
		}
		size += len(m.Content)
		if len(m.Content) > 256*1024 {
			return evaluationError("INVALID_REQUEST", "message exceeds 256 KiB")
		}
	}
	if size > 512*1024 {
		return evaluationError("INVALID_REQUEST", "messages exceed 512 KiB")
	}
	return ValidateEvaluationOptions(req.Options)
}

// ValidateEvaluationOptions applies the native evaluation option limits without
// requiring model identity or messages. Profile creation and inference share it.
func ValidateEvaluationOptions(o EvaluationOptions) error {
	if o.Temperature != nil && (math.IsNaN(*o.Temperature) || math.IsInf(*o.Temperature, 0) || *o.Temperature < 0 || *o.Temperature > 2) {
		return evaluationError("INVALID_REQUEST", "temperature must be between zero and two")
	}
	if o.TopP != nil && (math.IsNaN(*o.TopP) || math.IsInf(*o.TopP, 0) || *o.TopP < 0 || *o.TopP > 1) {
		return evaluationError("INVALID_REQUEST", "top_p must be between zero and one")
	}
	if (o.TopK != nil && (*o.TopK < 0 || *o.TopK > 100000)) || (o.NumCtx != nil && (*o.NumCtx < 0 || *o.NumCtx > 1048576)) || (o.MaxOutputTokens != nil && (*o.MaxOutputTokens < 0 || *o.MaxOutputTokens > 1048576)) {
		return evaluationError("INVALID_REQUEST", "integer generation option exceeds supported bounds")
	}
	if len(o.Stop) > 64 {
		return evaluationError("INVALID_REQUEST", "too many stop strings")
	}
	for _, s := range o.Stop {
		if len(s) > 1024 {
			return evaluationError("INVALID_REQUEST", "stop string exceeds 1024 bytes")
		}
	}
	if o.KeepAlive != "" {
		v, e := time.ParseDuration(o.KeepAlive)
		if len(o.KeepAlive) > 64 || e != nil || v < 0 || v > 24*time.Hour {
			return evaluationError("INVALID_REQUEST", "keep_alive must be between zero and 24 hours")
		}
	}
	return nil
}

func requestedNativeOptions(o EvaluationOptions) map[string]any {
	result := map[string]any{}
	if o.Temperature != nil {
		result["temperature"] = *o.Temperature
	}
	if o.Seed != nil {
		result["seed"] = *o.Seed
	}
	if o.TopP != nil {
		result["top_p"] = *o.TopP
	}
	if o.TopK != nil {
		result["top_k"] = *o.TopK
	}
	if o.NumCtx != nil {
		result["num_ctx"] = *o.NumCtx
	}
	if o.MaxOutputTokens != nil {
		result["num_predict"] = *o.MaxOutputTokens
	}
	if o.Stop != nil {
		result["stop"] = o.Stop
	}
	return result
}
func sameExpected(id ModelIdentity, req EvaluationRequest) bool {
	return id.LocalVerified && id.Digest == normalizeDigest(req.ExpectedDigest) && id.MetadataHash == normalizeDigest(req.ExpectedMetadataHash) && id.EngineVersion == req.ExpectedEngineVersion
}

func (p *OllamaProvider) Evaluate(parent context.Context, req EvaluationRequest) (*EvaluationResponse, error) {
	timeout, err := EvaluationTimeout(req.Timeout)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()
	if err = ctx.Err(); err != nil {
		return nil, EvaluationContextError(err)
	}
	if err = validateEvaluationRequest(req); err != nil {
		return nil, err
	}
	c, origin, err := p.nativeClient()
	if err != nil {
		return nil, err
	}
	defer closeNativeClient(c)
	before, err := p.discover(ctx, c, origin, req.ModelName)
	if err != nil {
		return nil, err
	}
	if len(before.Models) != 1 || !sameExpected(before.Models[0], req) {
		return nil, evaluationError("MODEL_DRIFT", "selected local identity no longer matches registered identity")
	}
	body := map[string]any{"model": req.ModelName, "stream": false, "options": requestedNativeOptions(req.Options)}
	path := "chat"
	if req.Kind == "warmup" {
		path = "generate"
		body["prompt"] = ""
	} else {
		body["messages"] = req.Messages
	}
	if req.Options.Think != nil {
		body["think"] = *req.Options.Think
	}
	if req.Options.KeepAlive != "" {
		body["keep_alive"] = req.Options.KeepAlive
	}
	start := time.Now()
	native, raw, err := p.nativeJSON(ctx, c, origin, path, body)
	wall := time.Since(start)
	if err != nil {
		return nil, err
	}
	r := &EvaluationResponse{IdentityBefore: before.Models[0], RequestedOptions: req.Options, RawResponse: raw, WallDuration: wall, Limitations: []string{identityCaveat, driftCaveat, "Generation options are requested values; the engine does not confirm all effective values."}}
	var outputErr error
	for _, field := range []string{"model", "done_reason", "response", "thinking"} {
		if value, present := native[field]; present {
			if _, ok := value.(string); !ok {
				outputErr = evaluationError("INVALID_OUTPUT", "native text fields must be strings")
			}
		}
	}
	if remoteMetadata(native) {
		outputErr = evaluationError("UNSAFE_ENDPOINT", "native response reports remote execution")
	}
	done, ok := native["done"].(bool)
	if !ok || !done {
		outputErr = evaluationError("INVALID_OUTPUT", "native response did not finish")
	}
	if canonicalModel(str(native, "model")) != canonicalModel(req.ModelName) {
		outputErr = evaluationError("MODEL_DRIFT", "native response model differs from requested identity")
	}
	r.DoneReason = str(native, "done_reason")
	r.Truncated = r.DoneReason == "length"
	if path == "generate" {
		r.Output = str(native, "response")
		r.Thinking = str(native, "thinking")
	} else {
		msg := obj(native, "message")
		for _, field := range []string{"content", "thinking"} {
			if value, present := msg[field]; present {
				if _, ok := value.(string); !ok {
					outputErr = evaluationError("INVALID_OUTPUT", "native message text must be strings")
				}
			}
		}
		r.Output = str(msg, "content")
		r.Thinking = str(msg, "thinking")
		if tools, ok := msg["tool_calls"].([]any); ok && len(tools) > 0 {
			outputErr = evaluationError("UNSUPPORTED_CAPABILITY", "native evaluation returned unexpected tool calls")
		}
	}
	if path != "generate" && strings.TrimSpace(r.Output) == "" && outputErr == nil {
		outputErr = evaluationError("INVALID_OUTPUT", "native evaluation returned no answer text")
	}
	r.Output = p.redact(r.Output)
	r.Thinking = p.redact(r.Thinking)
	metrics := []struct {
		key  string
		dest **int64
	}{{"total_duration", &r.TotalDurationNS}, {"load_duration", &r.LoadDurationNS}, {"prompt_eval_count", &r.PromptEvalCount}, {"prompt_eval_duration", &r.PromptEvalDurationNS}, {"eval_count", &r.EvalCount}, {"eval_duration", &r.EvalDurationNS}}
	for _, metric := range metrics {
		v, exists := native[metric.key]
		if !exists || v == nil {
			r.Limitations = append(r.Limitations, "Engine did not report "+metric.key+".")
			continue
		}
		n, valid := intValue(v)
		if !valid || n < 0 {
			outputErr = evaluationError("INVALID_OUTPUT", "native metrics must be nonnegative integers")
			continue
		}
		*metric.dest = &n
	}
	after, postErr := p.discover(ctx, c, origin, req.ModelName)
	if postErr != nil {
		r.Limitations = append(r.Limitations, "Post-inference identity could not be verified.")
		return r, postErr
	}
	if len(after.Models) == 1 {
		r.IdentityAfter = after.Models[0]
	}
	if len(after.Models) != 1 || !sameExpected(r.IdentityAfter, req) || r.IdentityAfter.MetadataHash != r.IdentityBefore.MetadataHash {
		return r, evaluationError("MODEL_DRIFT", "selected identity changed after inference; output retained as evidence")
	}
	return r, outputErr
}
