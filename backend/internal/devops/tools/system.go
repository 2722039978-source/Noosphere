package tools

import (
	"os/exec"
	"strings"
)

type ToolResult struct {
	Success bool   `json:"success"`
	Output  string `json:"output"`
	Error   string `json:"error,omitempty"`
}

func ExecSystemInfo() ToolResult {
	out, err := exec.Command("systeminfo").CombinedOutput()
	if err != nil {
		return ToolResult{Success: false, Error: err.Error()}
	}
	return ToolResult{Success: true, Output: string(out)}
}

func ExecTaskList() ToolResult {
	out, err := exec.Command("tasklist", "/FO", "CSV", "/NH").CombinedOutput()
	if err != nil {
		return ToolResult{Success: false, Error: err.Error()}
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) > 20 {
		lines = lines[:20]
	}
	return ToolResult{Success: true, Output: strings.Join(lines, "\n")}
}

func ExecNetStat() ToolResult {
	out, err := exec.Command("netstat", "-ano").CombinedOutput()
	if err != nil {
		return ToolResult{Success: false, Error: err.Error()}
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) > 30 {
		lines = lines[:30]
	}
	return ToolResult{Success: true, Output: strings.Join(lines, "\n")}
}
