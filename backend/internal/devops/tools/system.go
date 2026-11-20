package tools

import "context"

type ToolResult struct {
	Success bool   `json:"success"`
	Output  string `json:"output"`
	Error   string `json:"error,omitempty"`
}

func ExecSystemInfo() ToolResult {
	return ExecSystemInfoContext(context.Background())
}

func ExecTaskList() ToolResult {
	return ExecTaskListContext(context.Background())
}

func ExecNetStat() ToolResult {
	return ExecNetStatContext(context.Background())
}
