package providers

import (
	"context"
	"net/http"
	"time"
)

var _ EvaluationResidencyProvider = (*OllamaProvider)(nil)

// Residency reports daemon allocation, not physical peak or free GPU memory.
func (p *OllamaProvider) Residency(parent context.Context, req EvaluationRequest) (ModelResidencySnapshot, error) {
	ctx, cancel, err := residencyContext(parent, req)
	if err != nil {
		return ModelResidencySnapshot{}, err
	}
	defer cancel()
	c, origin, err := p.nativeClient()
	if err != nil {
		return ModelResidencySnapshot{}, err
	}
	defer closeNativeClient(c)
	if err = p.checkResidencyIdentity(ctx, c, origin, req); err != nil {
		return ModelResidencySnapshot{}, err
	}
	return p.residencySnapshot(ctx, c, origin, req)
}

// ReleaseModel assumes the direct caller owns the model. Gateway callers must
// additionally hold matching session ownership. One terminal unload request is
// followed by a fresh absence observation; there are no implicit retries.
func (p *OllamaProvider) ReleaseModel(parent context.Context, req EvaluationRequest) (ModelResidencySnapshot, error) {
	ctx, cancel, err := residencyContext(parent, req)
	if err != nil {
		return ModelResidencySnapshot{}, err
	}
	defer cancel()
	c, origin, err := p.nativeClient()
	if err != nil {
		return ModelResidencySnapshot{}, err
	}
	defer closeNativeClient(c)
	if err = p.checkResidencyIdentity(ctx, c, origin, req); err != nil {
		return ModelResidencySnapshot{}, err
	}
	terminal, _, err := p.nativeJSON(ctx, c, origin, "generate", map[string]any{"model": req.ModelName, "prompt": "", "stream": false, "keep_alive": 0})
	if err != nil {
		return ModelResidencySnapshot{}, releaseUnconfirmed(err)
	}
	done, ok := terminal["done"].(bool)
	if !ok || !done || !validModelName(str(terminal, "model")) || canonicalModel(str(terminal, "model")) != canonicalModel(req.ModelName) || remoteMetadata(terminal) {
		return ModelResidencySnapshot{}, releaseUnconfirmed(nil)
	}
	for _, field := range []string{"response", "thinking", "done_reason"} {
		if value, present := terminal[field]; present {
			if _, ok := value.(string); !ok {
				return ModelResidencySnapshot{}, releaseUnconfirmed(nil)
			}
		}
	}
	if value, present := terminal["digest"]; present {
		digest, ok := value.(string)
		if !ok || normalizeDigest(digest) != normalizeDigest(req.ExpectedDigest) {
			return ModelResidencySnapshot{}, releaseUnconfirmed(nil)
		}
	}
	for _, field := range []string{"total_duration", "load_duration", "prompt_eval_count", "prompt_eval_duration", "eval_count", "eval_duration"} {
		if value, present := terminal[field]; present {
			n, ok := intValue(value)
			if !ok || n < 0 {
				return ModelResidencySnapshot{}, releaseUnconfirmed(nil)
			}
		}
	}
	snapshot, err := p.residencySnapshot(ctx, c, origin, req)
	if err != nil {
		return snapshot, releaseUnconfirmed(err)
	}
	if snapshot.TargetResident {
		return snapshot, releaseUnconfirmed(nil)
	}
	// As with inference, installed metadata can drift while a lifecycle call runs.
	if err = p.checkResidencyIdentity(ctx, c, origin, req); err != nil {
		return snapshot, releaseUnconfirmed(err)
	}
	return snapshot, nil
}

func releaseUnconfirmed(cause error) error {
	return &EvaluationError{Code: "RELEASE_UNCONFIRMED", Message: "native model release could not be confirmed", Cause: cause}
}

func residencyContext(parent context.Context, req EvaluationRequest) (context.Context, context.CancelFunc, error) {
	timeout, err := EvaluationTimeout(req.Timeout)
	if err != nil {
		return nil, nil, err
	}
	if err = parent.Err(); err != nil {
		return nil, nil, EvaluationContextError(err)
	}
	// Lifecycle checks require the frozen identity, not generation messages/options.
	if !validModelName(req.ModelName) || normalizeDigest(req.ExpectedDigest) == "" || normalizeDigest(req.ExpectedMetadataHash) == "" || req.ExpectedEngineVersion == "" || len(req.ExpectedEngineVersion) > 256 {
		return nil, nil, evaluationError("INVALID_REQUEST", "model name and complete expected identity are required")
	}
	ctx, cancel := context.WithTimeout(parent, timeout)
	return ctx, cancel, nil
}

func (p *OllamaProvider) checkResidencyIdentity(ctx context.Context, c *http.Client, origin string, req EvaluationRequest) error {
	discovery, err := p.discover(ctx, c, origin, req.ModelName)
	if err != nil {
		return err
	}
	if len(discovery.Models) != 1 || !sameExpected(discovery.Models[0], req) {
		return evaluationError("MODEL_DRIFT", "selected local identity no longer matches registered identity")
	}
	return nil
}

func (p *OllamaProvider) residencySnapshot(ctx context.Context, c *http.Client, origin string, req EvaluationRequest) (ModelResidencySnapshot, error) {
	native, raw, err := p.nativeJSON(ctx, c, origin, "ps", nil)
	if err != nil {
		return ModelResidencySnapshot{}, err
	}
	entries, ok := native["models"].([]any)
	if !ok {
		return ModelResidencySnapshot{}, evaluationError("INVALID_OUTPUT", "native residency must include a model list")
	}
	if len(entries) > 128 {
		return ModelResidencySnapshot{}, evaluationError("RESPONSE_TOO_LARGE", "native residency exceeds 128 models")
	}
	snapshot := ModelResidencySnapshot{Models: make([]ResidentModel, 0, len(entries)), RawResponse: raw}
	seenNames, seenDigests := map[string]bool{}, map[string]bool{}
	for _, entry := range entries {
		model, ok := entry.(map[string]any)
		if !ok {
			return ModelResidencySnapshot{}, evaluationError("INVALID_OUTPUT", "invalid native resident model")
		}
		name := str(model, "name")
		if name == "" {
			name = str(model, "model")
		}
		if !validModelName(name) {
			return ModelResidencySnapshot{}, evaluationError("INVALID_OUTPUT", "invalid native resident model name")
		}
		for _, field := range []string{"name", "model"} {
			if value, present := model[field]; present {
				alias, ok := value.(string)
				if !ok || !validModelName(alias) || canonicalModel(alias) != canonicalModel(name) {
					return ModelResidencySnapshot{}, evaluationError("INVALID_OUTPUT", "ambiguous native resident model name")
				}
			}
		}
		if cloudAlias(name) || remoteMetadata(model) {
			return ModelResidencySnapshot{}, evaluationError("UNSAFE_ENDPOINT", "native residency reports a remote model")
		}
		digest := normalizeDigest(str(model, "digest"))
		if digest == "" {
			return ModelResidencySnapshot{}, evaluationError("INVALID_OUTPUT", "native residency requires a full model digest")
		}
		canonical := canonicalModel(name)
		if seenNames[canonical] || seenDigests[digest] {
			return ModelResidencySnapshot{}, evaluationError("INVALID_OUTPUT", "ambiguous duplicate native resident model")
		}
		seenNames[canonical], seenDigests[digest] = true, true
		resident := ResidentModel{Name: canonical, Digest: digest}
		for _, field := range []struct {
			name string
			dest **int64
		}{{"size", &resident.SizeBytes}, {"size_vram", &resident.SizeVRAMBytes}, {"context_length", nil}} {
			if value, present := model[field.name]; present {
				n, ok := intValue(value)
				if !ok || n < 0 {
					return ModelResidencySnapshot{}, evaluationError("INVALID_OUTPUT", "native residency sizes must be nonnegative integers")
				}
				if field.dest != nil {
					*field.dest = &n
				}
			}
		}
		if canonical == canonicalModel(req.ModelName) {
			if digest != normalizeDigest(req.ExpectedDigest) {
				return ModelResidencySnapshot{}, evaluationError("MODEL_DRIFT", "resident model digest differs from registered identity")
			}
			snapshot.TargetResident = true
		}
		snapshot.Models = append(snapshot.Models, resident)
	}
	if err = ctx.Err(); err != nil {
		return ModelResidencySnapshot{}, EvaluationContextError(err)
	}
	snapshot.ObservedAt = time.Now().UTC()
	return snapshot, nil
}
