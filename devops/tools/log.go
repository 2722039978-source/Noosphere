package tools

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/devops-agent/agent"
)

// RegisterLogTools 注册日志相关工具
func RegisterLogTools(reg *agent.ToolRegistry) {
	reg.Register(agent.ToolDef{
		Name:        "search_logs",
		Description: "在日志文件中搜索关键词。支持正则表达式和上下文行数，类似 grep -C。",
		Category:    agent.CatLog,
		Parameters: []agent.ToolParam{
			{Name: "path", Type: "string", Description: "日志文件路径", Required: true},
			{Name: "keyword", Type: "string", Description: "搜索关键词（支持正则）", Required: true},
			{Name: "context_lines", Type: "int", Description: "上下文行数", Required: false, Default: 3},
			{Name: "max_results", Type: "int", Description: "最大返回行数", Required: false, Default: 100},
			{Name: "tail", Type: "int", Description: "只搜索最近 N 行", Required: false, Default: 1000},
		},
		Examples:       []string{`search_logs --path "C:\logs\app.log" --keyword "ERROR" --context_lines 5`},
		RiskLevel:      "low",
		RequireConfirm: false,
	}, searchLogsTool)

	reg.Register(agent.ToolDef{
		Name:        "parse_error_log",
		Description: "解析错误日志，统计错误数量和类型，提取最近的错误详情。",
		Category:    agent.CatLog,
		Parameters: []agent.ToolParam{
			{Name: "path", Type: "string", Description: "日志文件路径", Required: true},
			{Name: "level", Type: "enum", Description: "日志级别过滤", Required: false, Default: "ERROR", Enum: []string{"ERROR", "WARN", "FATAL", "ALL"}},
		},
		Examples:       []string{`parse_error_log --path "C:\logs\app.log" --level ERROR`},
		RiskLevel:      "low",
		RequireConfirm: false,
	}, parseErrorLogTool)

	reg.Register(agent.ToolDef{
		Name:        "tail_log",
		Description: "获取日志文件末尾 N 行，类似 tail 命令。",
		Category:    agent.CatLog,
		Parameters: []agent.ToolParam{
			{Name: "path", Type: "string", Description: "日志文件路径", Required: true},
			{Name: "lines", Type: "int", Description: "返回行数", Required: false, Default: 50},
		},
		Examples:       []string{`tail_log --path "C:\logs\app.log" --lines 100`},
		RiskLevel:      "low",
		RequireConfirm: false,
	}, tailLogTool)
}

func searchLogsTool(args map[string]any) (*agent.ToolResult, error) {
	path, _ := args["path"].(string)
	keyword, _ := args["keyword"].(string)
	ctxLines, _ := args["context_lines"].(float64)
	maxResults, _ := args["max_results"].(float64)
	tailLines, _ := args["tail"].(float64)

	if path == "" || keyword == "" {
		return &agent.ToolResult{Success: false, Error: "path and keyword are required"}, nil
	}
	if ctxLines <= 0 {
		ctxLines = 3
	}
	if maxResults <= 0 {
		maxResults = 100
	}
	if tailLines <= 0 {
		tailLines = 1000
	}

	start := time.Now()
	cmd := fmt.Sprintf(
		`Get-Content "%s" -Tail %d | Select-String "%s" -Context %d,%d | Select-Object -First %d | ForEach-Object { ">>> $_"; $_.Context.PreContext | ForEach-Object { "  | $_" }; ">>> " + $_.Line; $_.Context.PostContext | ForEach-Object { "  | $_" }; "---" }`,
		path, int(tailLines), escPS(keyword), int(ctxLines), int(ctxLines), int(maxResults),
	)
	output, err := exec.Command("powershell", "-NoProfile", "-Command", cmd).CombinedOutput()
	result := &agent.ToolResult{Duration: time.Since(start)}
	if err != nil {
		result.Success = false
		result.Error = err.Error()
		result.Output = string(output)
		return result, nil
	}
	outStr := string(output)
	if strings.TrimSpace(outStr) == "" {
		outStr = fmt.Sprintf("在 %s 中未找到匹配 '%s' 的日志", path, keyword)
	}
	result.Success = true
	result.Output = outStr
	return result, nil
}

func parseErrorLogTool(args map[string]any) (*agent.ToolResult, error) {
	path, _ := args["path"].(string)
	level, _ := args["level"].(string)
	if path == "" {
		return &agent.ToolResult{Success: false, Error: "path is required"}, nil
	}
	if level == "" {
		level = "ERROR"
	}
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return &agent.ToolResult{Success: false, Error: fmt.Sprintf("file not found: %s", path)}, nil
	}

	start := time.Now()
	var pattern string
	switch level {
	case "ALL":
		pattern = "ERROR|WARN|FATAL|CRITICAL|Exception|panic|Failed"
	case "ERROR":
		pattern = "ERROR|FATAL|CRITICAL|Exception|panic"
	case "WARN":
		pattern = "WARN|WARNING"
	case "FATAL":
		pattern = "FATAL|CRITICAL|panic"
	}

	cmd := fmt.Sprintf(`
$lines = Get-Content "%s" | Select-String "%s"
$total = $lines.Count
$recent = $lines | Select-Object -Last 20
Write-Host "=== 错误日志解析报告 ==="
Write-Host "文件: %s"
Write-Host "过滤级别: %s"
Write-Host "匹配行总数: $total"
Write-Host ""
Write-Host "=== 最近20条 ==="
$recent | ForEach-Object { Write-Host $_.Line }
`, path, pattern, path, level)

	output, err := exec.Command("powershell", "-NoProfile", "-Command", cmd).CombinedOutput()
	result := &agent.ToolResult{Duration: time.Since(start)}
	if err != nil {
		result.Success = false
		result.Error = err.Error()
		result.Output = string(output)
		return result, nil
	}
	result.Success = true
	result.Output = string(output)
	return result, nil
}

func tailLogTool(args map[string]any) (*agent.ToolResult, error) {
	path, _ := args["path"].(string)
	lines, _ := args["lines"].(float64)
	if path == "" {
		return &agent.ToolResult{Success: false, Error: "path is required"}, nil
	}
	if lines <= 0 {
		lines = 50
	}
	start := time.Now()
	cmd := fmt.Sprintf(`Get-Content "%s" -Tail %d`, path, int(lines))
	output, err := exec.Command("powershell", "-NoProfile", "-Command", cmd).CombinedOutput()
	result := &agent.ToolResult{Duration: time.Since(start)}
	if err != nil {
		result.Success = false
		result.Error = err.Error()
		result.Output = string(output)
		return result, nil
	}
	result.Success = true
	result.Output = string(output)
	return result, nil
}

func escPS(s string) string {
	s = strings.ReplaceAll(s, `"`, "`\"")
	s = strings.ReplaceAll(s, "$", "`$")
	return s
}
