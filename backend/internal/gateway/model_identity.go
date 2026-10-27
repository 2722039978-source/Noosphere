package gateway

import (
	"strings"

	"github.com/noosphere/backend/internal/gateway/providers"
)

// discoveryModelName only applies Ollama's omitted default tag equivalence.
// Namespaces and explicit tags remain part of the configured model identity.
func discoveryModelName(provider, name string) string {
	if provider == "ollama" && name != "" && !strings.Contains(name[strings.LastIndex(name, "/")+1:], ":") {
		return name + ":latest"
	}
	return name
}

// syncDiscoveredModelMetadata copies reported identity facts to the configured
// model. It does not change explicit vendor/family overrides, model selection,
// or any runtime/verification status.
func (g *Gateway) syncDiscoveredModelMetadata(snapshot *ModelConfig, discovery providers.ModelDiscovery) error {
	g.mu.Lock()
	defer g.mu.Unlock()

	index := -1
	if g.config != nil {
		for i, current := range g.config.Models {
			if current.ID == snapshot.ID {
				if !current.Enabled || current.Provider != snapshot.Provider || current.BaseURL != snapshot.BaseURL || current.APIKey != snapshot.APIKey || current.ModelName != snapshot.ModelName {
					return &providers.EvaluationError{Code: "MODEL_DRIFT", Message: "configured model target changed during discovery; metadata was not saved"}
				}
				index = i
				break
			}
		}
	}
	if index < 0 {
		return &providers.EvaluationError{Code: "MODEL_DRIFT", Message: "configured model was removed during discovery; metadata was not saved"}
	}

	var identity *providers.ModelIdentity
	if snapshot.ModelName != "" {
		for i := range discovery.Models {
			candidate := &discovery.Models[i]
			if discoveryModelName(snapshot.Provider, candidate.Name) == discoveryModelName(snapshot.Provider, snapshot.ModelName) {
				if identity != nil {
					return &providers.EvaluationError{Code: "INVALID_OUTPUT", Message: "discovery reported ambiguous configured model identities; metadata was not saved"}
				}
				identity = candidate
			}
		}
	}
	// Discovery lists other installed models too. Never borrow their identity
	// when the configured model is absent or has no reported identity facts.
	if identity == nil {
		return nil
	}
	family := strings.TrimSpace(identity.Family)
	parameterSize := strings.TrimSpace(identity.ParameterSize)
	quantization := strings.TrimSpace(identity.Quantization)
	if family == "" && parameterSize == "" && quantization == "" {
		return nil
	}

	current := g.config.Models[index]
	// Replace the reported facts as one source snapshot. Retaining the old
	// vendor/family when an alias points to a different model would misbrand it.
	// User overrides remain in ModelConfig.Vendor and ModelFamily.
	metadata := ModelMetadata{Family: family, ParameterSize: parameterSize, Quantization: quantization, Source: snapshot.Provider}
	if current.Metadata != nil && *current.Metadata == metadata {
		return nil
	}

	previous := g.config
	next := *previous
	next.Models = append([]ModelConfig(nil), previous.Models...)
	next.Models[index].Metadata = &metadata
	g.config = &next
	if err := g.saveConfig(); err != nil {
		g.config = previous
		return &providers.EvaluationError{Code: "STORAGE_UNAVAILABLE", Message: "discovered model metadata could not be saved", Cause: err}
	}
	return nil
}
