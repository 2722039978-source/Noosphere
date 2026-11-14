package modellab

import (
	"embed"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/noosphere/backend/internal/gateway/providers"
	"github.com/noosphere/backend/internal/modelstore"
)

const ReviewerWorkflowID = "local.review.v1"
const ReviewerScorerVersion = "host-reviewer-v1.0.0"
const ReviewerPromptVersion = "local-reviewer-prompt-v1.0.0"

var reviewerIDs = []string{"reviewer-core-v1", "reviewer-context-4k-v1", "reviewer-context-8k-v1"}

//go:embed benchmarks/reviewer-*
var reviewerFiles embed.FS

//go:embed reviewer_scorer.go
var reviewerScorerSource []byte

//go:embed reviewer_context.go
var reviewerContextSource []byte

type CapabilityCoverage struct {
	Category string `json:"category"`
	Mode     string `json:"mode"`
	Reason   string `json:"reason"`
}
type ReviewerFile struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}
type ReviewerRetrieval struct {
	Rows   int `json:"rows"`
	Target int `json:"target"`
}
type ReviewerCase struct {
	Method    string              `json:"method"`
	Language  string              `json:"language"`
	PairID    string              `json:"pair_id,omitempty"`
	Messages  []providers.Message `json:"messages,omitempty"`
	Files     []ReviewerFile      `json:"files,omitempty"`
	Retrieval *ReviewerRetrieval  `json:"retrieval,omitempty"`
}
type reviewerManifest struct {
	SchemaVersion        int    `json:"schema_version"`
	ID                   string `json:"id"`
	Version              string `json:"version"`
	Title                string `json:"title"`
	Description          string `json:"description"`
	Source               string `json:"source"`
	License              string `json:"license"`
	MinimumContext       int64  `json:"minimum_context"`
	RecommendedMaxOutput int64  `json:"recommended_max_output"`
	ScorerVersion        string `json:"scorer_version"`
	ScorerHash           string `json:"scorer_sha256"`
	PromptVersion        string `json:"prompt_version"`
	PromptHash           string `json:"prompt_sha256"`
	Cases                []struct {
		ID             string          `json:"id"`
		Category       string          `json:"category"`
		Task           string          `json:"task"`
		Reviewer       ReviewerCase    `json:"reviewer"`
		ExpectedOutput json.RawMessage `json:"expected_output"`
	} `json:"cases"`
}

func reviewerID(id string) string {
	for _, known := range reviewerIDs {
		if id == known || id == known+".1.0.0" {
			return known
		}
	}
	return ""
}
func builtinIDs() []string {
	return append([]string{DatasetID, TextDatasetID, NativeToolsID}, reviewerIDs...)
}
func (d *Dataset) isReviewer() bool    { return d != nil && reviewerID(d.revision.BenchmarkID) != "" }
func (d *Dataset) isDeclarative() bool { return d.isText() || d.isReviewer() || d.isTools() }
func supportedWorkflow(id string) bool {
	return id == DatasetWorkflowID || id == TextWorkflowID || id == ReviewerWorkflowID || id == NativeToolsWorkflowID
}
func reviewerScorerHash() string {
	return digest(append(append([]byte(nil), reviewerScorerSource...), textScorerSource...))
}

func loadReviewerDataset(id string) (*Dataset, error) {
	raw, err := reviewerFiles.ReadFile("benchmarks/" + reviewerID(id) + "/manifest.json")
	if err != nil {
		return nil, err
	}
	var m reviewerManifest
	if err := strictJSON(raw, &m); err != nil {
		return nil, err
	}
	if reviewerID(m.ID) != m.ID || m.Version != "1.0.0" || m.SchemaVersion != 1 || m.License != "CC0-1.0" || m.Source == "" || m.Title == "" || m.ScorerVersion != ReviewerScorerVersion || m.ScorerHash != reviewerScorerHash() || m.PromptVersion != ReviewerPromptVersion || m.PromptHash != digest(reviewerContextSource) {
		return nil, fmt.Errorf("reviewer manifest/source contract mismatch")
	}
	count, minimum := 14, int64(2048)
	if strings.Contains(m.ID, "4k") {
		count, minimum = 3, 4096
	}
	if strings.Contains(m.ID, "8k") {
		count, minimum = 3, 8192
	}
	if len(m.Cases) != count || m.MinimumContext != minimum || m.RecommendedMaxOutput != 256 {
		return nil, fmt.Errorf("reviewer workload/context mismatch")
	}
	d := &Dataset{scorerHash: m.ScorerHash, reviewer: &m}
	d.revision = modelstore.DatasetRevision{ID: m.ID + "." + m.Version, BenchmarkID: m.ID, Version: m.Version, ContentHash: digest(raw), SourceRevisionID: m.ID + "." + m.Version, SourceContentHash: digest(raw), ScorerVersion: m.ScorerVersion, ScorerHash: m.ScorerHash, RunnerVersion: "host-reviewer-declarative-v1", PromptTemplateHash: m.PromptHash, License: m.License, Source: m.Source}
	seen := map[string]bool{}
	for _, c := range m.Cases {
		if !caseIDPattern.MatchString(c.ID) || seen[c.ID] || c.Task == "" || len(c.Task) > 12000 || c.Reviewer.Language != "zh" && c.Reviewer.Language != "en" {
			return nil, fmt.Errorf("invalid reviewer case %s", c.ID)
		}
		seen[c.ID] = true
		switch c.Category {
		case "bilingual_zh", "bilingual_en", "constrained_dialogue", "repository_qa", "context_retrieval_2k", "context_retrieval_4k", "context_retrieval_8k", "benign_boundary", "function_selection_json":
		default:
			return nil, fmt.Errorf("unknown reviewer category")
		}
		if c.Reviewer.Method != "exact_text" && c.Reviewer.Method != "exact_json" {
			return nil, fmt.Errorf("unknown reviewer scoring method")
		}
		if c.Reviewer.Method == "exact_text" {
			var v string
			if strictJSON(c.ExpectedOutput, &v) != nil || v == "" {
				return nil, fmt.Errorf("invalid expected text")
			}
		} else if _, err := parseTextJSON(c.ExpectedOutput); err != nil {
			return nil, err
		}
		if len(c.Reviewer.Messages) > 8 || len(c.Reviewer.Files) > 8 {
			return nil, fmt.Errorf("reviewer context count bound")
		}
		for _, message := range c.Reviewer.Messages {
			if message.Role != "user" && message.Role != "assistant" || len(message.Content) > 4000 {
				return nil, fmt.Errorf("invalid conversation history")
			}
		}
		for _, file := range c.Reviewer.Files {
			if file.Path == "" || strings.Contains(file.Path, "..") || len(file.Content) > 8000 {
				return nil, fmt.Errorf("invalid fixed repository fixture")
			}
		}
		if r := c.Reviewer.Retrieval; r != nil && (r.Rows < 1 || r.Rows > 700 || r.Target < 1 || r.Target > r.Rows) {
			return nil, fmt.Errorf("invalid retrieval fixture")
		}
		rules := []TextRule{{ID: "format", Kind: c.Reviewer.Method, Description: "仅输出要求的完整最终答案；JSON 不允许重复键、代码围栏、尾随说明或额外值。"}, {ID: "content", Kind: c.Reviewer.Method, Description: "按冻结资料与对话核对内容、字段类型和顺序。无模型裁判，不执行输出。"}}
		e := CaseEvidence{ID: c.ID, Version: m.Version, Category: c.Category, Task: c.Task, Reviewer: &c.Reviewer, Rules: rules, ExpectedOutput: c.ExpectedOutput, ReferenceHash: digest(c.ExpectedOutput)}
		fixture, _ := json.Marshal(struct {
			Task     string        `json:"task"`
			Reviewer *ReviewerCase `json:"reviewer"`
		}{e.Task, e.Reviewer})
		e.Faulty = string(fixture)
		e.FixtureHash = digest(fixture)
		body, _ := json.Marshal(e)
		e.CaseHash = digest(body)
		d.cases = append(d.cases, e)
		d.revision.Cases = append(d.revision.Cases, textRevisionCase(e))
	}
	return d, nil
}
func reviewerMetadata(d *Dataset) *SuiteMetadata {
	categories := []string{}
	seen := map[string]bool{}
	coverage := []CapabilityCoverage{}
	for _, c := range d.cases {
		if seen[c.Category] {
			continue
		}
		seen[c.Category] = true
		categories = append(categories, c.Category)
		mode, reason := "deterministic", "仅按本套件固定答案与公开规则评分。"
		if c.Category == "function_selection_json" {
			mode, reason = "proxy", "仅测试 JSON 函数选择与参数；未发送原生工具协议，也不证明 Agent 工具执行能力。"
		}
		if c.Category == "benign_boundary" {
			reason = "仅测无害请求的过度拒答与明确授权边界，不是通用安全评估。"
		}
		coverage = append(coverage, CapabilityCoverage{c.Category, mode, reason})
	}
	coverage = append(coverage, CapabilityCoverage{"native_tool_call", "unmeasured", "本套件未运行原生 tool-call 协议；不能用 JSON 选择代理结果替代。"}, CapabilityCoverage{"open_dialogue_quality", "manual", "开放式对话质量需要人工评阅，本套件没有自动分。"}, CapabilityCoverage{"general_safety", "unmeasured", "未测试高风险安全能力；只包含良性边界任务。"})
	return &SuiteMetadata{TaskType: "local_reviewer", Categories: categories, CaseCount: len(d.cases), MinimumContext: d.reviewer.MinimumContext, RecommendedMaxOutput: d.reviewer.RecommendedMaxOutput, MaxOutput: 512, Coverage: coverage, Limitations: []string{"原创公开 CC0 小样本；中英翻译对与检索位置相关，不形成通用排名。", "上下文档位表示最低配置容量；实际分词数以引擎原始证据为准，输入字节和全文可核对。", "函数选择为 JSON 代理；原生工具执行、开放式对话质量与广义安全保持未测或人工判断。"}}
}
func validateReviewerProfile(d *Dataset, p modelstore.InferenceProfile) error {
	if !d.isReviewer() && !d.isTools() {
		return nil
	}
	minimum := int64(2048)
	if d.isReviewer() {
		minimum = d.reviewer.MinimumContext
	}
	if p.Requested.NumCtx == nil || *p.Requested.NumCtx < minimum {
		return identityError("BUDGET_EXCEEDED", fmt.Sprintf("this suite requires context >= %d", minimum))
	}
	if p.Requested.MaxOutput == nil || *p.Requested.MaxOutput > 512 {
		return identityError("BUDGET_EXCEEDED", "reviewer workloads require max_output <= 512")
	}
	return nil
}

func builtinReviewerDetail(id string) (DatasetDetail, error) {
	d, err := LoadDataset(reviewerID(id))
	if err != nil {
		return DatasetDetail{}, err
	}
	m := d.reviewer
	doc := DatasetDocument{SchemaVersion: 1, BenchmarkID: m.ID, Version: m.Version, Title: m.Title, Description: m.Description, Source: m.Source, License: m.License, ScoringRule: "deterministic_reviewer_v1", Cases: []DatasetCaseDocument{}}
	for _, e := range d.cases {
		doc.Cases = append(doc.Cases, DatasetCaseDocument{ID: e.ID, Category: e.Category, Task: e.Task, Rules: jsonCopy(e.Rules), ExpectedOutput: append(json.RawMessage(nil), e.ExpectedOutput...), Reviewer: jsonCopy(e.Reviewer)})
	}
	detail := datasetDetail(d, doc, modelstore.DatasetDefinition{})
	detail.Revision.Kind = "builtin"
	detail.Revision.WorkflowID = ReviewerWorkflowID
	detail.Revision.ScoringRule = "deterministic_reviewer_v1"
	detail.Revision.Suite = reviewerMetadata(d)
	return detail, nil
}
