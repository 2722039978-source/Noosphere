package gateway

import (
	"context"
	"crypto/sha256"
	"fmt"
	"strings"
	"time"

	"github.com/noosphere/backend/internal/gateway/providers"
)

// evaluationModel requires an explicit enabled ID. Unlike chat resolution it
// must never select a default model or silently fall back to another provider.
func (g *Gateway) evaluationModel(id string) (*ModelConfig, error) {
	g.mu.RLock()
	defer g.mu.RUnlock()
	if id != "" && g.config != nil {
		for _, m := range g.config.Models {
			if m.ID == id && m.Enabled {
				if providers.Get(m.Provider) == nil {
					break
				}
				copy := m
				return &copy, nil
			}
		}
	}
	return nil, &providers.EvaluationError{Code: "UNSUPPORTED_CAPABILITY", Message: "evaluation requires an explicit enabled model with a registered provider"}
}
func (g *Gateway) evaluationProvider(id string) (providers.EvaluationProvider, error) {
	model, err := g.evaluationModel(id)
	if err != nil {
		return nil, err
	}
	return g.evaluationProviderForModel(model)
}

func (g *Gateway) evaluationProviderForModel(model *ModelConfig) (providers.EvaluationProvider, error) {
	p, err := g.getOrCreateProvider(model)
	if err != nil {
		return nil, &providers.EvaluationError{Code: "PROVIDER_UNAVAILABLE", Message: "configured evaluation provider could not be initialized", Cause: err}
	}
	capability, ok := p.(providers.EvaluationProvider)
	if !ok {
		return nil, &providers.EvaluationError{Code: "UNSUPPORTED_CAPABILITY", Message: "configured provider does not implement native evaluation"}
	}
	return capability, nil
}

func (g *Gateway) EvaluationSourceFingerprint(id string) (string, error) {
	model, err := g.evaluationModel(id)
	if err != nil {
		return "", err
	}
	return sourceFingerprint(model)
}

func sourceFingerprint(model *ModelConfig) (string, error) {
	if model.Provider != "ollama" {
		return "", &providers.EvaluationError{Code: "UNSUPPORTED_CAPABILITY", Message: "configured provider does not implement local native evaluation"}
	}
	origin, err := providers.NormalizeEvaluationEndpoint(model.BaseURL)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256([]byte(strings.ToLower(model.Provider) + "\x00" + origin))
	return fmt.Sprintf("%x", digest), nil
}

func (g *Gateway) DiscoverEvaluationModels(parent context.Context, id string) (providers.ModelDiscovery, error) {
	ctx, cancel := context.WithTimeout(parent, 2*time.Minute)
	defer cancel()
	if err := ctx.Err(); err != nil {
		return providers.ModelDiscovery{}, providers.EvaluationContextError(err)
	}
	model, err := g.evaluationModel(id)
	if err != nil {
		return providers.ModelDiscovery{}, err
	}
	// Discovery and metadata persistence must refer to the same configured
	// target, including its endpoint and credential revision.
	p, err := g.evaluationProviderForModel(model)
	if err != nil {
		return providers.ModelDiscovery{}, err
	}
	if err = ctx.Err(); err != nil {
		return providers.ModelDiscovery{}, providers.EvaluationContextError(err)
	}
	discovery, err := p.Discover(ctx)
	if err != nil {
		return discovery, err
	}
	if err = ctx.Err(); err != nil {
		return discovery, providers.EvaluationContextError(err)
	}
	if err = g.syncDiscoveredModelMetadata(model, discovery); err != nil {
		return discovery, err
	}
	return discovery, nil
}

func (g *Gateway) Evaluate(parent context.Context, id string, req providers.EvaluationRequest) (*providers.EvaluationResponse, error) {
	timeout, err := providers.EvaluationTimeout(req.Timeout)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()
	if err = ctx.Err(); err != nil {
		return nil, providers.EvaluationContextError(err)
	}
	model, err := g.evaluationModel(id)
	if err != nil {
		return nil, err
	}
	if req.ExpectedSourceFingerprint != "" {
		fingerprint, err := sourceFingerprint(model)
		if err != nil {
			return nil, err
		}
		if fingerprint != req.ExpectedSourceFingerprint {
			return nil, &providers.EvaluationError{Code: "MODEL_DRIFT", Message: "configured evaluation source differs from registered source"}
		}
	}
	// Fingerprint validation and provider creation use this same configuration
	// snapshot, so a concurrent reload cannot bind the check to a different source.
	p, err := g.evaluationProviderForModel(model)
	if err != nil {
		return nil, err
	}
	// Reserve before the legacy semaphore: queued metadata/evaluation must not
	// conceal foreground arrival or consume all foreground slots.
	explicitSession := g.sessionFrom(ctx) != nil
	if !explicitSession {
		var closeSession func()
		ctx, closeSession, err = g.BeginEvaluationSession(ctx)
		if err != nil {
			return nil, providers.EvaluationContextError(err)
		}
		defer closeSession()
	}
	finishSessionCall, err := g.acquireEvaluationCall(ctx, false)
	if err != nil {
		return nil, providers.EvaluationContextError(err)
	}
	defer finishSessionCall()
	if err = g.acquireCall(ctx); err != nil {
		return nil, providers.EvaluationContextError(err)
	}
	defer func() { <-g.sem }()
	if err = ctx.Err(); err != nil {
		return nil, providers.EvaluationContextError(err)
	}
	if explicitSession && req.Kind != "warmup" {
		key, keyErr := evaluationOwnershipKey(model, req)
		if keyErr != nil {
			return nil, keyErr
		}
		g.admission.mu.Lock()
		owned := g.sessionFrom(ctx).ownedKey == key
		g.admission.mu.Unlock()
		if !owned {
			return nil, &providers.EvaluationError{Code: "MODEL_BUSY", Message: "formal inference requires the session-owned model identity"}
		}
	}
	if req.Kind == "warmup" {
		key, keyErr := evaluationOwnershipKey(model, req)
		if keyErr != nil {
			return nil, keyErr
		}
		s := g.sessionFrom(ctx)
		g.admission.mu.Lock()
		verified := s.initialKey == key && s.ownedKey == ""
		g.admission.mu.Unlock()
		if !verified {
			return nil, &providers.EvaluationError{Code: "MODEL_BUSY", Message: "warmup requires a verified empty residency window"}
		}
		capability, ok := p.(providers.EvaluationResidencyProvider)
		if !ok {
			return nil, &providers.EvaluationError{Code: "UNSUPPORTED_CAPABILITY", Message: "provider does not implement residency"}
		}
		before, residencyErr := capability.Residency(ctx, req)
		if residencyErr != nil {
			return nil, residencyErr
		}
		if before.TargetResident || len(before.Models) > 0 {
			return nil, &providers.EvaluationError{Code: "MODEL_BUSY", Message: "preexisting model residency is not owned by evaluation"}
		}
		g.admission.mu.Lock()
		s.ownedKey = key
		g.admission.mu.Unlock()
	}
	finishInference := g.countInference(true)
	defer finishInference()
	return p.Evaluate(ctx, req)
}

func evaluationOwnershipKey(model *ModelConfig, req providers.EvaluationRequest) (string, error) {
	fingerprint, err := sourceFingerprint(model)
	if err != nil {
		return "", err
	}
	if req.ExpectedSourceFingerprint != "" && fingerprint != req.ExpectedSourceFingerprint {
		return "", &providers.EvaluationError{Code: "MODEL_DRIFT", Message: "configured evaluation source differs from registered source"}
	}
	if req.ModelName == "" || req.ExpectedDigest == "" || req.ExpectedMetadataHash == "" || req.ExpectedEngineVersion == "" {
		return "", &providers.EvaluationError{Code: "INVALID_REQUEST", Message: "complete expected identity required for residency"}
	}
	key := sha256.Sum256([]byte(model.ID + "\x00" + fingerprint + "\x00" + req.ModelName + "\x00" + req.ExpectedDigest + "\x00" + req.ExpectedMetadataHash + "\x00" + req.ExpectedEngineVersion))
	return fmt.Sprintf("%x", key), nil
}

func (g *Gateway) residencyProvider(ctx context.Context, id string, req providers.EvaluationRequest) (providers.EvaluationResidencyProvider, string, error) {
	model, err := g.evaluationModel(id)
	if err != nil {
		return nil, "", err
	}
	key, err := evaluationOwnershipKey(model, req)
	if err != nil {
		return nil, "", err
	}
	p, err := g.evaluationProviderForModel(model)
	if err != nil {
		return nil, "", err
	}
	capability, ok := p.(providers.EvaluationResidencyProvider)
	if !ok {
		return nil, "", &providers.EvaluationError{Code: "UNSUPPORTED_CAPABILITY", Message: "provider does not implement residency"}
	}
	return capability, key, nil
}

func (g *Gateway) InspectEvaluationResidency(ctx context.Context, id string, req providers.EvaluationRequest) (providers.ModelResidencySnapshot, error) {
	var empty providers.ModelResidencySnapshot
	finish, err := g.acquireEvaluationCall(ctx, true)
	if err != nil {
		return empty, err
	}
	defer finish()
	p, key, err := g.residencyProvider(ctx, id, req)
	if err != nil {
		return empty, err
	}
	snapshot, err := p.Residency(ctx, req)
	if err == nil {
		s := g.sessionFrom(ctx)
		g.admission.mu.Lock()
		owned := s.ownedKey != ""
		if s.ownedKey == "" {
			if len(snapshot.Models) == 0 && !snapshot.TargetResident {
				s.initialKey = key
			} else {
				s.initialKey = ""
			}
		}
		g.admission.mu.Unlock()
		if owned && len(snapshot.Models) > 1 {
			return snapshot, &providers.EvaluationError{Code: "MODEL_BUSY", Message: "external residency prevents a verified exclusive warm window"}
		}
	}
	return snapshot, err
}

func (g *Gateway) ReleaseEvaluationModel(ctx context.Context, id string, req providers.EvaluationRequest) (providers.ModelResidencySnapshot, error) {
	var empty providers.ModelResidencySnapshot
	finish, err := g.acquireEvaluationCall(ctx, true)
	if err != nil {
		return empty, err
	}
	defer finish()
	p, key, err := g.residencyProvider(ctx, id, req)
	if err != nil {
		return empty, err
	}
	s := g.sessionFrom(ctx)
	g.admission.mu.Lock()
	owned := s.ownedKey == key
	g.admission.mu.Unlock()
	if !owned {
		return empty, &providers.EvaluationError{Code: "MODEL_BUSY", Message: "session does not own the requested model"}
	}
	snapshot, err := p.Residency(ctx, req)
	if err != nil {
		return snapshot, err
	}
	if snapshot.TargetResident {
		if len(snapshot.Models) > 1 {
			return snapshot, &providers.EvaluationError{Code: "MODEL_BUSY", Message: "external residency prevents owned model release"}
		}
		finishInference := g.countInference(true)
		snapshot, err = p.ReleaseModel(ctx, req)
		finishInference()
	}
	if err == nil && snapshot.TargetResident {
		err = &providers.EvaluationError{Code: "RELEASE_UNCONFIRMED", Message: "owned target remains resident"}
	}
	if err == nil {
		g.admission.mu.Lock()
		s.ownedKey = ""
		s.initialKey = ""
		g.admission.mu.Unlock()
	}
	return snapshot, err
}
