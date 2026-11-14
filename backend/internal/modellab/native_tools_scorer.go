package modellab

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"math/big"
	"time"

	"github.com/noosphere/backend/internal/gateway/providers"
)

type NativeToolTranscript struct {
	FirstOutput string               `json:"first_output"`
	Calls       []providers.ToolCall `json:"calls"`
	Feedback    json.RawMessage      `json:"feedback,omitempty"`
	FinalOutput string               `json:"final_output"`
	FinalCalls  []providers.ToolCall `json:"final_calls,omitempty"`
}

// This finite switch is the entire tool executor. It never reads files, calls a
// process, accesses a network, changes state or dispatches arbitrary function names.
func executeBenchmarkTool(call providers.ToolCall) (json.RawMessage, error) {
	parsed, err := parseTextJSON([]byte(call.Function.Arguments))
	if err != nil {
		return nil, err
	}
	args, ok := parsed.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("tool arguments must be an object")
	}
	switch call.Function.Name {
	case "inventory_lookup":
		if len(args) != 1 {
			return nil, fmt.Errorf("inventory requires only sku")
		}
		sku, ok := args["sku"].(string)
		if !ok {
			return nil, fmt.Errorf("sku must be a string")
		}
		stock := map[string]int{"PENCIL": 12, "ERASER": 5}
		n, ok := stock[sku]
		if !ok {
			return nil, fmt.Errorf("sku not in fixed fixture")
		}
		return json.Marshal(struct {
			SKU       string `json:"sku"`
			Available int    `json:"available"`
		}{sku, n})
	case "convert_length":
		if len(args) != 3 {
			return nil, fmt.Errorf("conversion requires value, from and to")
		}
		number, ok := args["value"].(*big.Rat)
		if !ok {
			return nil, fmt.Errorf("value must be numeric")
		}
		v, _ := number.Float64()
		if math.IsNaN(v) || math.IsInf(v, 0) || v < 0 || v > 10000 {
			return nil, fmt.Errorf("value outside finite fixture bounds")
		}
		from, fromOK := args["from"].(string)
		to, toOK := args["to"].(string)
		if !fromOK || !toOK || from != "cm" || to != "m" {
			return nil, fmt.Errorf("only cm to m conversion is provided")
		}
		return json.Marshal(struct {
			Value float64 `json:"value"`
			Unit  string  `json:"unit"`
		}{v / 100, "m"})
	default:
		return nil, fmt.Errorf("unknown benchmark tool")
	}
}

func toolCallMatches(call providers.ToolCall, e CaseEvidence) bool {
	if call.ID == "" || call.Type != "function" || call.Function.Name != e.NativeTools.ExpectedCall.Name {
		return false
	}
	a, err := parseTextJSON([]byte(call.Function.Arguments))
	if err != nil {
		return false
	}
	b, err := parseTextJSON([]byte(e.NativeTools.ExpectedCall.Arguments))
	return err == nil && equalTextJSON(a, b)
}

func scoreNativeTools(ctx context.Context, ref CaseRef, source string) (result TextScoreResult, err error) {
	start := time.Now()
	defer func() { result.TotalDuration = time.Since(start) }()
	d, e, err := authoritative(ref)
	if err != nil {
		return result, err
	}
	if !d.isTools() || e.NativeTools == nil {
		return result, fmt.Errorf("trusted native tool case required")
	}
	result = TextScoreResult{TaskType: "native_tool_call", CaseID: e.ID, CaseVersion: e.Version, CaseHash: e.CaseHash, FixtureHash: e.FixtureHash, DatasetID: d.revision.BenchmarkID, DatasetVersion: d.revision.Version, DatasetHash: d.revision.ContentHash, ScorerVersion: d.revision.ScorerVersion, ScorerHash: d.scorerHash, RunnerVersion: d.revision.RunnerVersion, RawOutputHash: digest([]byte(source)), TotalAssertions: 4, Status: "failed", Rules: []TextRuleResult{}}
	if err = ctx.Err(); err != nil {
		return result, err
	}
	var trace NativeToolTranscript
	formatErr := validateTextOutput(source)
	if formatErr == nil {
		formatErr = strictJSON([]byte(source), &trace)
	}
	passed := [4]bool{}
	if formatErr == nil && len(trace.Calls) == 1 {
		call := trace.Calls[0]
		passed[0] = call.ID != "" && call.Type == "function"
		passed[1] = passed[0] && toolCallMatches(call, e)
		if passed[1] {
			feedback, execErr := executeBenchmarkTool(call)
			a, aErr := parseTextJSON(feedback)
			b, bErr := parseTextJSON(trace.Feedback)
			passed[2] = execErr == nil && aErr == nil && bErr == nil && equalTextJSON(a, b)
		}
		actual, parseErr := parseTextJSON([]byte(trace.FinalOutput))
		expected, expectedErr := parseTextJSON(e.ExpectedOutput)
		passed[3] = passed[2] && len(trace.FinalCalls) == 0 && parseErr == nil && expectedErr == nil && equalTextJSON(actual, expected)
	}
	reasons := []string{"未保存恰好一个原生函数调用", "函数名称或参数与任务不符", "未观察到可核验的固定工具反馈", "反馈后的最终 JSON 不完整、不正确或继续调用工具"}
	for i, rule := range e.Rules {
		v := TextRuleResult{TextRule: rule, Passed: passed[i]}
		if passed[i] {
			result.PassedAssertions++
		} else {
			v.Reason = reasons[i]
		}
		result.Rules = append(result.Rules, v)
	}
	result.Passed = result.PassedAssertions == 4
	if result.Passed {
		result.Status = "passed"
	} else {
		result.ErrorStage, result.ErrorCode, result.Reason = "assertion", "WRONG_ANSWER", "原生工具调用、参数、反馈或最终答案未通过固定规则"
		if formatErr != nil {
			result.ErrorStage, result.ErrorCode, result.Reason = "output", "INVALID_OUTPUT", formatErr.Error()
		}
	}
	return result, nil
}
