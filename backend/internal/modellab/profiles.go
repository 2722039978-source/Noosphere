package modellab

import (
	"context"
	"github.com/noosphere/backend/internal/gateway/providers"
	"github.com/noosphere/backend/internal/modelstore"
	"time"
)

// Profiles describe requested options. The engine does not attest that every
// request option was effective; actual reported fields belong to each response.
func (s *Service) SaveProfile(ctx context.Context, p modelstore.InferenceProfile) (modelstore.InferenceProfile, error) {
	if p.Effective != nil || p.Reported != nil || len(p.Unsupported) > 0 {
		return p, identityError("INVALID_REQUEST", "new profiles cannot assert effective or reported engine options")
	}
	if _, _, err := evaluationOptions(p); err != nil {
		return p, err
	}
	p.ID = ""
	p.Confirmation = "requested_not_attested_by_engine"
	id, err := contentID("profile", p)
	if err != nil {
		return p, identityError("INVALID_REQUEST", "profile cannot be encoded as JSON")
	}
	p.ID = id
	if err := s.store.SaveInferenceProfile(ctx, p); err != nil {
		return p, err
	}
	return p, nil
}

func (s *Service) GetProfile(ctx context.Context, id string) (modelstore.InferenceProfile, error) {
	return s.store.GetInferenceProfile(ctx, id)
}

func evaluationOptions(p modelstore.InferenceProfile) (providers.EvaluationOptions, time.Duration, error) {
	o := p.Requested
	if o.Temperature == nil || o.Seed == nil || o.NumCtx == nil || o.MaxOutput == nil || *o.Seed < 0 {
		return providers.EvaluationOptions{}, 0, identityError("INVALID_REQUEST", "evaluation profiles require explicit temperature, nonnegative seed, context and output limits")
	}
	if (o.RetryLimit != nil && *o.RetryLimit != 0) || (o.Parallelism != nil && *o.Parallelism != 1) {
		return providers.EvaluationOptions{}, 0, identityError("UNSUPPORTED_CAPABILITY", "native evaluation has no hidden retry or configurable engine parallelism")
	}
	if *o.NumCtx <= 0 || *o.NumCtx > 1<<20 || *o.MaxOutput <= 0 || *o.MaxOutput > 65536 || *o.Temperature < 0 || *o.Temperature > 2 || (o.TopP != nil && (*o.TopP < 0 || *o.TopP > 1)) || (o.TopK != nil && (*o.TopK < 0 || *o.TopK > 1<<20)) {
		return providers.EvaluationOptions{}, 0, identityError("INVALID_REQUEST", "profile options are outside evaluation bounds")
	}
	var timeout time.Duration
	if o.DeadlineNS != nil {
		timeout = time.Duration(*o.DeadlineNS)
	}
	if _, err := providers.EvaluationTimeout(timeout); err != nil {
		return providers.EvaluationOptions{}, 0, err
	}
	contextSize, output := int(*o.NumCtx), int(*o.MaxOutput)
	var topK *int
	if o.TopK != nil {
		v := int(*o.TopK)
		topK = &v
	}
	options := providers.EvaluationOptions{Temperature: o.Temperature, Seed: o.Seed, TopP: o.TopP, TopK: topK, NumCtx: &contextSize, MaxOutputTokens: &output, Stop: o.Stop, Think: o.Think, KeepAlive: o.KeepAlive}
	if err := providers.ValidateEvaluationOptions(options); err != nil {
		return providers.EvaluationOptions{}, 0, err
	}
	return options, timeout, nil
}
