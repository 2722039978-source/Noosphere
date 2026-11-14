package modellab

import (
	"context"
	"encoding/json"

	"github.com/noosphere/backend/internal/gateway/providers"
)

func nativeToolMessages(e CaseEvidence) []providers.Message {
	system := "请使用提供的原生工具查询或换算，不要在文本中模拟函数调用。先调用且仅调用一个工具；收到工具反馈后，只输出任务要求的最终 JSON，不加解释。"
	if e.NativeTools.Language == "en" {
		system = "Use the provided native tools; do not imitate a tool call in text. Call exactly one tool, then use its result to return only the requested final JSON without explanation."
	}
	return []providers.Message{{Role: "system", Content: system}, {Role: "user", Content: e.Task}}
}
func buildNativeToolsContext(ctx context.Context, d *Dataset, ref CaseRef) (FrozenContext, error) {
	if err := ctx.Err(); err != nil {
		return FrozenContext{}, err
	}
	e, err := d.Evidence(ref)
	if err != nil {
		return FrozenContext{}, err
	}
	request := struct {
		Messages   []providers.Message        `json:"messages"`
		Tools      []providers.ToolDefinition `json:"tools"`
		ToolChoice string                     `json:"tool_choice"`
		MaxCalls   int                        `json:"max_calls"`
	}{nativeToolMessages(e), e.NativeTools.Tools, "required", 1}
	prompt, _ := json.Marshal(request)
	public := PublicContext{File: "cases/" + e.ID + "/tool-request.json", Language: e.NativeTools.Language, Lines: 2, SizeBytes: len(prompt), Parser: "not_applicable", HostFixtureHash: e.FixtureHash}
	raw, _ := json.Marshal(public)
	return FrozenContext{Public: public, ContextHash: digest(raw), Prompt: string(prompt), PromptHash: digest(prompt), PromptVersion: NativeToolsPromptVersion, PromptTemplateHash: d.revision.PromptTemplateHash, DatasetHash: d.revision.ContentHash, CaseHash: e.CaseHash, AnalysisResponse: json.RawMessage("null"), HealthResponse: json.RawMessage("null"), AnalysisResponseHash: digest([]byte("null")), HealthResponseHash: digest([]byte("null"))}, nil
}

func configureNativeToolRequest(req *providers.EvaluationRequest, in frozenInput) {
	if in.Case.NativeTools == nil {
		return
	}
	req.Messages = nativeToolMessages(in.Case)
	for _, m := range req.Messages {
		req.ToolMessages = append(req.ToolMessages, providers.ToolMessage{Role: m.Role, Content: m.Content})
	}
	req.Tools = jsonCopy(in.Case.NativeTools.Tools)
	req.ToolChoice = "required"
	req.ObserveStream = false
}
