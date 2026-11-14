package providers

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// LocalCompatibleProvider 使用 llama.cpp 的本机元数据进行测评，聊天沿用兼容协议。
type LocalCompatibleProvider struct {
	*OpenAIProvider
	identityMu sync.Mutex
	filePath   string
	fileInfo   os.FileInfo
	fileDigest string
}

func NewLocalCompatible(baseURL, apiKey string) (LLMProvider, error) {
	p, err := NewOpenAI(baseURL, apiKey)
	if err != nil {
		return nil, err
	}
	return &LocalCompatibleProvider{OpenAIProvider: p.(*OpenAIProvider)}, nil
}

func (p *LocalCompatibleProvider) weightDigest(ctx context.Context, path string) (string, int64, error) {
	if !filepath.IsAbs(path) || strings.HasPrefix(path, `\\`) || !strings.EqualFold(filepath.Ext(path), ".gguf") {
		return "", 0, evaluationError("INSUFFICIENT_EVIDENCE", "llama.cpp must report an absolute local GGUF path")
	}
	p.identityMu.Lock()
	defer p.identityMu.Unlock()
	file, err := os.Open(path)
	if err != nil {
		return "", 0, evaluationError("INSUFFICIENT_EVIDENCE", "reported GGUF file is not readable on this host")
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > 128<<30 {
		return "", 0, evaluationError("INSUFFICIENT_EVIDENCE", "reported GGUF file is not a bounded regular file")
	}
	if p.filePath == path && p.fileInfo != nil && os.SameFile(info, p.fileInfo) && info.Size() == p.fileInfo.Size() && info.ModTime() == p.fileInfo.ModTime() {
		return p.fileDigest, info.Size(), nil
	}
	h := sha256.New()
	if _, err = io.Copy(h, &evaluationContextReader{ctx: ctx, r: file}); err != nil {
		return "", 0, EvaluationContextError(err)
	}
	after, err := file.Stat()
	if err != nil || after.Size() != info.Size() || after.ModTime() != info.ModTime() {
		return "", 0, evaluationError("MODEL_DRIFT", "GGUF file changed while hashing")
	}
	p.filePath, p.fileInfo, p.fileDigest = path, info, hex.EncodeToString(h.Sum(nil))
	return p.fileDigest, info.Size(), nil
}

type evaluationContextReader struct {
	ctx context.Context
	r   io.Reader
}

func (r *evaluationContextReader) Read(b []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.r.Read(b)
}

func (p *LocalCompatibleProvider) localDiscovery(ctx context.Context, c *http.Client, origin string) (ModelDiscovery, map[string]any, error) {
	d := ModelDiscovery{Models: []ModelIdentity{}}
	native := &OllamaProvider{p.OpenAIProvider}
	props, _, err := native.nativeJSONPath(ctx, c, origin, "/props", nil)
	if err != nil {
		return d, nil, err
	}
	version := str(props, "build_info")
	if version == "" || len(version) > 256 {
		return d, nil, evaluationError("UNSUPPORTED_CAPABILITY", "local compatible evaluation requires llama.cpp build metadata")
	}
	d.EngineVersion = "llama.cpp/" + version
	models, _, err := native.nativeJSONPath(ctx, c, origin, "/v1/models", nil)
	if err != nil {
		return d, nil, err
	}
	list, ok := models["data"].([]any)
	if !ok || len(list) != 1 {
		return d, nil, evaluationError("UNSUPPORTED_CAPABILITY", "local compatible evaluation requires one fixed llama.cpp model")
	}
	tag, ok := list[0].(map[string]any)
	if !ok || str(tag, "owned_by") != "llamacpp" || !validModelName(str(tag, "id")) {
		return d, nil, evaluationError("UNSUPPORTED_CAPABILITY", "llama.cpp model metadata is required")
	}
	name := str(tag, "id")
	if alias := str(props, "model_alias"); alias != "" && alias != name {
		return d, nil, evaluationError("MODEL_DRIFT", "llama.cpp alias and model list disagree")
	}
	digest, size, err := p.weightDigest(ctx, str(props, "model_path"))
	if err != nil {
		return d, nil, err
	}
	meta := obj(tag, "meta")
	config := map[string]any{"generation": props["default_generation_settings"], "template": props["chat_template"], "slots": props["total_slots"], "model_file": hashValue(str(props, "model_path"))}
	id := ModelIdentity{Provider: "openai_compatible", Name: name, Digest: digest, EngineVersion: d.EngineVersion, ConfigHash: hashValue(config), TemplateHash: hashValue(props["chat_template"]), Format: "gguf", SizeBytes: size, Quantization: str(meta, "ftype"), Capabilities: []string{"completion"}, LocalVerified: true, Limitations: []string{"GGUF SHA256 is computed from the local file reported by the loopback daemon; this does not attest the daemon's loaded memory.", "File hash caching uses file identity, size and modification time; hostile edits preserving those facts cannot be detected.", "The externally managed llama.cpp model remains resident; no exclusive warmup, unload, load duration or per-model memory measurement is claimed.", "Generation options are requested values; context is checked against the existing server capacity."}}
	if count, ok := intValue(meta["n_params"]); ok && count > 0 {
		id.ParameterSize = strconv.FormatFloat(float64(count)/1e9, 'f', 2, 64) + "B"
	}
	id.MetadataHash = hashValue(map[string]any{"provider": id.Provider, "name": name, "digest": digest, "size": size, "version": d.EngineVersion, "config": id.ConfigHash, "meta": meta})
	d.Models = append(d.Models, id)
	return d, props, nil
}

func (p *LocalCompatibleProvider) Discover(parent context.Context) (ModelDiscovery, error) {
	ctx, cancel := context.WithTimeout(parent, 2*time.Minute)
	defer cancel()
	c, origin, err := (&OllamaProvider{p.OpenAIProvider}).nativeClient()
	if err != nil {
		return ModelDiscovery{}, err
	}
	defer closeNativeClient(c)
	d, _, err := p.localDiscovery(ctx, c, origin)
	return d, err
}

func (p *LocalCompatibleProvider) Evaluate(parent context.Context, req EvaluationRequest) (*EvaluationResponse, error) {
	timeout, err := EvaluationTimeout(req.Timeout)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()
	if err = validateEvaluationRequest(req); err != nil {
		return nil, err
	}
	if err = validateEvaluationTools(req); err != nil {
		return nil, err
	}
	if req.Kind == "warmup" {
		return nil, evaluationError("UNSUPPORTED_CAPABILITY", "externally managed llama.cpp does not support owned warmup or unload")
	}
	native := &OllamaProvider{p.OpenAIProvider}
	c, origin, err := native.nativeClient()
	if err != nil {
		return nil, err
	}
	defer closeNativeClient(c)
	before, props, err := p.localDiscovery(ctx, c, origin)
	if err != nil {
		return nil, err
	}
	if len(before.Models) != 1 || before.Models[0].Name != req.ModelName || !sameExpected(before.Models[0], req) {
		return nil, evaluationError("MODEL_DRIFT", "local GGUF identity differs from registered model")
	}
	capacity, ok := intValue(obj(props, "default_generation_settings")["n_ctx"])
	if !ok || capacity <= 0 || (req.Options.NumCtx != nil && int64(*req.Options.NumCtx) > capacity) {
		return nil, evaluationError("BUDGET_EXCEEDED", "requested context exceeds the configured llama.cpp capacity")
	}
	body := map[string]any{"model": req.ModelName, "messages": req.Messages, "stream": false, "cache_prompt": false}
	if len(req.Tools) > 0 {
		body["messages"], body["tools"], body["tool_choice"] = req.ToolMessages, req.Tools, req.ToolChoice
		body["parallel_tool_calls"] = false
	}
	o := req.Options
	if o.Temperature != nil {
		body["temperature"] = *o.Temperature
	}
	if o.Seed != nil {
		body["seed"] = *o.Seed
	}
	if o.TopP != nil {
		body["top_p"] = *o.TopP
	}
	if o.TopK != nil {
		body["top_k"] = *o.TopK
	}
	if o.MaxOutputTokens != nil {
		body["max_tokens"] = *o.MaxOutputTokens
	}
	if o.Stop != nil {
		body["stop"] = o.Stop
	}
	if o.Think != nil {
		body["chat_template_kwargs"] = map[string]any{"enable_thinking": *o.Think}
	}
	var contextBudget *ContextBudgetEvidence
	if req.VerifyContextBudget {
		contextBudget, err = p.verifyLlamaContext(ctx, c, origin, req, body, capacity)
		if err != nil {
			return &EvaluationResponse{IdentityBefore: before.Models[0], RequestedOptions: o, ContextBudget: contextBudget}, err
		}
	}
	start := time.Now()
	var result map[string]any
	var raw json.RawMessage
	var firstToken, firstAnswer *int64
	var usage *ProcessUsage
	if req.ObserveStream && len(req.Tools) == 0 {
		result, raw, firstToken, firstAnswer, usage, err = p.readLlamaStream(ctx, c, origin, req.ModelName, body, start)
	} else {
		result, raw, err = native.nativeJSONPath(ctx, c, origin, "/v1/chat/completions", body)
	}
	if err != nil {
		var rejection *EvaluationError
		if len(req.Tools) > 0 && errors.As(err, &rejection) && (rejection.HTTPStatus == 400 || rejection.HTTPStatus == 422 || rejection.HTTPStatus == 501) {
			return nil, &EvaluationError{Code: "UNSUPPORTED_CAPABILITY", Message: "native tool request was rejected; tool protocol support is unconfirmed", HTTPStatus: rejection.HTTPStatus}
		}
		return nil, err
	}
	out := &EvaluationResponse{MetricSource: "llamacpp_native", IdentityBefore: before.Models[0], RequestedOptions: o, RawResponse: raw, WallDuration: time.Since(start), FirstTokenNS: firstToken, FirstAnswerNS: firstAnswer, ProcessUsage: usage, Limitations: before.Models[0].Limitations}
	out.ContextBudget = contextBudget
	var outputErr error
	choices, ok := result["choices"].([]any)
	if !ok || len(choices) != 1 {
		outputErr = evaluationError("INVALID_OUTPUT", "llama.cpp must return one terminal answer")
	} else if choice, ok := choices[0].(map[string]any); ok {
		message, messageOK := choice["message"].(map[string]any)
		content, contentPresent := message["content"]
		text, textOK := content.(string)
		// A terminal response may have an empty/null final answer after using
		// its token budget on reasoning. That is model output for the host
		// scorer, not a failed transport. Missing/ill-typed protocol fields
		// still return errors, so the worker cannot score corrupt responses.
		contentOK := contentPresent && (textOK || content == nil)
		thinking, thinkingPresent := message["reasoning_content"]
		thought, thinkingOK := thinking.(string)
		out.Output = p.redact(text)
		out.Thinking = p.redact(thought)
		out.DoneReason = str(choice, "finish_reason")
		out.Truncated = out.DoneReason == "length"
		if len(req.Tools) > 0 {
			out.ToolCalls, outputErr = parseEvaluationToolCalls(message["tool_calls"])
			// OpenAI tool turns may omit content entirely; final text turns may not.
			if !contentPresent && len(out.ToolCalls) > 0 {
				contentOK = true
			}
			if out.DoneReason == "tool_calls" && len(out.ToolCalls) == 0 || len(out.ToolCalls) > 0 && out.DoneReason != "tool_calls" && out.DoneReason != "length" {
				outputErr = evaluationError("INVALID_OUTPUT", "tool calls and terminal reason disagree")
			}
		}
		validDone := out.DoneReason == "stop" || out.DoneReason == "length" || len(req.Tools) > 0 && out.DoneReason == "tool_calls"
		if !messageOK || !contentOK || thinkingPresent && thinking != nil && !thinkingOK || !validDone || len(req.Tools) == 0 && (message["tool_calls"] != nil && lenValue(message["tool_calls"]) > 0) {
			outputErr = evaluationError("INVALID_OUTPUT", "llama.cpp returned an invalid terminal text protocol")
		}
	} else {
		outputErr = evaluationError("INVALID_OUTPUT", "invalid llama.cpp answer")
	}
	if str(result, "model") != req.ModelName {
		outputErr = evaluationError("MODEL_DRIFT", "llama.cpp response model differs from registered identity")
	}
	timings := obj(result, "timings")
	for _, metric := range []struct {
		key  string
		dest **int64
		ms   bool
	}{{"predicted_n", &out.EvalCount, false}, {"predicted_ms", &out.EvalDurationNS, true}, {"prompt_n", &out.PromptEvalCount, false}, {"prompt_ms", &out.PromptEvalDurationNS, true}} {
		value, present := timings[metric.key]
		if !present {
			continue
		}
		number, ok := value.(json.Number)
		if !ok {
			outputErr = evaluationError("INVALID_OUTPUT", "invalid llama.cpp timings")
			continue
		}
		v, err := number.Float64()
		if err != nil || math.IsNaN(v) || math.IsInf(v, 0) || v < 0 || (metric.ms && v > float64(math.MaxInt64)/1e6) || (!metric.ms && (v > float64(math.MaxInt64) || math.Trunc(v) != v)) {
			outputErr = evaluationError("INVALID_OUTPUT", "invalid llama.cpp timing range")
			continue
		}
		if metric.ms {
			v *= 1e6
		}
		n := int64(v)
		*metric.dest = &n
	}
	if contextErr := checkReportedPrompt(out.ContextBudget, out.PromptEvalCount); contextErr != nil && outputErr == nil {
		outputErr = contextErr
	}
	after, _, err := p.localDiscovery(ctx, c, origin)
	if err != nil {
		return out, err
	}
	if len(after.Models) != 1 || after.Models[0].Name != req.ModelName || !sameExpected(after.Models[0], req) {
		return out, evaluationError("MODEL_DRIFT", "local GGUF identity changed after inference")
	}
	out.IdentityAfter = after.Models[0]
	return out, outputErr
}

// Metadata only: never infer, load, unload or reserve the externally managed model.
func (p *LocalCompatibleProvider) CheckEvaluationOptions(ctx context.Context, options EvaluationOptions) error {
	if err := ValidateEvaluationOptions(options); err != nil {
		return err
	}
	native := &OllamaProvider{p.OpenAIProvider}
	c, origin, err := native.nativeClient()
	if err != nil {
		return err
	}
	defer closeNativeClient(c)
	props, _, err := native.nativeJSONPath(ctx, c, origin, "/props", nil)
	if err != nil {
		return err
	}
	capacity, ok := intValue(obj(props, "default_generation_settings")["n_ctx"])
	if !ok || capacity <= 0 || (options.NumCtx != nil && int64(*options.NumCtx) > capacity) {
		return evaluationError("BUDGET_EXCEEDED", "requested context exceeds the configured llama.cpp capacity")
	}
	return nil
}

func lenValue(v any) int {
	if values, ok := v.([]any); ok {
		return len(values)
	}
	return 1
}
