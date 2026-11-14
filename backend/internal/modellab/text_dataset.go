package modellab

import (
	_ "embed"
	"encoding/json"
	"fmt"

	"github.com/noosphere/backend/internal/modelstore"
)

const TextDatasetID = "local-text-v1"
const TextWorkflowID = "local.text.v1"
const TextScorerVersion = "host-text-v1.0.0"
const TextPromptVersion = "local-text-prompt-v1.0.0"
const textRunnerVersion = "host-declarative-text-v1"
const textScoringRule = "deterministic_text_v1"

//go:embed benchmarks/local-text-v1/manifest.json
var textDatasetSource []byte

//go:embed text_scorer.go
var textScorerSource []byte

// The complete builder, including the template, is part of the prompt identity.
//
//go:embed text_context.go
var textContextSource []byte

type TextRule struct {
	ID          string `json:"id"`
	Kind        string `json:"kind"`
	Description string `json:"description"`
}

type SuiteMetadata struct {
	MinimumContext       int64                `json:"minimum_context,omitempty"`
	RecommendedMaxOutput int64                `json:"recommended_max_output,omitempty"`
	MaxOutput            int64                `json:"max_output,omitempty"`
	Coverage             []CapabilityCoverage `json:"capability_coverage,omitempty"`
	TaskType             string               `json:"task_type"`
	Categories           []string             `json:"categories"`
	CaseCount            int                  `json:"case_count"`
	Limitations          []string             `json:"limitations"`
}

type textDatasetManifest struct {
	SchemaVersion int    `json:"schema_version"`
	ID            string `json:"id"`
	Version       string `json:"version"`
	Title         string `json:"title"`
	Description   string `json:"description"`
	Source        string `json:"source"`
	License       string `json:"license"`
	WorkflowID    string `json:"workflow_id"`
	ScorerVersion string `json:"scorer_version"`
	ScorerHash    string `json:"scorer_sha256"`
	PromptVersion string `json:"prompt_version"`
	PromptHash    string `json:"prompt_sha256"`
	Cases         []struct {
		ID             string          `json:"id"`
		Category       string          `json:"category"`
		Task           string          `json:"task"`
		Rules          []TextRule      `json:"rules"`
		ExpectedOutput json.RawMessage `json:"expected_output"`
	} `json:"cases"`
}

func (d *Dataset) isText() bool { return d != nil && d.revision.BenchmarkID == TextDatasetID }
func (d *Dataset) workflowID() string {
	if d.isTools() {
		return NativeToolsWorkflowID
	}
	if d.isReviewer() {
		return ReviewerWorkflowID
	}
	if d.isText() {
		return TextWorkflowID
	}
	return DatasetWorkflowID
}

func loadTextDataset() (*Dataset, error) { return loadTextDatasetBytes(textDatasetSource) }
func loadTextDatasetBytes(raw []byte) (*Dataset, error) {
	var m textDatasetManifest
	if err := strictJSON(raw, &m); err != nil {
		return nil, err
	}
	if m.SchemaVersion != 1 || m.ID != TextDatasetID || m.Version != "1.0.0" || m.Title == "" || m.Source == "" || m.License != "CC0-1.0" || m.WorkflowID != TextWorkflowID || m.ScorerVersion != TextScorerVersion || m.ScorerHash != digest(textScorerSource) || m.PromptVersion != TextPromptVersion || m.PromptHash != digest(textContextSource) || len(m.Cases) != 16 {
		return nil, fmt.Errorf("text dataset manifest/version/contract hash mismatch")
	}
	d := &Dataset{scorerHash: m.ScorerHash}
	d.revision.ID, d.revision.BenchmarkID, d.revision.Version = m.ID+"."+m.Version, m.ID, m.Version
	d.revision.ContentHash, d.revision.ScorerVersion, d.revision.ScorerHash = digest(raw), m.ScorerVersion, m.ScorerHash
	d.revision.SourceRevisionID, d.revision.SourceContentHash = d.revision.ID, d.revision.ContentHash
	d.revision.RunnerVersion, d.revision.PromptTemplateHash = textRunnerVersion, m.PromptHash
	d.revision.License, d.revision.Source = m.License, m.Source
	seen, categories := map[string]bool{}, map[string]int{}
	for _, c := range m.Cases {
		if !caseIDPattern.MatchString(c.ID) || seen[c.ID] || c.Task == "" || len(c.Rules) != 2 {
			return nil, fmt.Errorf("incomplete/duplicate text case: %s", c.ID)
		}
		seen[c.ID] = true
		format, content := "text_format", "exact_text"
		switch c.Category {
		case "chinese_instruction":
			var value string
			if err := strictJSON(c.ExpectedOutput, &value); err != nil || value == "" {
				return nil, fmt.Errorf("invalid expected text: %s", c.ID)
			}
		case "structured_json":
			format, content = "strict_json", "exact_json"
			if _, err := parseTextJSON(c.ExpectedOutput); err != nil {
				return nil, fmt.Errorf("invalid expected JSON: %s: %w", c.ID, err)
			}
		default:
			return nil, fmt.Errorf("unknown text category")
		}
		if c.Rules[0].ID != "format" || c.Rules[0].Kind != format || c.Rules[1].ID != "content" || c.Rules[1].Kind != content || c.Rules[0].Description == "" || c.Rules[1].Description == "" {
			return nil, fmt.Errorf("unknown text rule contract: %s", c.ID)
		}
		categories[c.Category]++
		e := CaseEvidence{ID: c.ID, Version: m.Version, Category: c.Category, Task: c.Task, Rules: c.Rules, ExpectedOutput: c.ExpectedOutput, FixtureHash: digest([]byte(c.Task)), ReferenceHash: digest(c.ExpectedOutput), Faulty: c.Task}
		body, _ := json.Marshal(e)
		e.CaseHash = digest(body)
		d.cases = append(d.cases, e)
		d.revision.Cases = append(d.revision.Cases, textRevisionCase(e))
	}
	if categories["chinese_instruction"] != 8 || categories["structured_json"] != 8 {
		return nil, fmt.Errorf("text category balance mismatch")
	}
	return d, nil
}

func textSuiteMetadata() *SuiteMetadata {
	return &SuiteMetadata{TaskType: "local_text", Categories: []string{"chinese_instruction", "structured_json"}, CaseCount: 16, Limitations: []string{
		"原创公开小样本，仅测量本套件中的中文约束指令和 JSON 结构及内容正确性；不构成通用模型排名。",
		"不评测开放式对话质量、事实知识、工具调用或安全能力；规则评分不调用模型裁判，也不执行模型输出。",
		"中文答案仅规范 CRLF 换行和首尾 ASCII 空白；JSON 对象键顺序和等值数字写法不影响结果，数组顺序、字段类型、额外字段及重复键均参与判定。",
	}}
}

func caseExpected(e CaseEvidence) string {
	var raw []byte
	if len(e.Rules) > 0 {
		raw, _ = json.Marshal(struct {
			Task           string          `json:"task"`
			Rules          []TextRule      `json:"rules"`
			ExpectedOutput json.RawMessage `json:"expected_output"`
			Reviewer       *ReviewerCase   `json:"reviewer,omitempty"`
			NativeTools    *NativeToolCase `json:"native_tools,omitempty"`
		}{e.Task, e.Rules, e.ExpectedOutput, e.Reviewer, e.NativeTools})
	} else {
		raw, _ = json.Marshal(e.Assertions)
	}
	return string(raw)
}

func textRevisionCase(e CaseEvidence) modelstore.Case {
	return modelstore.Case{ID: e.ID, Category: e.Category, FixtureHash: e.FixtureHash, Expected: caseExpected(e)}
}
