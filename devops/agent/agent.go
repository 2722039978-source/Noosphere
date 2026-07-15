package agent

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/nebula-agent/nebula/engine"
)

// DevOpsAgent 运维智能体
//
// 核心能力：
//  1. 理解运维需求 → 推荐/调用工具
//  2. 动态执行系统命令、日志查询、服务管理等工具
//  3. 采集系统指标并结合 LLM 进行故障分析
//  4. 使用 Nebula Memory 保存和检索历史故障记录
type DevOpsAgent struct {
	mu          sync.RWMutex
	state       AgentState
	registry    *ToolRegistry
	nebulaEngine *engine.Engine
	config      *Config
}

// Config Agent 配置
type Config struct {
	MaxToolsPerTask int
	ToolTimeout     time.Duration
	AutoExecute     bool
	SaveDiagnosis   bool
	SessionID       string
}

// DefaultConfig 默认配置
func DefaultConfig() *Config {
	return &Config{
		MaxToolsPerTask: 10,
		ToolTimeout:     30 * time.Second,
		AutoExecute:     false,
		SaveDiagnosis:   true,
		SessionID:       "devops-agent",
	}
}

// ChatMessage 聊天消息
type ChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// NewDevOpsAgent 创建 DevOps Agent
func NewDevOpsAgent(eng *engine.Engine, cfg *Config) *DevOpsAgent {
	if cfg == nil {
		cfg = DefaultConfig()
	}
	return &DevOpsAgent{
		registry:     NewToolRegistry(),
		nebulaEngine: eng,
		config:       cfg,
		state:        StateIdle,
	}
}

// Registry 返回工具注册中心
func (a *DevOpsAgent) Registry() *ToolRegistry {
	return a.registry
}

// Engine 返回 Nebula 引擎
func (a *DevOpsAgent) Engine() *engine.Engine {
	return a.nebulaEngine
}

// State 返回当前状态
func (a *DevOpsAgent) State() AgentState {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.state
}

// ExecuteTool 直接执行指定工具
func (a *DevOpsAgent) ExecuteTool(call ToolCall) (*ToolResult, error) {
	a.setState(StateExecuting)
	defer a.setState(StateIdle)
	call.Timestamp = time.Now()
	return a.registry.Execute(call)
}

// ExecuteWithContext 带超时的工具执行
func (a *DevOpsAgent) ExecuteWithContext(ctx context.Context, call ToolCall) (*ToolResult, error) {
	type resultWrapper struct {
		result *ToolResult
		err    error
	}
	ch := make(chan resultWrapper, 1)
	go func() {
		r, e := a.registry.Execute(call)
		ch <- resultWrapper{r, e}
	}()
	select {
	case <-ctx.Done():
		return &ToolResult{CallID: call.ID, Success: false, Error: "tool execution timeout"}, ctx.Err()
	case rw := <-ch:
		return rw.result, rw.err
	}
}

// Diagnose 运行诊断任务
func (a *DevOpsAgent) Diagnose(task *AgentTask) (*AgentResponse, error) {
	a.setState(StateThinking)
	defer a.setState(StateIdle)
	start := time.Now()

	if task.MaxTools <= 0 {
		task.MaxTools = a.config.MaxToolsPerTask
	}

	response := &AgentResponse{
		TaskID:    task.ID,
		Model:     "devops-agent-v1",
		Timestamp: time.Now(),
	}

	// 诊断流程：工具建议 + 系统指标分析
	response.Diagnosis = a.buildToolGuidedDiagnosis(task)
	response.Duration = time.Since(start)

	return response, nil
}

// buildToolGuidedDiagnosis 基于可用工具的引导诊断
func (a *DevOpsAgent) buildToolGuidedDiagnosis(task *AgentTask) string {
	tools := a.registry.ListDefs()

	diagnosis := fmt.Sprintf("## DevOps Agent 分析报告\n\n")
	diagnosis += fmt.Sprintf("**任务**: %s\n\n", task.Query)
	diagnosis += "### 建议使用的工具\n\n"

	// 根据查询内容推荐工具
	queryLower := task.Query
	for _, t := range tools {
		score := matchToolToQuery(t, queryLower)
		if score > 0 {
			diagnosis += fmt.Sprintf("- **%s** (相关度: %d%%)\n  %s\n", t.Name, score, t.Description)
			if len(t.Examples) > 0 {
				diagnosis += fmt.Sprintf("  示例: `%s`\n", t.Examples[0])
			}
		}
	}

	diagnosis += "\n### 可用工具列表\n"
	for _, t := range tools {
		icon := "🔧"
		switch t.Category {
		case CatSystem:
			icon = "🖥️"
		case CatLog:
			icon = "📋"
		case CatService:
			icon = "⚙️"
		case CatNetwork:
			icon = "🌐"
		case CatMetrics:
			icon = "📊"
		}
		riskBadge := ""
		if t.RiskLevel == "high" || t.RiskLevel == "critical" {
			riskBadge = " ⚠️"
		}
		diagnosis += fmt.Sprintf("- %s **%s**%s: %s\n", icon, t.Name, riskBadge, t.Description)
	}

	return diagnosis
}

// matchToolToQuery 简单关键词匹配工具推荐
func matchToolToQuery(tool ToolDef, query string) int {
	score := 0
	keywords := map[string][]string{
		"exec_command":    {"执行", "命令", "运行", "exec", "run", "shell"},
		"get_system_info": {"系统信息", "系统", "版本", "os", "system", "info"},
		"list_processes":  {"进程", "process", "cpu", "内存", "memory"},
		"check_port":      {"端口", "port", "占用", "端口冲突"},
		"search_logs":     {"日志", "log", "搜索", "查找", "grep"},
		"parse_error_log": {"错误", "error", "异常", "解析日志", "报错"},
		"tail_log":        {"tail", "末尾", "最新", "实时"},
		"check_service":   {"服务", "service", "状态", "运行"},
		"restart_service": {"重启", "restart", "重启服务"},
		"list_services":   {"服务列表", "列出服务", "services"},
	}

	if ks, ok := keywords[tool.Name]; ok {
		for _, kw := range ks {
			if contains(query, kw) {
				score += 20
			}
		}
	}
	if score > 80 {
		score = 80
	}
	return score
}

func contains(s, substr string) bool {
	return len(s) > 0 && len(substr) > 0 &&
		(s == substr || len(s) >= len(substr) && findSubstr(s, substr))
}

func findSubstr(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

func (a *DevOpsAgent) setState(s AgentState) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.state = s
}

var _ = log.Println
var _ = fmt.Sprintf
