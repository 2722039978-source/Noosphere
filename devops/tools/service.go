package tools

import (
	"fmt"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/devops-agent/agent"
)

// RegisterServiceTools 注册服务管理工具
func RegisterServiceTools(reg *agent.ToolRegistry) {
	reg.Register(agent.ToolDef{
		Name:        "check_service",
		Description: "检查指定服务的运行状态。Windows 检查 Windows Service，Linux 检查 systemd。",
		Category:    agent.CatService,
		Parameters: []agent.ToolParam{
			{Name: "name", Type: "string", Description: "服务名称", Required: true},
		},
		Examples:       []string{`check_service --name "MSSQLSERVER"`, `check_service --name "nginx"`},
		RiskLevel:      "low",
		RequireConfirm: false,
	}, checkServiceTool)

	reg.Register(agent.ToolDef{
		Name:        "restart_service",
		Description: "重启指定服务。需要管理员权限，执行前会先检查当前状态。",
		Category:    agent.CatService,
		Parameters: []agent.ToolParam{
			{Name: "name", Type: "string", Description: "服务名称", Required: true},
		},
		Examples:       []string{`restart_service --name "MSSQLSERVER"`},
		RiskLevel:      "critical",
		RequireConfirm: true,
	}, restartServiceTool)

	reg.Register(agent.ToolDef{
		Name:        "list_services",
		Description: "列出所有服务及其运行状态，可按状态和名称过滤。",
		Category:    agent.CatService,
		Parameters: []agent.ToolParam{
			{Name: "status", Type: "enum", Description: "状态过滤", Required: false, Default: "all", Enum: []string{"all", "running", "stopped"}},
			{Name: "filter", Type: "string", Description: "服务名过滤", Required: false},
		},
		Examples:       []string{"list_services --status running", "list_services --filter sql"},
		RiskLevel:      "low",
		RequireConfirm: false,
	}, listServicesTool)
}

func checkServiceTool(args map[string]any) (*agent.ToolResult, error) {
	name, _ := args["name"].(string)
	if name == "" {
		return &agent.ToolResult{Success: false, Error: "service name is required"}, nil
	}
	start := time.Now()

	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.Command("powershell", "-NoProfile", "-Command",
			fmt.Sprintf("Get-Service -Name '%s' -ErrorAction SilentlyContinue | Select-Object Name,DisplayName,Status,StartType | Format-List", name))
	} else {
		cmd = exec.Command("systemctl", "status", name)
	}

	output, err := cmd.CombinedOutput()
	result := &agent.ToolResult{Duration: time.Since(start)}
	if err != nil {
		outStr := string(output)
		result.Success = true
		if strings.Contains(outStr, "not found") || strings.Contains(outStr, "NotFound") {
			result.Output = fmt.Sprintf("服务 '%s' 不存在", name)
		} else {
			result.Output = outStr
		}
		return result, nil
	}
	result.Success = true
	result.Output = string(output)
	return result, nil
}

func restartServiceTool(args map[string]any) (*agent.ToolResult, error) {
	name, _ := args["name"].(string)
	if name == "" {
		return &agent.ToolResult{Success: false, Error: "service name is required"}, nil
	}
	start := time.Now()

	if runtime.GOOS == "windows" {
		stopOut, _ := exec.Command("powershell", "-NoProfile", "-Command",
			fmt.Sprintf("Stop-Service -Name '%s' -Force -ErrorAction Stop", name)).CombinedOutput()
		time.Sleep(2 * time.Second)
		startOut, startErr := exec.Command("powershell", "-NoProfile", "-Command",
			fmt.Sprintf("Start-Service -Name '%s' -ErrorAction Stop", name)).CombinedOutput()

		if startErr != nil {
			return &agent.ToolResult{
				Success:  false,
				Error:    startErr.Error(),
				Output:   fmt.Sprintf("STOP:\n%s\nSTART:\n%s", string(stopOut), string(startOut)),
				Duration: time.Since(start),
			}, nil
		}
		return &agent.ToolResult{
			Success:  true,
			Output:   fmt.Sprintf("服务 '%s' 已成功重启", name),
			Duration: time.Since(start),
		}, nil
	}

	output, err := exec.Command("systemctl", "restart", name).CombinedOutput()
	if err != nil {
		return &agent.ToolResult{Success: false, Error: err.Error(), Output: string(output), Duration: time.Since(start)}, nil
	}
	return &agent.ToolResult{Success: true, Output: fmt.Sprintf("服务 '%s' 重启成功", name), Duration: time.Since(start)}, nil
}

func listServicesTool(args map[string]any) (*agent.ToolResult, error) {
	status, _ := args["status"].(string)
	filter, _ := args["filter"].(string)
	start := time.Now()

	if runtime.GOOS == "windows" {
		psCmd := "Get-Service"
		switch status {
		case "running":
			psCmd += " | Where-Object {$_.Status -eq 'Running'}"
		case "stopped":
			psCmd += " | Where-Object {$_.Status -eq 'Stopped'}"
		}
		if filter != "" {
			psCmd += fmt.Sprintf(` | Where-Object {$_.Name -like '*%s*' -or $_.DisplayName -like '*%s*'}`, filter, filter)
		}
		psCmd += " | Select-Object Name,DisplayName,Status,StartType | Format-Table -AutoSize"
		output, err := exec.Command("powershell", "-NoProfile", "-Command", psCmd).CombinedOutput()
		if err != nil {
			return &agent.ToolResult{Success: false, Error: err.Error(), Duration: time.Since(start)}, nil
		}
		return &agent.ToolResult{Success: true, Output: string(output), Duration: time.Since(start)}, nil
	}

	args_list := []string{"list-units", "--type=service", "--no-pager"}
	if status == "running" {
		args_list = append(args_list, "--state=running")
	}
	output, err := exec.Command("systemctl", args_list...).CombinedOutput()
	if err != nil {
		return &agent.ToolResult{Success: false, Error: err.Error(), Output: string(output), Duration: time.Since(start)}, nil
	}
	return &agent.ToolResult{Success: true, Output: string(output), Duration: time.Since(start)}, nil
}
