// Package modellab registers exact model identities and prepares native
// evaluation calls. Task orchestration and scoring are separate later steps.
package modellab

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/noosphere/backend/internal/gateway"
	"github.com/noosphere/backend/internal/gateway/providers"
	"github.com/noosphere/backend/internal/modelstore"
)

type Service struct {
	store       *modelstore.Store
	gateway     *gateway.Gateway
	runtime     *evaluationRuntime
	httpMu      sync.Mutex
	httpStopped bool
	httpWork    sync.WaitGroup
}

func New(store *modelstore.Store, gateway *gateway.Gateway) *Service {
	return &Service{store: store, gateway: gateway}
}

func (s *Service) Discover(ctx context.Context, gatewayModelID string) (providers.ModelDiscovery, error) {
	return s.gateway.DiscoverEvaluationModels(ctx, gatewayModelID)
}

func identityError(code, message string) error {
	return &providers.EvaluationError{Code: code, Message: message}
}

func (s *Service) Register(ctx context.Context, gatewayModelID, modelName, confirmedMetadataHash string) (modelstore.ModelRevision, error) {
	if confirmedMetadataHash == "" {
		return modelstore.ModelRevision{}, identityError("INVALID_REQUEST", "confirm the discovered metadata hash before registration")
	}
	fingerprint, err := s.gateway.EvaluationSourceFingerprint(gatewayModelID)
	if err != nil {
		return modelstore.ModelRevision{}, err
	}
	discovery, err := s.Discover(ctx, gatewayModelID)
	if err != nil {
		return modelstore.ModelRevision{}, err
	}
	after, err := s.gateway.EvaluationSourceFingerprint(gatewayModelID)
	if err != nil {
		return modelstore.ModelRevision{}, err
	}
	if after != fingerprint {
		return modelstore.ModelRevision{}, identityError("MODEL_DRIFT", "configured source changed during discovery")
	}
	for _, model := range discovery.Models {
		if model.Name != modelName {
			continue
		}
		if !model.LocalVerified {
			return modelstore.ModelRevision{}, identityError("UNSUPPORTED_CAPABILITY", model.IneligibilityReason)
		}
		if model.MetadataHash != confirmedMetadataHash {
			return modelstore.ModelRevision{}, identityError("MODEL_DRIFT", "discovered model metadata changed; confirm the new revision")
		}
		id, err := contentID("model", []string{gatewayModelID, fingerprint, model.MetadataHash})
		if err != nil {
			return modelstore.ModelRevision{}, err
		}
		revision := modelstore.ModelRevision{ID: id, GatewayModelID: gatewayModelID, Provider: "ollama", ModelName: model.Name, IdentityLevel: "digest", Digest: model.Digest, ConfigHash: model.ConfigHash, MetadataHash: model.MetadataHash, TemplateHash: model.TemplateHash, EndpointFingerprint: fingerprint, EngineVersion: model.EngineVersion, Format: model.Format, Quantization: model.Quantization, Capabilities: append([]string(nil), model.Capabilities...), ParameterSize: model.ParameterSize, SizeBytes: modelstore.Int(model.SizeBytes), UnknownReason: strings.Join(append([]string{"parameter count was not independently inspected; reported model file size is not a VRAM measurement"}, model.Limitations...), "; ")}
		// The content ID excludes time. A repeated confirmed discovery returns
		// the original immutable record, including its first observed timestamp.
		if existing, readErr := s.store.GetModelRevision(ctx, id); readErr == nil {
			if !sameRegisteredMetadata(existing, revision) {
				return modelstore.ModelRevision{}, fmt.Errorf("%w: discovered metadata collides with registered revision", modelstore.ErrInvalid)
			}
			return existing, nil
		} else if !errors.Is(readErr, modelstore.ErrNotFound) {
			return modelstore.ModelRevision{}, readErr
		}
		revision.FirstDiscoveredAt = time.Now().UTC().Format(time.RFC3339Nano)
		if err = s.store.SaveModelRevision(ctx, revision); err != nil {
			// A concurrent identical registration may have committed first.
			// Accept its timestamp only when every other persisted field agrees.
			if existing, readErr := s.store.GetModelRevision(ctx, id); readErr == nil && sameRegisteredMetadata(existing, revision) {
				return existing, nil
			}
			return modelstore.ModelRevision{}, err
		}
		return revision, nil
	}
	return modelstore.ModelRevision{}, identityError("INVALID_REQUEST", "selected model is not present in discovery")
}

func (s *Service) GetModel(ctx context.Context, id string) (modelstore.ModelRevision, error) {
	return s.store.GetModelRevision(ctx, id)
}

// InferenceSpec is a single registered call, not a paired evaluation job.
// T5 owns the paired EvaluationSpec and durable task orchestration.
type InferenceSpec struct {
	ModelRevisionID string
	ProfileID       string
	AttemptID       string
	Kind            string
	Messages        []providers.Message
}

func (s *Service) Evaluate(ctx context.Context, spec InferenceSpec) (*providers.EvaluationResponse, error) {
	model, err := s.GetModel(ctx, spec.ModelRevisionID)
	if err != nil {
		return nil, err
	}
	if model.MetadataHash == "" || model.EndpointFingerprint == "" || model.EngineVersion == "" || model.IdentityLevel != "digest" {
		return nil, identityError("INSUFFICIENT_EVIDENCE", "registered identity needs fresh discovery before evaluation")
	}
	profile, err := s.store.GetInferenceProfile(ctx, spec.ProfileID)
	if err != nil {
		return nil, err
	}
	options, timeout, err := evaluationOptions(profile)
	if err != nil {
		return nil, err
	}
	fingerprint, err := s.gateway.EvaluationSourceFingerprint(model.GatewayModelID)
	if err != nil {
		return nil, err
	}
	if fingerprint != model.EndpointFingerprint {
		return nil, identityError("MODEL_DRIFT", "registered source configuration changed")
	}
	return s.gateway.Evaluate(ctx, model.GatewayModelID, providers.EvaluationRequest{ModelName: model.ModelName, ExpectedDigest: model.Digest, ExpectedMetadataHash: model.MetadataHash, ExpectedEngineVersion: model.EngineVersion, ExpectedSourceFingerprint: model.EndpointFingerprint, AttemptID: spec.AttemptID, Kind: spec.Kind, Messages: spec.Messages, Options: options, Timeout: timeout})
}

func contentID(prefix string, v any) (string, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	hash := sha256.Sum256(raw)
	return fmt.Sprintf("%s_%x", prefix, hash[:16]), nil
}

func sameRegisteredMetadata(a, b modelstore.ModelRevision) bool {
	a.FirstDiscoveredAt = ""
	b.FirstDiscoveredAt = ""
	left, err := json.Marshal(a)
	if err != nil {
		return false
	}
	right, err := json.Marshal(b)
	return err == nil && bytes.Equal(left, right)
}
