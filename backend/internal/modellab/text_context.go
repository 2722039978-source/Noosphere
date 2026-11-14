package modellab

import (
	"context"
	"encoding/json"
	"strings"
)

const textPromptTemplate = "请完成下面的中文任务。只输出题目要求的最终答案，不要输出推理过程、Markdown 代码围栏、解释或额外字段。\n"

func buildTextPrompt(e CaseEvidence) string {
	var out strings.Builder
	out.WriteString(textPromptTemplate)
	out.WriteString("任务 ID：" + e.ID + "\n任务：" + e.Task + "\n判定规则：\n")
	for _, rule := range e.Rules {
		out.WriteString("- " + rule.Description + "\n")
	}
	return out.String()
}

func buildTextContext(ctx context.Context, d *Dataset, ref CaseRef) (FrozenContext, error) {
	if err := ctx.Err(); err != nil {
		return FrozenContext{}, err
	}
	e, err := d.Evidence(ref)
	if err != nil {
		return FrozenContext{}, err
	}
	public := PublicContext{File: "cases/" + e.ID + "/prompt.txt", Language: "text", Lines: strings.Count(e.Task, "\n") + 1, SizeBytes: len(e.Task), Parser: "not_applicable", HostFixtureHash: e.FixtureHash}
	raw, _ := json.Marshal(public)
	prompt := buildTextPrompt(e)
	// Explicit nulls retain the frozen-input wire shape without inventing
	// CodeLens observations for a task that does not use that service.
	return FrozenContext{Public: public, ContextHash: digest(raw), Prompt: prompt, PromptHash: digest([]byte(prompt)), PromptVersion: TextPromptVersion, PromptTemplateHash: d.revision.PromptTemplateHash, DatasetHash: d.revision.ContentHash, CaseHash: e.CaseHash, AnalysisResponse: json.RawMessage("null"), HealthResponse: json.RawMessage("null"), AnalysisResponseHash: digest([]byte("null")), HealthResponseHash: digest([]byte("null"))}, nil
}
