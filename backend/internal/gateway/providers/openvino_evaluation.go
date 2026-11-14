package providers

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// OpenVINOProvider speaks the bundled, explicitly versioned GenAI adapter
// contract. An arbitrary OpenAI-compatible endpoint is not evidence of OpenVINO.
type OpenVINOProvider struct {
	*OpenAIProvider
	identityMu sync.Mutex
	files      map[string]openVINOFileCache
}
type openVINOFileCache struct {
	info   os.FileInfo
	digest string
}
type openVINOArtifact struct {
	name, digest string
	size         int64
}

func init() { Register("openvino", NewOpenVINO) }
func NewOpenVINO(baseURL, apiKey string) (LLMProvider, error) {
	p, err := NewOpenAI(baseURL, apiKey)
	if err != nil {
		return nil, err
	}
	return &OpenVINOProvider{OpenAIProvider: p.(*OpenAIProvider), files: map[string]openVINOFileCache{}}, nil
}
func (p *OpenVINOProvider) Name() string { return "openvino" }

// Chat uses the same bounded native contract as evaluation, without treating a
// conversational response as a registered or formally scored experiment.
func (p *OpenVINOProvider) Chat(ctx context.Context, model string, messages []Message, opts ChatOptions) (*ChatResponse, error) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	c, origin, err := (&OllamaProvider{p.OpenAIProvider}).nativeClient()
	if err != nil {
		return nil, err
	}
	defer closeNativeClient(c)
	if opts.SystemPrompt != "" {
		messages = append([]Message{{Role: "system", Content: opts.SystemPrompt}}, messages...)
	}
	max := opts.MaxTokens
	if max <= 0 {
		max = 512
	}
	metadata, _, err := (&OllamaProvider{p.OpenAIProvider}).nativeJSONPath(ctx, c, origin, "/noosphere/openvino/metadata", nil)
	if err != nil {
		return nil, err
	}
	if err = openVINOMetadata(metadata); err != nil {
		return nil, err
	}
	// Chat's output request is an upper bound; formal evaluation instead rejects
	// a profile exceeding capacity so a benchmark cannot silently change limits.
	if limit, ok := intValue(obj(metadata, "runtime_config")["max_output_tokens"]); ok && int64(max) > limit {
		max = int(limit)
	}
	think := false
	body := map[string]any{"model": model, "messages": messages, "options": EvaluationOptions{Temperature: &opts.Temperature, MaxOutputTokens: &max, Think: &think}}
	result, _, err := (&OllamaProvider{p.OpenAIProvider}).nativeJSONPath(ctx, c, origin, "/noosphere/openvino/evaluate", body)
	if err != nil {
		return nil, err
	}
	out := &EvaluationResponse{}
	if err = openVINOAnswer(result, out); err != nil {
		return nil, err
	}
	if out.Output == "" {
		return nil, evaluationError("INVALID_OUTPUT", "OpenVINO returned no final answer")
	}
	response := &ChatResponse{Content: p.redact(out.Output)}
	if out.PromptEvalCount != nil {
		response.TokensIn = int(*out.PromptEvalCount)
	}
	if out.EvalCount != nil {
		response.TokensOut = int(*out.EvalCount)
	}
	return response, nil
}

// This adapter returns one terminal chunk. It does not invent streaming timing.
func (p *OpenVINOProvider) ChatStream(ctx context.Context, model string, messages []Message, opts ChatOptions) (<-chan StreamChunk, error) {
	ch := make(chan StreamChunk, 1)
	go func() {
		defer close(ch)
		r, err := p.Chat(ctx, model, messages, opts)
		v := StreamChunk{Done: true}
		if err != nil {
			v.Error = err
		} else {
			v.Content = r.Content
		}
		select {
		case ch <- v:
		case <-ctx.Done():
		}
	}()
	return ch, nil
}

func (p *OpenVINOProvider) artifactDigest(ctx context.Context, root string, raw any) (string, int64, error) {
	if !filepath.IsAbs(root) || strings.HasPrefix(root, `\\`) {
		return "", 0, evaluationError("INSUFFICIENT_EVIDENCE", "OpenVINO must report an absolute local model directory")
	}
	root, err := filepath.EvalSymlinks(root)
	if err != nil || strings.HasPrefix(root, `\\`) {
		return "", 0, evaluationError("INSUFFICIENT_EVIDENCE", "OpenVINO model directory is not readable")
	}
	rows, ok := raw.([]any)
	if !ok || len(rows) < 9 || len(rows) > 64 {
		return "", 0, evaluationError("INSUFFICIENT_EVIDENCE", "OpenVINO artifact manifest is missing or unbounded")
	}
	required := map[string]bool{"openvino_model.xml": false, "openvino_model.bin": false, "openvino_tokenizer.xml": false, "openvino_tokenizer.bin": false, "openvino_detokenizer.xml": false, "openvino_detokenizer.bin": false, "config.json": false, "tokenizer_config.json": false, "generation_config.json": false}
	seen := map[string]bool{}
	items := []openVINOArtifact{}
	var total int64
	p.identityMu.Lock()
	defer p.identityMu.Unlock()
	for _, row := range rows {
		r, ok := row.(map[string]any)
		if !ok {
			return "", 0, evaluationError("INSUFFICIENT_EVIDENCE", "invalid OpenVINO artifact entry")
		}
		name := str(r, "name")
		size, sizeOK := intValue(r["size"])
		want := str(r, "sha256")
		ext := strings.ToLower(filepath.Ext(name))
		allowed := ext == ".xml" || ext == ".bin" || ext == ".json" || ext == ".txt" || name == "README.md"
		decoded, e := hex.DecodeString(want)
		if name == "" || len(name) > 128 || filepath.Base(name) != name || strings.ContainsAny(name, `/\:`) || seen[strings.ToLower(name)] || !allowed || !sizeOK || size <= 0 || size > 128<<30 || e != nil || len(decoded) != 32 {
			return "", 0, evaluationError("INSUFFICIENT_EVIDENCE", "invalid OpenVINO artifact path, size or hash")
		}
		seen[strings.ToLower(name)] = true
		if _, exists := required[name]; exists {
			required[name] = true
		}
		path := filepath.Join(root, name)
		resolved, e := filepath.EvalSymlinks(path)
		relative, relErr := filepath.Rel(root, resolved)
		if e != nil || relErr != nil || filepath.Dir(relative) != "." || filepath.IsAbs(relative) {
			return "", 0, evaluationError("INSUFFICIENT_EVIDENCE", "artifact escapes the model directory or is unreadable")
		}
		file, e := os.Open(resolved)
		if e != nil {
			return "", 0, evaluationError("INSUFFICIENT_EVIDENCE", "OpenVINO artifact is unreadable")
		}
		info, e := file.Stat()
		if e != nil || !info.Mode().IsRegular() || info.Size() != size {
			file.Close()
			return "", 0, evaluationError("MODEL_DRIFT", "OpenVINO artifact size or type changed")
		}
		cached := p.files[resolved]
		digest := cached.digest
		if cached.info == nil || !os.SameFile(info, cached.info) || info.Size() != cached.info.Size() || info.ModTime() != cached.info.ModTime() {
			h := sha256.New()
			_, e = io.Copy(h, &evaluationContextReader{ctx: ctx, r: file})
			after, statErr := file.Stat()
			file.Close()
			if e != nil {
				return "", 0, EvaluationContextError(e)
			}
			if statErr != nil || after.Size() != info.Size() || after.ModTime() != info.ModTime() {
				return "", 0, evaluationError("MODEL_DRIFT", "OpenVINO artifact changed while hashing")
			}
			digest = hex.EncodeToString(h.Sum(nil))
			p.files[resolved] = openVINOFileCache{info: info, digest: digest}
		} else {
			file.Close()
		}
		if digest != want {
			return "", 0, evaluationError("MODEL_DRIFT", "OpenVINO artifact SHA256 differs from the loaded manifest")
		}
		total += size
		if total > 128<<30 {
			return "", 0, evaluationError("INSUFFICIENT_EVIDENCE", "OpenVINO model exceeds artifact limit")
		}
		items = append(items, openVINOArtifact{name: name, size: size, digest: digest})
	}
	for _, present := range required {
		if !present {
			return "", 0, evaluationError("INSUFFICIENT_EVIDENCE", "OpenVINO model/tokenizer IR or configuration artifact is missing")
		}
	}
	sort.Slice(items, func(i, j int) bool { return items[i].name < items[j].name })
	h := sha256.New()
	for _, item := range items {
		fmt.Fprintf(h, "%s\x00%d\x00%s\n", item.name, item.size, item.digest)
	}
	return hex.EncodeToString(h.Sum(nil)), total, nil
}

func openVINOMetadata(m map[string]any) error {
	schema, ok := intValue(m["schema_version"])
	if !ok || schema != 1 || str(m, "adapter") != "noosphere-openvino-v1" || str(m, "engine") != "openvino_genai" {
		return evaluationError("UNSUPPORTED_CAPABILITY", "the versioned Noosphere OpenVINO GenAI adapter is required")
	}
	for _, key := range []string{"genai_version", "openvino_version", "tokenizers_version", "model_name", "chat_template"} {
		v := str(m, key)
		if v == "" || len(v) > 65536 {
			return evaluationError("INSUFFICIENT_EVIDENCE", "OpenVINO engine, model or template metadata is missing")
		}
	}
	adapterHash, hashErr := hex.DecodeString(str(m, "adapter_sha256"))
	if hashErr != nil || len(adapterHash) != 32 {
		return evaluationError("INSUFFICIENT_EVIDENCE", "OpenVINO adapter source hash is required")
	}
	runtime := obj(m, "runtime_config")
	device := str(runtime, "device")
	capacity, cOK := intValue(runtime["num_ctx"])
	output, oOK := intValue(runtime["max_output_tokens"])
	parallel, pOK := intValue(runtime["parallel"])
	if (device != "CPU" && device != "GPU" && device != "GPU.0") || str(runtime, "device_name") == "" || !cOK || capacity < 128 || capacity > 131072 || !oOK || output < 1 || output > 16384 || !pOK || parallel != 1 || obj(runtime, "default_options") == nil {
		return evaluationError("INSUFFICIENT_EVIDENCE", "OpenVINO device or execution limits are invalid")
	}
	return nil
}
func (p *OpenVINOProvider) discovery(ctx context.Context, c *http.Client, origin string) (ModelDiscovery, map[string]any, error) {
	d := ModelDiscovery{Models: []ModelIdentity{}}
	native := &OllamaProvider{p.OpenAIProvider}
	m, _, err := native.nativeJSONPath(ctx, c, origin, "/noosphere/openvino/metadata", nil)
	if err != nil {
		return d, nil, err
	}
	if err = openVINOMetadata(m); err != nil {
		return d, nil, err
	}
	digest, size, err := p.artifactDigest(ctx, str(m, "model_dir"), m["artifacts"])
	if err != nil {
		return d, nil, err
	}
	if digest != str(m, "model_digest") {
		return d, nil, evaluationError("MODEL_DRIFT", "OpenVINO aggregate artifact identity differs")
	}
	list, _, err := native.nativeJSONPath(ctx, c, origin, "/v1/models", nil)
	if err != nil {
		return d, nil, err
	}
	models, ok := list["data"].([]any)
	if !ok || len(models) != 1 {
		return d, nil, evaluationError("INSUFFICIENT_EVIDENCE", "OpenVINO adapter must report one fixed model")
	}
	tag, ok := models[0].(map[string]any)
	name := str(m, "model_name")
	if !ok || !validModelName(name) || str(tag, "id") != name || str(tag, "owned_by") != "openvino_genai" {
		return d, nil, evaluationError("MODEL_DRIFT", "OpenVINO model listing and native metadata disagree")
	}
	d.EngineVersion = "openvino-genai/" + str(m, "genai_version") + ";openvino/" + str(m, "openvino_version")
	meta := obj(m, "model_metadata")
	id := ModelIdentity{Provider: "openvino", Name: name, Digest: digest, EngineVersion: d.EngineVersion, ConfigHash: hashValue(map[string]any{"adapter": m["adapter"], "runtime": m["runtime_config"], "adapter_sha256": m["adapter_sha256"], "dependencies": m["dependency_versions"]}), TemplateHash: hashValue(m["chat_template"]), Format: "openvino_ir", Family: str(meta, "family"), ParameterSize: str(meta, "parameter_size"), Quantization: str(meta, "quantization"), SizeBytes: size, Capabilities: []string{"completion"}, LocalVerified: true, Limitations: []string{"Local IR, tokenizer and configuration SHA256 values are independently verified; daemon metadata is not an attestation of loaded memory or physical execution.", "File hash caching uses file identity, size and modification time; hostile same-metadata edits cannot be detected.", "The externally managed OpenVINO process stays resident; owned warmup, keep_alive, unload and exclusive memory metrics are unsupported.", "OpenVINO inference duration includes prefill and is not reported as decode-only duration; missing metrics remain unknown.", "num_ctx is a checked prompt-plus-output token budget; it is not a fixed KV allocation claim. CPU and GPU settings produce distinct configuration identities."}}
	id.MetadataHash = hashValue(map[string]any{"provider": id.Provider, "name": name, "digest": digest, "size": size, "engine": id.EngineVersion, "config": id.ConfigHash, "template": id.TemplateHash, "model": meta, "source": m["source"]})
	d.Models = append(d.Models, id)
	return d, m, nil
}
func (p *OpenVINOProvider) Discover(parent context.Context) (ModelDiscovery, error) {
	ctx, cancel := context.WithTimeout(parent, 2*time.Minute)
	defer cancel()
	c, origin, err := (&OllamaProvider{p.OpenAIProvider}).nativeClient()
	if err != nil {
		return ModelDiscovery{}, err
	}
	defer closeNativeClient(c)
	d, _, err := p.discovery(ctx, c, origin)
	return d, err
}
func checkOpenVINOOptions(options EvaluationOptions, m map[string]any) error {
	if err := ValidateEvaluationOptions(options); err != nil {
		return err
	}
	if options.KeepAlive != "" {
		return evaluationError("UNSUPPORTED_CAPABILITY", "OpenVINO is externally resident; keep_alive is not supported")
	}
	if options.Seed != nil && *options.Seed < 0 {
		return evaluationError("UNSUPPORTED_CAPABILITY", "OpenVINO rng_seed must be non-negative")
	}
	if err := openVINOMetadata(m); err != nil {
		return err
	}
	runtime := obj(m, "runtime_config")
	capacity, _ := intValue(runtime["num_ctx"])
	output, _ := intValue(runtime["max_output_tokens"])
	if options.NumCtx != nil && int64(*options.NumCtx) > capacity || options.MaxOutputTokens != nil && int64(*options.MaxOutputTokens) > output {
		return evaluationError("BUDGET_EXCEEDED", "requested OpenVINO context or output exceeds the configured server limit")
	}
	return nil
}
func (p *OpenVINOProvider) CheckEvaluationOptions(ctx context.Context, options EvaluationOptions) error {
	c, origin, err := (&OllamaProvider{p.OpenAIProvider}).nativeClient()
	if err != nil {
		return err
	}
	defer closeNativeClient(c)
	m, _, err := (&OllamaProvider{p.OpenAIProvider}).nativeJSONPath(ctx, c, origin, "/noosphere/openvino/metadata", nil)
	if err != nil {
		return err
	}
	return checkOpenVINOOptions(options, m)
}

func openVINOAnswer(result map[string]any, out *EvaluationResponse) error {
	choices, ok := result["choices"].([]any)
	if !ok || len(choices) != 1 {
		return evaluationError("INVALID_OUTPUT", "OpenVINO must return one terminal text answer")
	}
	choice, ok := choices[0].(map[string]any)
	if !ok {
		return evaluationError("INVALID_OUTPUT", "invalid OpenVINO answer")
	}
	message := obj(choice, "message")
	content, present := message["content"]
	text, textOK := content.(string)
	thought, thoughtPresent := message["reasoning_content"]
	thinking, thinkingOK := thought.(string)
	out.Output = text
	out.Thinking = thinking
	out.DoneReason = str(choice, "finish_reason")
	out.Truncated = out.DoneReason == "length"
	if !present || !textOK || (thoughtPresent && thought != nil && !thinkingOK) || (out.DoneReason != "stop" && out.DoneReason != "length") || (message["tool_calls"] != nil && lenValue(message["tool_calls"]) > 0) {
		return evaluationError("INVALID_OUTPUT", "invalid OpenVINO terminal protocol")
	}
	metrics := obj(result, "openvino_metrics")
	for _, metric := range []struct {
		key  string
		dest **int64
		ms   bool
	}{{"num_input_tokens", &out.PromptEvalCount, false}, {"num_generated_tokens", &out.EvalCount, false}, {"generate_duration_ms", &out.TotalDurationNS, true}} {
		value, present := metrics[metric.key]
		if !present {
			continue
		}
		number, ok := value.(json.Number)
		if !ok {
			return evaluationError("INVALID_OUTPUT", "invalid OpenVINO metric")
		}
		v, err := number.Float64()
		if err != nil || math.IsNaN(v) || math.IsInf(v, 0) || v < 0 || (metric.ms && v > float64(math.MaxInt64)/1e6) || (!metric.ms && (v > float64(math.MaxInt64) || math.Trunc(v) != v)) {
			return evaluationError("INVALID_OUTPUT", "invalid OpenVINO metric range")
		}
		if metric.ms {
			v *= 1e6
		}
		n := int64(v)
		*metric.dest = &n
	}
	return nil
}
func (p *OpenVINOProvider) Evaluate(parent context.Context, req EvaluationRequest) (*EvaluationResponse, error) {
	timeout, err := EvaluationTimeout(req.Timeout)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()
	if err = validateEvaluationRequest(req); err != nil {
		return nil, err
	}
	if len(req.Tools) > 0 || len(req.ToolMessages) > 0 {
		return nil, evaluationError("UNSUPPORTED_CAPABILITY", "OpenVINO adapter supports text evaluation only; structured tools are not implemented")
	}
	if req.Kind == "warmup" {
		return nil, evaluationError("UNSUPPORTED_CAPABILITY", "OpenVINO does not support owned warmup or unload")
	}
	native := &OllamaProvider{p.OpenAIProvider}
	c, origin, err := native.nativeClient()
	if err != nil {
		return nil, err
	}
	defer closeNativeClient(c)
	before, metadata, err := p.discovery(ctx, c, origin)
	if err != nil {
		return nil, err
	}
	if len(before.Models) != 1 || before.Models[0].Name != req.ModelName || !sameExpected(before.Models[0], req) {
		return nil, evaluationError("MODEL_DRIFT", "OpenVINO identity differs from the registered model")
	}
	if err = checkOpenVINOOptions(req.Options, metadata); err != nil {
		return nil, err
	}
	start := time.Now()
	result, raw, err := native.nativeJSONPath(ctx, c, origin, "/noosphere/openvino/evaluate", map[string]any{"model": req.ModelName, "messages": req.Messages, "options": req.Options})
	if err != nil {
		return nil, err
	}
	out := &EvaluationResponse{MetricSource: "openvino_genai_native", IdentityBefore: before.Models[0], RequestedOptions: req.Options, RawResponse: raw, WallDuration: time.Since(start), Limitations: before.Models[0].Limitations}
	outputErr := openVINOAnswer(result, out)
	out.Output = p.redact(out.Output)
	out.Thinking = p.redact(out.Thinking)
	if str(result, "engine") != "openvino_genai" || str(result, "model") != req.ModelName || str(result, "model_digest") != before.Models[0].Digest {
		outputErr = evaluationError("MODEL_DRIFT", "OpenVINO response engine or model identity differs")
	}
	var requested map[string]any
	encoded, _ := json.Marshal(req.Options)
	decoder := json.NewDecoder(strings.NewReader(string(encoded)))
	decoder.UseNumber()
	_ = decoder.Decode(&requested)
	applied := obj(result, "applied_options")
	for key, want := range requested {
		if hashValue(applied[key]) != hashValue(want) {
			outputErr = evaluationError("INVALID_OUTPUT", "OpenVINO did not confirm the requested option mapping")
		}
	}
	after, _, err := p.discovery(ctx, c, origin)
	if err != nil {
		return out, err
	}
	if len(after.Models) != 1 || after.Models[0].Name != req.ModelName || !sameExpected(after.Models[0], req) {
		return out, evaluationError("MODEL_DRIFT", "OpenVINO identity changed after inference")
	}
	out.IdentityAfter = after.Models[0]
	return out, outputErr
}
