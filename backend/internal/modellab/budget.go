package modellab

import (
	"github.com/noosphere/backend/internal/modelstore"
	"time"
)

const LoadingProtocol = "warmup_then_measure_v1"

// Budget is trusted application configuration, never model-generated options.
type Budget struct {
	MaxContext       int64         `json:"max_context"`
	MaxOutput        int64         `json:"max_output"`
	InferenceTimeout time.Duration `json:"inference_timeout_ns"`
	CleanupTimeout   time.Duration `json:"cleanup_timeout_ns"`
}

func DefaultBudget() Budget {
	return Budget{MaxContext: 8192, MaxOutput: 2048, InferenceTimeout: 120 * time.Second, CleanupTimeout: 10 * time.Second}
}
func (b Budget) normalized() (Budget, error) {
	d := DefaultBudget()
	if b.MaxContext == 0 {
		b.MaxContext = d.MaxContext
	}
	if b.MaxOutput == 0 {
		b.MaxOutput = d.MaxOutput
	}
	if b.InferenceTimeout == 0 {
		b.InferenceTimeout = d.InferenceTimeout
	}
	if b.CleanupTimeout == 0 {
		b.CleanupTimeout = d.CleanupTimeout
	}
	if b.MaxContext < 1 || b.MaxContext > 8192 || b.MaxOutput < 1 || b.MaxOutput > 2048 || b.InferenceTimeout < time.Millisecond || b.InferenceTimeout > 120*time.Second || b.CleanupTimeout < time.Millisecond || b.CleanupTimeout > 30*time.Second {
		return b, identityError("BUDGET_EXCEEDED", "invalid local evaluation budget")
	}
	return b, nil
}
func (b Budget) validate(p modelstore.InferenceProfile) error {
	options, timeout, err := evaluationOptions(p)
	if err != nil {
		return err
	}
	if timeout == 0 {
		timeout = 2 * time.Minute
	}
	if int64(*options.NumCtx) > b.MaxContext || int64(*options.MaxOutputTokens) > b.MaxOutput || timeout > b.InferenceTimeout {
		return identityError("BUDGET_EXCEEDED", "profile exceeds configured context, output or inference budget")
	}
	if options.KeepAlive != "" && options.KeepAlive != "0" && options.KeepAlive != "0s" {
		return identityError("INVALID_REQUEST", "warmup_then_measure requires formal keep_alive zero")
	}
	return nil
}
