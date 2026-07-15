// Package agent — DevOps Agent 核心类型定义
//
// 面向服务器运维场景，定义 Agent 的核心接口、工具描述、任务和响应类型。
// 支持 Tool Calling、日志分析和系统监控。
package agent

import (
	"encoding/json"
	"time"
)

// AgentState Agent 运行状态
type AgentState string

const (
	StateIdle       AgentState = "idle"
	StateThinking   AgentState = "thinking"
	StateExecuting  AgentState = "executing"
	StateResponding AgentState = "responding"
	StateError      AgentState = "error"
)

// ToolCategory 工具分类
type ToolCategory string

const (
	CatSystem  ToolCategory = "system"  // 系统命令
	CatLog     ToolCategory = "log"     // 日志查询
	CatService ToolCategory = "service" // 服务管理
	CatNetwork ToolCategory = "network" // 网络诊断
	CatMetrics ToolCategory = "metrics" // 指标采集
	CatMemory  ToolCategory = "memory"  // 记忆检索
)

// ─── Tool Definition ───

// ToolDef 工具定义——注册到 Agent 的工具描述
type ToolDef struct {
	Name           string            `json:"name"`
	Description    string            `json:"description"`
	Category       ToolCategory      `json:"category"`
	Parameters     []ToolParam       `json:"parameters"`
	Examples       []string          `json:"examples,omitempty"`
	RiskLevel      string            `json:"risk_level"`
	RequireConfirm bool              `json:"require_confirm"`
	Metadata       map[string]string `json:"metadata,omitempty"`
}

// ToolParam 工具参数定义
type ToolParam struct {
	Name        string   `json:"name"`
	Type        string   `json:"type"`
	Description string   `json:"description"`
	Required    bool     `json:"required"`
	Default     any      `json:"default,omitempty"`
	Enum        []string `json:"enum,omitempty"`
}

// ToolCall Agent 发起的一次工具调用
type ToolCall struct {
	ID        string         `json:"id"`
	Name      string         `json:"name"`
	Args      map[string]any `json:"args"`
	Timestamp time.Time      `json:"timestamp"`
}

// ToolResult 工具执行结果
type ToolResult struct {
	CallID   string         `json:"call_id"`
	Success  bool           `json:"success"`
	Output   string         `json:"output"`
	Error    string         `json:"error,omitempty"`
	Data     map[string]any `json:"data,omitempty"`
	Duration time.Duration  `json:"duration"`
}

// ─── Task & Response ───

// AgentTask 用户提交给 Agent 的任务
type AgentTask struct {
	ID         string            `json:"id"`
	SessionID  string            `json:"session_id"`
	Query      string            `json:"query"`
	ServerName string            `json:"server_name,omitempty"`
	TimeRange  *TimeRange        `json:"time_range,omitempty"`
	Tags       []string          `json:"tags,omitempty"`
	Metadata   map[string]string `json:"metadata,omitempty"`
	MaxTools   int               `json:"max_tools"`
	AutoExec   bool              `json:"auto_exec"`
	StreamMode bool              `json:"stream_mode"`
}

// TimeRange 时间范围
type TimeRange struct {
	From time.Time `json:"from"`
	To   time.Time `json:"to"`
}

// AgentResponse Agent 对任务的响应
type AgentResponse struct {
	TaskID       string           `json:"task_id"`
	Thought      string           `json:"thought,omitempty"`
	Diagnosis    string           `json:"diagnosis,omitempty"`
	RootCause    string           `json:"root_cause,omitempty"`
	Suggestions  []string         `json:"suggestions,omitempty"`
	ToolCalls    []ToolCall       `json:"tool_calls,omitempty"`
	ToolResults  []ToolResult     `json:"tool_results,omitempty"`
	RelatedFaults []FaultRecord   `json:"related_faults,omitempty"`
	SavedAsMemory bool            `json:"saved_as_memory"`
	MetricsSnapshot *MetricsSnapshot `json:"metrics_snapshot,omitempty"`
	Model        string           `json:"model"`
	TotalTokens  int              `json:"total_tokens"`
	Duration     time.Duration    `json:"duration"`
	Timestamp    time.Time        `json:"timestamp"`
}

// ─── Metrics ───

// MetricsSnapshot 系统指标快照
type MetricsSnapshot struct {
	Timestamp time.Time   `json:"timestamp"`
	CPU       CPUMetrics  `json:"cpu"`
	Memory    MemMetrics  `json:"memory"`
	Disk      DiskMetrics `json:"disk"`
	Network   NetMetrics  `json:"network,omitempty"`
	Processes []ProcInfo  `json:"processes,omitempty"`
	Services  []SvcStatus `json:"services,omitempty"`
}

// CPUMetrics CPU 指标
type CPUMetrics struct {
	UsagePercent float64 `json:"usage_percent"`
	Cores        int     `json:"cores"`
	LoadAvg1     float64 `json:"load_avg_1,omitempty"`
	LoadAvg5     float64 `json:"load_avg_5,omitempty"`
	LoadAvg15    float64 `json:"load_avg_15,omitempty"`
}

// MemMetrics 内存指标
type MemMetrics struct {
	TotalGB      float64 `json:"total_gb"`
	UsedGB       float64 `json:"used_gb"`
	AvailableGB  float64 `json:"available_gb"`
	UsagePercent float64 `json:"usage_percent"`
}

// DiskMetrics 磁盘指标
type DiskMetrics struct {
	TotalGB      float64          `json:"total_gb"`
	UsedGB       float64          `json:"used_gb"`
	UsagePercent float64          `json:"usage_percent"`
	Partitions   []DiskPartition  `json:"partitions,omitempty"`
}

// DiskPartition 分区信息
type DiskPartition struct {
	MountPoint   string  `json:"mount_point"`
	TotalGB      float64 `json:"total_gb"`
	UsedGB       float64 `json:"used_gb"`
	UsagePercent float64 `json:"usage_percent"`
}

// NetMetrics 网络指标
type NetMetrics struct {
	RxMBps      float64 `json:"rx_mbps"`
	TxMBps      float64 `json:"tx_mbps"`
	Connections int     `json:"connections"`
}

// ProcInfo 进程信息
type ProcInfo struct {
	PID    int     `json:"pid"`
	Name   string  `json:"name"`
	CPU    float64 `json:"cpu_percent"`
	Memory float64 `json:"memory_mb"`
	Uptime string  `json:"uptime"`
	Status string  `json:"status"`
}

// SvcStatus 服务状态
type SvcStatus struct {
	Name      string `json:"name"`
	Status    string `json:"status"`
	PID       int    `json:"pid,omitempty"`
	Port      int    `json:"port,omitempty"`
	Uptime    string `json:"uptime,omitempty"`
	LastError string `json:"last_error,omitempty"`
}

// ─── Fault Record ───

// FaultRecord 故障记录——持久化到 Nebula Memory
type FaultRecord struct {
	ID         string           `json:"id"`
	Title      string           `json:"title"`
	Summary    string           `json:"summary"`
	RootCause  string           `json:"root_cause"`
	Solution   string           `json:"solution"`
	Severity   string           `json:"severity"`
	Tags       []string         `json:"tags"`
	ServerName string           `json:"server_name"`
	LogSnippet string           `json:"log_snippet,omitempty"`
	MetricsAt  *MetricsSnapshot `json:"metrics_at,omitempty"`
	OccurredAt time.Time        `json:"occurred_at"`
	ResolvedAt time.Time        `json:"resolved_at,omitempty"`
	Resolved   bool             `json:"resolved"`
}

// ─── Log Entry ───

// LogEntry 解析后的日志条目
type LogEntry struct {
	Timestamp time.Time         `json:"timestamp"`
	Level     string            `json:"level"`
	Source    string            `json:"source"`
	Message   string            `json:"message"`
	Raw       string            `json:"raw"`
	Fields    map[string]string `json:"fields,omitempty"`
}

// AnomalyReport 异常报告
type AnomalyReport struct {
	LogEntries  []LogEntry `json:"log_entries"`
	Pattern     string     `json:"pattern"`
	Count       int        `json:"count"`
	TimeSpan    TimeRange  `json:"time_span"`
	Severity    string     `json:"severity"`
	Summary     string     `json:"summary"`
	SuggestedBy string     `json:"suggested_by"`
}

// ─── Serialization ───

func ToJSON(v any) string {
	b, _ := json.MarshalIndent(v, "", "  ")
	return string(b)
}
