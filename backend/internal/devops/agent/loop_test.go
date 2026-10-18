package agent_test

import (
	"testing"

	"github.com/noosphere/backend/internal/devops/agent"
)

func TestParsePlanResponse(t *testing.T) {
	tests := []struct {
		input   string
		wantAct string
	}{
		{"THOUGHT: check cpu first\nACTION: system_info\n", "system_info"},
		{"THOUGHT: enough data\nACTION: respond", "respond"},
		{"ACTION: netstat", "netstat"},
		{"no explicit action here", "respond"},
	}

	for _, tc := range tests {
		r := agent.ParsePlanResponseForTest(tc.input)
		if r != tc.wantAct {
			t.Errorf("parsePlan(%q) = %q, want %q", tc.input, r, tc.wantAct)
		}
	}
}

func TestRiskLevels(t *testing.T) {
	if agent.RiskLow.String() != "low" {
		t.Error("RiskLow string mismatch")
	}
	if agent.RiskCritical.String() != "critical" {
		t.Error("RiskCritical string mismatch")
	}
}
