package tools

import (
	"os/exec"
	"strings"
)

func ExecServiceList() ToolResult {
	out, err := exec.Command("sc", "query", "state=", "all").CombinedOutput()
	if err != nil {
		return ToolResult{Success: false, Error: err.Error()}
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) > 30 {
		lines = lines[:30]
	}
	return ToolResult{Success: true, Output: strings.Join(lines, "\n")}
}

func ExecServiceStatus(name string) ToolResult {
	out, err := exec.Command("sc", "query", name).CombinedOutput()
	if err != nil {
		return ToolResult{Success: false, Error: err.Error()}
	}
	return ToolResult{Success: true, Output: string(out)}
}
