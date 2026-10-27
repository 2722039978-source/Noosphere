package providers

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"time"
)

// EvaluationOptions records requested values, not values attested by an engine.
// Pointers preserve the distinction between an omitted value and explicit zero.
type EvaluationOptions struct {
	Temperature     *float64 `json:"temperature,omitempty"`
	Seed            *int64   `json:"seed,omitempty"`
	TopP            *float64 `json:"top_p,omitempty"`
	TopK            *int     `json:"top_k,omitempty"`
	NumCtx          *int     `json:"num_ctx,omitempty"`
	MaxOutputTokens *int     `json:"max_output_tokens,omitempty"`
	Think           *bool    `json:"think,omitempty"`
	Stop            []string `json:"stop,omitempty"`
	KeepAlive       string   `json:"keep_alive,omitempty"`
}

type ModelIdentity struct {
	Name                string   `json:"name"`
	Digest              string   `json:"digest"`
	EngineVersion       string   `json:"engine_version"`
	MetadataHash        string   `json:"metadata_hash"`
	ConfigHash          string   `json:"config_hash"`
	TemplateHash        string   `json:"template_hash"`
	Format              string   `json:"format"`
	Family              string   `json:"family"`
	ParameterSize       string   `json:"parameter_size"`
	Quantization        string   `json:"quantization"`
	SizeBytes           int64    `json:"size_bytes"`
	Capabilities        []string `json:"capabilities"`
	LocalVerified       bool     `json:"local_verified"`
	IneligibilityReason string   `json:"ineligibility_reason,omitempty"`
	Limitations         []string `json:"limitations,omitempty"`
}

type ModelDiscovery struct {
	EngineVersion string          `json:"engine_version"`
	Models        []ModelIdentity `json:"models"`
}

type EvaluationRequest struct {
	ModelName                 string            `json:"model_name"`
	ExpectedDigest            string            `json:"expected_digest"`
	ExpectedMetadataHash      string            `json:"expected_metadata_hash"`
	ExpectedEngineVersion     string            `json:"expected_engine_version"`
	ExpectedSourceFingerprint string            `json:"expected_source_fingerprint,omitempty"`
	AttemptID                 string            `json:"attempt_id"`
	Kind                      string            `json:"kind"`
	Messages                  []Message         `json:"messages"`
	Options                   EvaluationOptions `json:"options"`
	Timeout                   time.Duration     `json:"timeout"`
}

type EvaluationResponse struct {
	Output               string            `json:"output"`
	Thinking             string            `json:"thinking"`
	DoneReason           string            `json:"done_reason"`
	Truncated            bool              `json:"truncated"`
	IdentityBefore       ModelIdentity     `json:"identity_before"`
	IdentityAfter        ModelIdentity     `json:"identity_after"`
	RequestedOptions     EvaluationOptions `json:"requested_options"`
	RawResponse          json.RawMessage   `json:"raw_response"`
	WallDuration         time.Duration     `json:"wall_duration"`
	TotalDurationNS      *int64            `json:"total_duration_ns"`
	LoadDurationNS       *int64            `json:"load_duration_ns"`
	PromptEvalCount      *int64            `json:"prompt_eval_count"`
	PromptEvalDurationNS *int64            `json:"prompt_eval_duration_ns"`
	EvalCount            *int64            `json:"eval_count"`
	EvalDurationNS       *int64            `json:"eval_duration_ns"`
	Limitations          []string          `json:"limitations,omitempty"`
}

// EvaluationProvider is optional; existing chat providers need no fake methods.
type EvaluationProvider interface {
	Discover(context.Context) (ModelDiscovery, error)
	Evaluate(context.Context, EvaluationRequest) (*EvaluationResponse, error)
}

// ResidentModel reports daemon residency, not physical GPU peak or free memory.
// Nil size fields distinguish unreported allocation from an explicit zero.
type ResidentModel struct {
	Name          string `json:"name"`
	Digest        string `json:"digest"`
	SizeBytes     *int64 `json:"size_bytes"`
	SizeVRAMBytes *int64 `json:"size_vram_bytes"`
}

type ModelResidencySnapshot struct {
	Models         []ResidentModel `json:"models"`
	TargetResident bool            `json:"target_resident"`
	RawResponse    json.RawMessage `json:"raw_response"`
	ObservedAt     time.Time       `json:"observed_at"`
}

// EvaluationResidencyProvider is optional. Direct callers must own the model
// before release; the gateway separately enforces session ownership.
type EvaluationResidencyProvider interface {
	Residency(context.Context, EvaluationRequest) (ModelResidencySnapshot, error)
	ReleaseModel(context.Context, EvaluationRequest) (ModelResidencySnapshot, error)
}

type EvaluationError struct {
	Code       string `json:"code"`
	Message    string `json:"message"`
	HTTPStatus int    `json:"http_status,omitempty"`
	Cause      error  `json:"-"`
}

func (e *EvaluationError) Error() string { return e.Code + ": " + e.Message }
func (e *EvaluationError) Unwrap() error { return e.Cause }

// EvaluationContextError keeps caller cancellation distinct from engine failure.
func EvaluationContextError(err error) error {
	if errors.Is(err, context.Canceled) {
		return &EvaluationError{Code: "USER_CANCELLED", Message: "evaluation cancelled by caller", Cause: err}
	}
	var networkError net.Error
	if errors.Is(err, context.DeadlineExceeded) || (errors.As(err, &networkError) && networkError.Timeout()) {
		return &EvaluationError{Code: "INFERENCE_TIMEOUT", Message: "evaluation deadline exceeded", Cause: err}
	}
	return &EvaluationError{Code: "PROVIDER_UNAVAILABLE", Message: "evaluation provider unavailable", Cause: err}
}

// EvaluationTimeout applies to metadata, semaphore queueing and inference together.
// Zero selects two minutes; requests cannot exceed ten minutes.
func EvaluationTimeout(timeout time.Duration) (time.Duration, error) {
	if timeout < 0 || timeout > 10*time.Minute {
		return 0, &EvaluationError{Code: "INVALID_REQUEST", Message: "timeout must be between zero and ten minutes"}
	}
	if timeout == 0 {
		return 2 * time.Minute, nil
	}
	return timeout, nil
}
