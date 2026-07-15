// Package tools — DevOps 工具实现
//
// 系统命令执行、进程管理、端口检查等运维工具。
// 适配 Windows (PowerShell/WMI)，同时兼容 Linux (procfs/systemd)。
package tools

import (
	"fmt"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/devops-agent/agent"
)

// RegisterSystemTools 注册系统相关工具
func RegisterSystemTools(reg *agent.ToolRegistry) {
	reg.Register(agent.ToolDef{
		Name:        "exec_command",
		Description: "在服务器上执行系统命令并返回输出。Windows 使用 PowerShell，Linux 使用 sh。",
		Category:    agent.CatSystem,
		Parameters: []agent.ToolParam{
			{Name: "command", Type: "string", Description: "要执行的命令", Required: true},
			{Name: "timeout", Type: "int", Description: "超时（秒），默认30", Required: false, Default: 30},
		},
		Examples:       []string{`exec_command --command "Get-Process \| Sort-Object CPU -Descending \| Select-Object -First 5"`},
		RiskLevel:      "high",
		RequireConfirm: true,
	}, execCommandTool)

	reg.Register(agent.ToolDef{
		Name:           "get_system_info",
		Description:    "获取操作系统基本信息：主机名、OS版本、架构、运行时间等",
		Category:       agent.CatSystem,
		Parameters:     []agent.ToolParam{},
		Examples:       []string{"get_system_info"},
		RiskLevel:      "low",
		RequireConfirm: false,
	}, getSystemInfoTool)

	reg.Register(agent.ToolDef{
		Name:        "list_processes",
		Description: "列出进程，按 CPU 或内存排序。用于排查资源占用异常。",
		Category:    agent.CatSystem,
		Parameters: []agent.ToolParam{
			{Name: "sort_by", Type: "enum", Description: "排序字段", Required: false, Default: "cpu", Enum: []string{"cpu", "memory", "name"}},
			{Name: "top_n", Type: "int", Description: "返回前 N 条", Required: false, Default: 10},
			{Name: "filter", Type: "string", Description: "进程名过滤", Required: false},
		},
		Examples:       []string{"list_processes --sort_by memory --top_n 10"},
		RiskLevel:      "low",
		RequireConfirm: false,
	}, listProcessesTool)

	reg.Register(agent.ToolDef{
		Name:        "check_port",
		Description: "检查指定端口占用情况，返回占用进程信息。",
		Category:    agent.CatNetwork,
		Parameters: []agent.ToolParam{
			{Name: "port", Type: "int", Description: "端口号", Required: true},
		},
		Examples:       []string{"check_port --port 8080", "check_port --port 3306"},
		RiskLevel:      "low",
		RequireConfirm: false,
	}, checkPortTool)
}

func execCommandTool(args map[string]any) (*agent.ToolResult, error) {
	cmd, _ := args["command"].(string)
	if cmd == "" {
		return &agent.ToolResult{Success: false, Error: "command is required"}, nil
	}
	start := time.Now()
	var exe *exec.Cmd
	if runtime.GOOS == "windows" {
		exe = exec.Command("powershell", "-NoProfile", "-NonInteractive", "-Command", cmd)
	} else {
		exe = exec.Command("sh", "-c", cmd)
	}
	output, err := exe.CombinedOutput()
	result := &agent.ToolResult{CallID: "", Duration: time.Since(start)}
	if err != nil {
		result.Success = false
		result.Error = err.Error()
	}
	result.Output = string(output)
	result.Success = true
	return result, nil
}

func getSystemInfoTool(args map[string]any) (*agent.ToolResult, error) {
	start := time.Now()
	info := make(map[string]any)
	info["os"] = runtime.GOOS
	info["arch"] = runtime.GOARCH
	info["num_cpu"] = runtime.NumCPU()

	hostOut, _ := exec.Command("hostname").CombinedOutput()
	info["hostname"] = strings.TrimSpace(string(hostOut))

	if runtime.GOOS == "windows" {
		osOut, _ := exec.Command("powershell", "-NoProfile", "-Command",
			"(Get-CimInstance Win32_OperatingSystem).Caption").CombinedOutput()
		info["os_version"] = strings.TrimSpace(string(osOut))
	} else {
		osOut, _ := exec.Command("uname", "-a").CombinedOutput()
		info["os_version"] = strings.TrimSpace(string(osOut))
	}

	return &agent.ToolResult{
		Success:  true,
		Output:   agent.ToJSON(info),
		Data:     info,
		Duration: time.Since(start),
	}, nil
}

func listProcessesTool(args map[string]any) (*agent.ToolResult, error) {
	start := time.Now()
	sortBy, _ := args["sort_by"].(string)
	if sortBy == "" {
		sortBy = "cpu"
	}
	topN, _ := args["top_n"].(float64)
	if topN <= 0 {
		topN = 10
	}
	filter, _ := args["filter"].(string)

	var psCmd string
	switch sortBy {
	case "memory":
		psCmd = "Get-Process | Sort-Object WorkingSet64 -Descending"
	case "name":
		psCmd = "Get-Process | Sort-Object Name"
	default:
		psCmd = "Get-Process | Sort-Object CPU -Descending"
	}
	if filter != "" {
		psCmd = fmt.Sprintf("Get-Process -Name '*%s*' | Sort-Object CPU -Descending", filter)
	}
	psCmd += fmt.Sprintf(" | Select-Object -First %d | Format-Table Name,Id,CPU,@{N='MemoryMB';E={[math]::Round($_.WorkingSet64/1MB,1)}} -AutoSize", int(topN))

	output, err := exec.Command("powershell", "-NoProfile", "-Command", psCmd).CombinedOutput()
	if err != nil {
		return &agent.ToolResult{Success: false, Error: err.Error(), Duration: time.Since(start)}, nil
	}
	return &agent.ToolResult{Success: true, Output: string(output), Duration: time.Since(start)}, nil
}

func checkPortTool(args map[string]any) (*agent.ToolResult, error) {
	port, _ := args["port"].(float64)
	if port <= 0 {
		return &agent.ToolResult{Success: false, Error: "port is required"}, nil
	}
	start := time.Now()
	cmd := fmt.Sprintf("Get-NetTCPConnection -LocalPort %d -ErrorAction SilentlyContinue | Select-Object LocalPort,State,OwningProcess | Format-List", int(port))
	output, err := exec.Command("powershell", "-NoProfile", "-Command", cmd).CombinedOutput()
	outStr := string(output)

	result := &agent.ToolResult{Duration: time.Since(start)}
	if err != nil {
		result.Success = false
		result.Error = err.Error()
		result.Output = outStr
		return result, nil
	}
	if strings.TrimSpace(outStr) == "" {
		result.Output = fmt.Sprintf("端口 %d 未被占用", int(port))
	} else {
		result.Output = outStr
	}
	result.Success = true
	return result, nil
}
