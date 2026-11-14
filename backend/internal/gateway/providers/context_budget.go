package providers

import (
	"context"
	"errors"
	"net/http"
)

type ContextBudgetEvidence struct {
	Source               string `json:"source"`
	PromptTokens         int64  `json:"prompt_tokens"`
	OutputBudget         int64  `json:"output_budget"`
	ContextLimit         int64  `json:"context_limit"`
	RemainingTokens      int64  `json:"remaining_tokens"`
	AppliedTemplateHash  string `json:"applied_template_sha256"`
	TokenizationHash     string `json:"tokenization_sha256"`
	ReportedPromptTokens *int64 `json:"reported_prompt_tokens,omitempty"`
	Verification         string `json:"verification"`
}

// The running model's actual chat template and tokenizer are checked without
// inference, model loading, configuration changes or GPU reservation.
func (p *LocalCompatibleProvider) verifyLlamaContext(ctx context.Context, c *http.Client, origin string, req EvaluationRequest, body map[string]any, capacity int64) (*ContextBudgetEvidence, error) {
	e := &ContextBudgetEvidence{Source: "llamacpp_apply_template_tokenize", ContextLimit: capacity, Verification: "unverified"}
	if req.Options.NumCtx != nil && int64(*req.Options.NumCtx) < e.ContextLimit {
		e.ContextLimit = int64(*req.Options.NumCtx)
	}
	if req.Options.MaxOutputTokens == nil || *req.Options.MaxOutputTokens < 1 {
		return e, evaluationError("CONTEXT_UNVERIFIED", "an explicit output budget is required for context admission")
	}
	e.OutputBudget = int64(*req.Options.MaxOutputTokens)
	templateBody := map[string]any{"messages": body["messages"], "add_generation_prompt": true}
	for _, key := range []string{"tools", "tool_choice", "parallel_tool_calls", "chat_template_kwargs"} {
		if value, ok := body[key]; ok {
			templateBody[key] = value
		}
	}
	native := &OllamaProvider{p.OpenAIProvider}
	applied, _, err := native.nativeJSONPath(ctx, c, origin, "/apply-template", templateBody)
	if err != nil {
		if ctx.Err() != nil {
			return e, EvaluationContextError(ctx.Err())
		}
		var rejection *EvaluationError
		if len(req.Tools) > 0 && errors.As(err, &rejection) && (rejection.HTTPStatus == 400 || rejection.HTTPStatus == 422 || rejection.HTTPStatus == 501) {
			return e, &EvaluationError{Code: "UNSUPPORTED_CAPABILITY", Message: "native tool template request was rejected; tool protocol support is unconfirmed", HTTPStatus: rejection.HTTPStatus}
		}
		return e, &EvaluationError{Code: "CONTEXT_UNVERIFIED", Message: "the engine could not apply its actual chat template before inference", Cause: err}
	}
	prompt, ok := applied["prompt"].(string)
	if !ok || prompt == "" || len(prompt) > 512*1024 {
		return e, evaluationError("CONTEXT_UNVERIFIED", "applied template is missing or exceeds the admission bound")
	}
	tokenized, raw, err := native.nativeJSONPath(ctx, c, origin, "/tokenize", map[string]any{"content": prompt, "add_special": false, "parse_special": true})
	if err != nil {
		if ctx.Err() != nil {
			return e, EvaluationContextError(ctx.Err())
		}
		return e, &EvaluationError{Code: "CONTEXT_UNVERIFIED", Message: "the engine tokenizer did not verify the applied template", Cause: err}
	}
	tokens, ok := tokenized["tokens"].([]any)
	if !ok || len(tokens) < 1 || len(tokens) > 1048576 {
		return e, evaluationError("CONTEXT_UNVERIFIED", "invalid tokenization evidence")
	}
	for _, token := range tokens {
		if id, ok := intValue(token); !ok || id < 0 {
			return e, evaluationError("CONTEXT_UNVERIFIED", "invalid tokenizer token identifier")
		}
	}
	e.PromptTokens = int64(len(tokens))
	e.RemainingTokens = e.ContextLimit - e.PromptTokens - e.OutputBudget
	e.AppliedTemplateHash = hashValue(prompt)
	e.TokenizationHash = hashValue(raw)
	if e.RemainingTokens < 0 {
		e.Verification = "context_limit"
		return e, evaluationError("CONTEXT_LIMIT", "applied prompt plus requested output exceeds the actual context budget; inference was not dispatched")
	}
	e.Verification = "admitted_engine_prompt_count_unreported"
	return e, nil
}

func checkReportedPrompt(e *ContextBudgetEvidence, reported *int64) error {
	if e == nil {
		return nil
	}
	e.ReportedPromptTokens = reported
	if reported == nil {
		return nil
	}
	// Model APIs can count a few boundary/special tokens differently; a larger
	// loss must not be presented as a successful full-input run.
	if *reported+8 < e.PromptTokens {
		e.Verification = "input_truncated"
		return evaluationError("CONTEXT_TRUNCATED", "engine prompt count is shorter than the independently tokenized input; full-context scoring withheld")
	}
	if *reported+e.OutputBudget > e.ContextLimit {
		e.Verification = "context_limit"
		return evaluationError("CONTEXT_LIMIT", "engine prompt count plus output budget exceeds the admitted context")
	}
	e.Verification = "engine_prompt_count_verified"
	return nil
}
