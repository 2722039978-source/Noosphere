package modellab

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

const maxTextOutputBytes = 64 << 10

type TextRuleResult struct {
	TextRule
	Passed bool   `json:"passed"`
	Reason string `json:"reason,omitempty"`
}

// TextScoreResult has no compiler or static-code fields. The assertion counters
// are shared by readers, but each assertion is a declared text rule.
type TextScoreResult struct {
	TaskType         string           `json:"task_type"`
	CaseID           string           `json:"case_id"`
	CaseVersion      string           `json:"case_version"`
	CaseHash         string           `json:"case_hash"`
	FixtureHash      string           `json:"fixture_hash"`
	DatasetID        string           `json:"dataset_id"`
	DatasetVersion   string           `json:"dataset_version"`
	DatasetHash      string           `json:"dataset_hash"`
	ScorerVersion    string           `json:"scorer_version"`
	ScorerHash       string           `json:"scorer_hash"`
	RunnerVersion    string           `json:"runner_version"`
	RawOutputHash    string           `json:"raw_output_hash"`
	Passed           bool             `json:"passed"`
	PassedAssertions int              `json:"passed_assertions"`
	TotalAssertions  int              `json:"total_assertions"`
	Rules            []TextRuleResult `json:"rules"`
	Status           string           `json:"status"`
	ErrorStage       string           `json:"error_stage,omitempty"`
	ErrorCode        string           `json:"error_code,omitempty"`
	Reason           string           `json:"reason,omitempty"`
	TotalDuration    time.Duration    `json:"total_duration_ns"`
}

func scoreText(ctx context.Context, ref CaseRef, output string) (result TextScoreResult, err error) {
	start := time.Now()
	defer func() { result.TotalDuration = time.Since(start) }()
	d, e, err := authoritative(ref)
	if err != nil {
		return result, err
	}
	if !d.isText() {
		return result, fmt.Errorf("text scorer requires trusted text case")
	}
	result = TextScoreResult{TaskType: "local_text", CaseID: e.ID, CaseVersion: e.Version, CaseHash: e.CaseHash, FixtureHash: e.FixtureHash, DatasetID: d.revision.BenchmarkID, DatasetVersion: d.revision.Version, DatasetHash: d.revision.ContentHash, ScorerVersion: d.revision.ScorerVersion, ScorerHash: d.scorerHash, RunnerVersion: d.revision.RunnerVersion, RawOutputHash: digest([]byte(output)), TotalAssertions: len(e.Rules), Status: "failed", Rules: []TextRuleResult{}}
	if err = ctx.Err(); err != nil {
		result.Status, result.ErrorStage, result.ErrorCode, result.Reason = "cancelled", "scoring", "USER_CANCELLED", "text scoring cancelled"
		return result, err
	}
	formatErr := validateTextOutput(output)
	var actual, expected any
	if formatErr == nil && e.Category == "structured_json" {
		actual, formatErr = parseTextJSON([]byte(output))
	}
	contentOK := false
	if formatErr == nil {
		if e.Category == "structured_json" {
			expected, err = parseTextJSON(e.ExpectedOutput)
			if err != nil {
				return result, fmt.Errorf("trusted expected JSON invalid: %w", err)
			}
			contentOK = equalTextJSON(actual, expected)
		} else {
			var answer string
			if err = json.Unmarshal(e.ExpectedOutput, &answer); err != nil {
				return result, err
			}
			contentOK = normalizeTextAnswer(output) == normalizeTextAnswer(answer)
		}
	}
	for i, rule := range e.Rules {
		verdict := TextRuleResult{TextRule: rule, Passed: formatErr == nil && (i == 0 || contentOK)}
		if !verdict.Passed {
			if formatErr != nil {
				verdict.Reason = formatErr.Error()
			} else {
				verdict.Reason = "输出内容、类型、字段集合或顺序不符合本题确定答案"
			}
		} else {
			result.PassedAssertions++
		}
		result.Rules = append(result.Rules, verdict)
	}
	result.Passed = result.PassedAssertions == result.TotalAssertions
	switch {
	case result.Passed:
		result.Status = "passed"
	case formatErr != nil:
		result.ErrorStage, result.ErrorCode, result.Reason = "output", "INVALID_OUTPUT", formatErr.Error()
	default:
		result.ErrorStage, result.ErrorCode, result.Reason = "assertion", "WRONG_ANSWER", "输出内容、类型、字段集合或顺序不符合本题确定答案"
	}
	return result, nil
}

func normalizeTextAnswer(s string) string {
	return strings.Trim(strings.ReplaceAll(s, "\r\n", "\n"), " \t\r\n")
}
func validateTextOutput(s string) error {
	if len(s) > maxTextOutputBytes {
		return fmt.Errorf("输出超过 65536 字节上限")
	}
	if !utf8.ValidString(s) {
		return fmt.Errorf("输出不是有效 UTF-8")
	}
	if normalizeTextAnswer(s) == "" {
		return fmt.Errorf("输出为空，缺少最终答案")
	}
	return nil
}

// parseTextJSON reads tokens rather than unmarshalling into a map, so duplicate
// keys (including escaped aliases) cannot be silently overwritten. All depth,
// numeric and byte bounds are host-defined; model text is never executed.
func parseTextJSON(raw []byte) (any, error) {
	if err := validateTextOutput(string(raw)); err != nil {
		return nil, err
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	v, err := readTextJSONValue(d, 0)
	if err != nil {
		return nil, err
	}
	if _, err = d.Token(); err != io.EOF {
		return nil, fmt.Errorf("JSON 后存在额外内容或多个值")
	}
	return v, nil
}

func readTextJSONValue(d *json.Decoder, depth int) (any, error) {
	if depth > 64 {
		return nil, fmt.Errorf("JSON 嵌套超过 64 层")
	}
	token, err := d.Token()
	if err != nil {
		return nil, fmt.Errorf("JSON 格式不完整或无效")
	}
	switch t := token.(type) {
	case json.Delim:
		switch t {
		case '{':
			out := map[string]any{}
			for d.More() {
				key, err := d.Token()
				if err != nil {
					return nil, fmt.Errorf("JSON 对象键无效")
				}
				name, ok := key.(string)
				if !ok {
					return nil, fmt.Errorf("JSON 对象键必须是字符串")
				}
				if _, exists := out[name]; exists {
					return nil, fmt.Errorf("JSON 包含重复键")
				}
				value, err := readTextJSONValue(d, depth+1)
				if err != nil {
					return nil, err
				}
				out[name] = value
			}
			end, err := d.Token()
			if err != nil || end != json.Delim('}') {
				return nil, fmt.Errorf("JSON 对象未闭合")
			}
			return out, nil
		case '[':
			out := []any{}
			for d.More() {
				value, err := readTextJSONValue(d, depth+1)
				if err != nil {
					return nil, err
				}
				out = append(out, value)
			}
			end, err := d.Token()
			if err != nil || end != json.Delim(']') {
				return nil, fmt.Errorf("JSON 数组未闭合")
			}
			return out, nil
		default:
			return nil, fmt.Errorf("意外 JSON 分隔符")
		}
	case json.Number:
		if len(t) > 128 {
			return nil, fmt.Errorf("JSON 数字超过 128 字符")
		}
		if i := strings.IndexAny(string(t), "eE"); i >= 0 {
			exponent, err := strconv.Atoi(string(t)[i+1:])
			if err != nil || exponent < -308 || exponent > 308 {
				return nil, fmt.Errorf("JSON 数字指数超出 -308..308")
			}
		}
		v, ok := new(big.Rat).SetString(string(t))
		if !ok {
			return nil, fmt.Errorf("JSON 数字无效")
		}
		return v, nil
	default:
		return t, nil
	}
}

func equalTextJSON(a, b any) bool {
	switch expected := b.(type) {
	case map[string]any:
		actual, ok := a.(map[string]any)
		if !ok || len(actual) != len(expected) {
			return false
		}
		for key, value := range expected {
			other, exists := actual[key]
			if !exists || !equalTextJSON(other, value) {
				return false
			}
		}
		return true
	case []any:
		actual, ok := a.([]any)
		if !ok || len(actual) != len(expected) {
			return false
		}
		for i := range expected {
			if !equalTextJSON(actual[i], expected[i]) {
				return false
			}
		}
		return true
	case *big.Rat:
		actual, ok := a.(*big.Rat)
		return ok && actual.Cmp(expected) == 0
	case string:
		actual, ok := a.(string)
		return ok && actual == expected
	case bool:
		actual, ok := a.(bool)
		return ok && actual == expected
	default:
		return a == nil && b == nil
	}
}
