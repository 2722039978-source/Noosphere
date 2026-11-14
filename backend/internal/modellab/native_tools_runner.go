package modellab

import (
	"context"
	"encoding/json"
	"github.com/noosphere/backend/internal/gateway/providers"
	"github.com/noosphere/backend/internal/modelstore"
	"time"
)

func nativeToolUnmeasured(out modelstore.AttemptOutcome, err error) (modelstore.AttemptOutcome, error) {
	out.State, out.Verdict, out.ErrorCode = "interrupted", "not_scored", errorCode(err)
	out.Result.InferenceCompleted = false
	out.Result.ErrorStage = "capability"
	out.Result.ErrorReason = "原生工具协议未证实，本项未测；不计模型正确性或基础设施失败。当前适配器或引擎未支持此次工具请求。"
	if errorCode(err) == "TOOL_PROTOCOL_UNOBSERVED" {
		out.Result.ErrorReason = "未观察到原生 tool_calls，无法区分模型未调用和引擎未支持；本项保持未测。文本中的函数调用 JSON 不替代真实协议。"
	}
	return out, nil
}
func nativeToolUnsupported(err error) bool {
	return errorCode(err) == "UNSUPPORTED_CAPABILITY" || errorCode(err) == "TOOL_PROTOCOL_UNOBSERVED"
}

// A single admission/session covers both bounded inference turns. Feedback is
// generated only for a valid, expected call to the finite pure-tool switch.
func (s *Service) nativeToolFeedback(ctx context.Context, model string, req providers.EvaluationRequest, e CaseEvidence, first *providers.EvaluationResponse, persist func(any, string) error) (*providers.EvaluationResponse, error) {
	if first == nil {
		return nil, nil
	}
	if err := persist(phase("native_tool_call", &req, first, nil, nil, time.Now()), ""); err != nil {
		return first, err
	}
	if len(first.ToolCalls) == 0 && !first.Truncated && first.DoneReason != "length" {
		return first, identityError("TOOL_PROTOCOL_UNOBSERVED", "No native tool call was observed; adapter/model protocol support is unconfirmed. Text imitations are not scored as tool execution.")
	}
	trace := NativeToolTranscript{FirstOutput: first.Output, Calls: first.ToolCalls}
	out := *first
	encode := func() { raw, _ := json.Marshal(trace); out.Output = string(raw) }
	if first.Truncated || first.DoneReason == "length" || len(first.ToolCalls) != 1 || !toolCallMatches(first.ToolCalls[0], e) {
		encode()
		return &out, nil
	}
	feedback, err := executeBenchmarkTool(first.ToolCalls[0])
	if err != nil {
		encode()
		return &out, nil
	}
	trace.Feedback = feedback
	call := first.ToolCalls[0]
	req.ToolMessages = append(req.ToolMessages, providers.ToolMessage{Role: "assistant", Content: first.Output, ToolCalls: first.ToolCalls}, providers.ToolMessage{Role: "tool", ToolCallID: call.ID, Content: string(feedback)})
	req.ToolChoice = "none"
	start := time.Now()
	if err := persist(phase("planned_tool_feedback", &req, nil, nil, nil, start), ""); err != nil {
		return &out, err
	}
	final, inferErr := s.runtime.infer(ctx, model, req)
	inferErr = sessionError(ctx, inferErr)
	if err := persist(phase("native_tool_feedback", &req, final, nil, inferErr, start), ""); err != nil {
		return &out, err
	}
	if final != nil {
		trace.FinalOutput, trace.FinalCalls = final.Output, final.ToolCalls
		out.Truncated = final.Truncated || final.DoneReason == "length"
		out.DoneReason = final.DoneReason
		out.WallDuration += final.WallDuration
		out.PromptEvalCount = sumNativeMetric(first.PromptEvalCount, final.PromptEvalCount)
		out.PromptEvalDurationNS = sumNativeMetric(first.PromptEvalDurationNS, final.PromptEvalDurationNS)
		out.EvalCount = sumNativeMetric(first.EvalCount, final.EvalCount)
		out.EvalDurationNS = sumNativeMetric(first.EvalDurationNS, final.EvalDurationNS)
		out.IdentityAfter = final.IdentityAfter
	}
	encode()
	if inferErr != nil {
		return &out, inferErr
	}
	if final == nil {
		return &out, identityError("INSUFFICIENT_EVIDENCE", "tool feedback terminal response missing")
	}
	return &out, nil
}
func sumNativeMetric(a, b *int64) *int64 {
	if a == nil || b == nil || *a < 0 || *b < 0 || *a > int64(^uint64(0)>>1)-*b {
		return nil
	}
	v := *a + *b
	return &v
}
