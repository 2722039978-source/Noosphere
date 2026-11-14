package modellab

import (
	_ "embed"
	"encoding/json"
	"fmt"

	"github.com/noosphere/backend/internal/gateway/providers"
	"github.com/noosphere/backend/internal/modelstore"
)

const NativeToolsID = "reviewer-tools-v1"
const NativeToolsWorkflowID = "local.tools.v1"
const NativeToolsScorerVersion = "host-native-tools-v1.0.0"
const NativeToolsPromptVersion = "native-tools-prompt-v1.0.0"

//go:embed benchmarks/reviewer-tools-v1/manifest.json
var nativeToolsManifestSource []byte

//go:embed native_tools_scorer.go
var nativeToolsScorerSource []byte

//go:embed native_tools_context.go
var nativeToolsContextSource []byte

type NativeToolCase struct {
	Language     string                     `json:"language"`
	Tools        []providers.ToolDefinition `json:"tools"`
	ExpectedCall providers.ToolFunction     `json:"expected_call"`
}

func (d *Dataset) isTools() bool   { return d != nil && d.revision.BenchmarkID == NativeToolsID }
func nativeToolsID(id string) bool { return id == NativeToolsID || id == NativeToolsID+".1.0.0" }
func nativeToolsScorerHash() string {
	return digest(append(append([]byte(nil), nativeToolsScorerSource...), textScorerSource...))
}
func loadNativeToolsDataset() (*Dataset, error) {
	var m struct {
		SchemaVersion                       int `json:"schema_version"`
		ID, Version, Title, Source, License string
		ScorerHash                          string                     `json:"scorer_sha256"`
		PromptHash                          string                     `json:"prompt_sha256"`
		Tools                               []providers.ToolDefinition `json:"tools"`
		Cases                               []struct {
			ID, Language, Task string
			Call               providers.ToolFunction `json:"call"`
			ExpectedOutput     json.RawMessage        `json:"expected_output"`
		} `json:"cases"`
	}
	if err := strictJSON(nativeToolsManifestSource, &m); err != nil {
		return nil, err
	}
	if m.SchemaVersion != 1 || m.ID != NativeToolsID || m.Version != "1.0.0" || m.License != "CC0-1.0" || m.Source == "" || m.ScorerHash != nativeToolsScorerHash() || m.PromptHash != digest(nativeToolsContextSource) || len(m.Cases) != 2 || len(m.Tools) != 2 {
		return nil, fmt.Errorf("native tool source contract mismatch")
	}
	d := &Dataset{scorerHash: m.ScorerHash}
	d.revision = modelstore.DatasetRevision{ID: m.ID + "." + m.Version, BenchmarkID: m.ID, Version: m.Version, ContentHash: digest(nativeToolsManifestSource), SourceRevisionID: m.ID + "." + m.Version, SourceContentHash: digest(nativeToolsManifestSource), ScorerVersion: NativeToolsScorerVersion, ScorerHash: m.ScorerHash, RunnerVersion: "host-native-tools-v1", PromptTemplateHash: m.PromptHash, License: m.License, Source: m.Source}
	seen := map[string]bool{}
	for _, c := range m.Cases {
		if !caseIDPattern.MatchString(c.ID) || seen[c.ID] || c.Task == "" || len(c.Task) > 4000 || c.Language != "zh" && c.Language != "en" {
			return nil, fmt.Errorf("invalid native tool case")
		}
		seen[c.ID] = true
		if _, err := parseTextJSON(c.ExpectedOutput); err != nil {
			return nil, err
		}
		if _, err := executeBenchmarkTool(providers.ToolCall{Function: c.Call}); err != nil {
			return nil, err
		}
		e := CaseEvidence{ID: c.ID, Version: m.Version, Category: "native_tool_call", Task: c.Task, NativeTools: &NativeToolCase{Language: c.Language, Tools: jsonCopy(m.Tools), ExpectedCall: c.Call}, ExpectedOutput: c.ExpectedOutput, ReferenceHash: digest(c.ExpectedOutput), Rules: []TextRule{{ID: "native_call", Kind: "native_protocol", Description: "必须返回恰好一个真实原生函数调用。"}, {ID: "function_arguments", Kind: "exact_json", Description: "函数名与参数必须匹配题意；重复键或额外参数无效。"}, {ID: "deterministic_feedback", Kind: "exact_json", Description: "工具反馈由固定无副作用实现产生，不能由模型补造。"}, {ID: "final_answer", Kind: "exact_json", Description: "接收工具反馈后给出完整正确的最终 JSON；不允许额外调用或尾随说明。"}}}
		fixture, _ := json.Marshal(struct {
			Task        string          `json:"task"`
			NativeTools *NativeToolCase `json:"native_tools"`
		}{e.Task, e.NativeTools})
		e.Faulty, e.FixtureHash = string(fixture), digest(fixture)
		raw, _ := json.Marshal(e)
		e.CaseHash = digest(raw)
		d.cases = append(d.cases, e)
		d.revision.Cases = append(d.revision.Cases, textRevisionCase(e))
	}
	return d, nil
}

func builtinNativeToolsDetail() (DatasetDetail, error) {
	d, err := loadNativeToolsDataset()
	if err != nil {
		return DatasetDetail{}, err
	}
	doc := DatasetDocument{SchemaVersion: 1, BenchmarkID: NativeToolsID, Version: "1.0.0", Title: "原生工具协议 · 两项安全工具", Description: "固定库存查询与长度换算；真实 tool_calls、确定性无副作用反馈与最终答案。", Source: d.revision.Source, License: d.revision.License, ScoringRule: "deterministic_tools_v1", Cases: []DatasetCaseDocument{}}
	for _, e := range d.cases {
		doc.Cases = append(doc.Cases, DatasetCaseDocument{ID: e.ID, Category: e.Category, Task: e.Task, Rules: jsonCopy(e.Rules), ExpectedOutput: append(json.RawMessage(nil), e.ExpectedOutput...), NativeTools: jsonCopy(e.NativeTools)})
	}
	detail := datasetDetail(d, doc, modelstore.DatasetDefinition{})
	detail.Revision.Kind = "builtin"
	detail.Revision.WorkflowID = NativeToolsWorkflowID
	detail.Revision.ScoringRule = "deterministic_tools_v1"
	detail.Revision.Suite = &SuiteMetadata{TaskType: "native_tool_call", Categories: []string{"native_tool_call"}, CaseCount: 2, MinimumContext: 2048, RecommendedMaxOutput: 256, MaxOutput: 512, Coverage: []CapabilityCoverage{{"native_tool_call", "deterministic", "真实原生工具调用和固定无副作用反馈；协议未证实则保持未测。"}, {"open_dialogue_quality", "manual", "开放式对话质量需人工评阅。"}, {"general_safety", "unmeasured", "不包含高风险工具或通用安全测试。"}}, Limitations: []string{"只有两项原创 CC0 任务，不代表通用 Agent 或工具安全能力。", "仅支持可信本机 llama.cpp 兼容测评通道；拒绝工具请求或未观察到原生调用时记未测。", "每题最多一个工具调用与一次反馈后的回答；无网络、文件、账户或外部副作用。"}}
	return detail, nil
}
