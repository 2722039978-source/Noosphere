package tools

import (
	"os"
	"strings"
)

func ExecTailLog(path string, lines int) ToolResult {
	if path == "" {
		path = "./logs/noosphere-2026-07-26.log"
	}
	if lines <= 0 {
		lines = 20
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return ToolResult{Success: false, Error: err.Error()}
	}
	allLines := strings.Split(strings.TrimSpace(string(data)), "\n")
	start := len(allLines) - lines
	if start < 0 {
		start = 0
	}
	return ToolResult{Success: true, Output: strings.Join(allLines[start:], "\n")}
}

func ExecSearchLog(path string, keyword string) ToolResult {
	if path == "" {
		path = "./logs/noosphere-2026-07-26.log"
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return ToolResult{Success: false, Error: err.Error()}
	}
	var matched []string
	for _, line := range strings.Split(string(data), "\n") {
		if strings.Contains(strings.ToLower(line), strings.ToLower(keyword)) {
			matched = append(matched, line)
		}
	}
	if len(matched) > 50 {
		matched = matched[:50]
	}
	return ToolResult{Success: true, Output: strings.Join(matched, "\n")}
}
