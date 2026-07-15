// Package analyzer — 日志异常分析与故障诊断
//
// 双层分析架构：
//   - 规则引擎：快速筛查 12 种常见异常模式（OOM、磁盘满、连接拒绝等）
//   - LLM 深度分析：结合系统指标进行根因推理和修复建议
package analyzer

import (
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/devops-agent/agent"
)

// LogAnalyzer 日志分析器
type LogAnalyzer struct {
	mu    sync.RWMutex
	rules []AnomalyRule
}

// AnomalyRule 异常检测规则
type AnomalyRule struct {
	Name        string
	Pattern     *regexp.Regexp
	Severity    string
	Category    string
	Description string
}

// NewLogAnalyzer 创建日志分析器，预置 12 条运维常用异常规则
func NewLogAnalyzer() *LogAnalyzer {
	a := &LogAnalyzer{}
	a.registerDefaultRules()
	return a
}

func (a *LogAnalyzer) registerDefaultRules() {
	a.rules = []AnomalyRule{
		{
			Name: "OOM_Killer", Pattern: regexp.MustCompile(`(?i)(out of memory|OOM killer|memory exhausted|killed process)`),
			Severity: "critical", Category: "memory", Description: "内存耗尽，触发 OOM Killer——进程被强制终止",
		},
		{
			Name: "DiskFull", Pattern: regexp.MustCompile(`(?i)(no space left on device|disk full|ENOSPC|insufficient disk space)`),
			Severity: "critical", Category: "disk", Description: "磁盘空间不足——写入操作失败",
		},
		{
			Name: "ConnectionRefused", Pattern: regexp.MustCompile(`(?i)(connection refused|connect:.*refused|ECONNREFUSED)`),
			Severity: "high", Category: "network", Description: "连接被拒绝——目标服务未运行或端口未开放",
		},
		{
			Name: "ConnectionTimeout", Pattern: regexp.MustCompile(`(?i)(connection timed out|timeout|ETIMEDOUT|dial tcp.*timeout)`),
			Severity: "high", Category: "network", Description: "连接超时——网络延迟过高或目标服务无响应",
		},
		{
			Name: "DNSError", Pattern: regexp.MustCompile(`(?i)(name resolution|DNS.*fail|no such host|NXDOMAIN|could not resolve host)`),
			Severity: "high", Category: "network", Description: "DNS 解析失败——域名无法解析",
		},
		{
			Name: "ServiceCrash", Pattern: regexp.MustCompile(`(?i)(fatal error|segfault|segmentation fault|SIGSEGV|panic:|runtime error)`),
			Severity: "critical", Category: "crash", Description: "服务崩溃——程序异常终止",
		},
		{
			Name: "HighCPU", Pattern: regexp.MustCompile(`(?i)(CPU.*(100%|9[5-9]%)|high CPU usage|cpu throttl)`),
			Severity: "high", Category: "cpu", Description: "CPU 使用率过高——性能瓶颈",
		},
		{
			Name: "DBConnectionFail", Pattern: regexp.MustCompile(`(?i)(database.*(refused|timeout|fail|error)|too many connections|connection pool.*exhausted|SQLSTATE)`),
			Severity: "critical", Category: "service", Description: "数据库连接失败——后端服务不可达",
		},
		{
			Name: "TLSCertError", Pattern: regexp.MustCompile(`(?i)(certificate.*expired|TLS.*error|x509:|SSL.*error|untrusted certificate)`),
			Severity: "high", Category: "security", Description: "TLS/SSL 证书错误——安全连接失败",
		},
		{
			Name: "PermissionDenied", Pattern: regexp.MustCompile(`(?i)(permission denied|access denied|EACCES|unauthorized|forbidden)`),
			Severity: "medium", Category: "security", Description: "权限不足——文件/资源访问被拒绝",
		},
		{
			Name: "RateLimit", Pattern: regexp.MustCompile(`(?i)(rate limit.*exceeded|too many requests|429|throttl|quota exceeded)`),
			Severity: "medium", Category: "service", Description: "速率限制——API 调用频率超限",
		},
		{
			Name: "FileDescriptorExhaustion", Pattern: regexp.MustCompile(`(?i)(too many open files|EMFILE|ENFILE|file descriptor|ulimit)`),
			Severity: "critical", Category: "service", Description: "文件描述符耗尽——无法打开新文件/连接",
		},
	}
}

// ScanLogs 扫描日志，检测异常模式
func (a *LogAnalyzer) ScanLogs(entries []agent.LogEntry) []agent.AnomalyReport {
	a.mu.RLock()
	defer a.mu.RUnlock()

	var reports []agent.AnomalyReport
	for _, rule := range a.rules {
		var matched []agent.LogEntry
		for _, entry := range entries {
			if rule.Pattern.MatchString(entry.Message) || rule.Pattern.MatchString(entry.Raw) {
				matched = append(matched, entry)
			}
		}
		if len(matched) == 0 {
			continue
		}
		report := agent.AnomalyReport{
			LogEntries: matched, Pattern: rule.Name, Count: len(matched),
			Severity: rule.Severity, Summary: rule.Description, SuggestedBy: "rule",
		}
		if len(matched) > 0 {
			report.TimeSpan = agent.TimeRange{From: matched[0].Timestamp, To: matched[len(matched)-1].Timestamp}
		}
		reports = append(reports, report)
	}
	return reports
}

// ScanRawLogs 扫描原始日志文本（适用于未解析为 LogEntry 的日志）
func (a *LogAnalyzer) ScanRawLogs(rawLines []string) []agent.AnomalyReport {
	entries := make([]agent.LogEntry, len(rawLines))
	for i, line := range rawLines {
		entries[i] = agent.LogEntry{
			Timestamp: time.Now(),
			Message:   line,
			Raw:       line,
		}
	}
	return a.ScanLogs(entries)
}

// BuildDiagnosisFromReports 根据异常报告和系统指标生成诊断
func (a *LogAnalyzer) BuildDiagnosisFromReports(reports []agent.AnomalyReport, metrics *agent.MetricsSnapshot) string {
	var b strings.Builder
	b.WriteString("## 故障诊断报告\n\n")
	b.WriteString(fmt.Sprintf("> 生成时间: %s\n\n", time.Now().Format("2006-01-02 15:04:05")))

	if metrics != nil {
		b.WriteString("### 📊 当前系统指标\n")
		b.WriteString(fmt.Sprintf("| 指标 | 数值 | 状态 |\n"))
		b.WriteString(fmt.Sprintf("|------|------|------|\n"))
		cpuStatus := "✅ 正常"
		if metrics.CPU.UsagePercent > 90 {
			cpuStatus = "🔴 异常"
		} else if metrics.CPU.UsagePercent > 70 {
			cpuStatus = "🟡 偏高"
		}
		b.WriteString(fmt.Sprintf("| CPU | %.1f%% (%d核) | %s |\n", metrics.CPU.UsagePercent, metrics.CPU.Cores, cpuStatus))

		memStatus := "✅ 正常"
		if metrics.Memory.UsagePercent > 90 {
			memStatus = "🔴 异常"
		} else if metrics.Memory.UsagePercent > 70 {
			memStatus = "🟡 偏高"
		}
		b.WriteString(fmt.Sprintf("| 内存 | %.1f%% (%.1f/%.1f GB) | %s |\n", metrics.Memory.UsagePercent, metrics.Memory.UsedGB, metrics.Memory.TotalGB, memStatus))

		diskStatus := "✅ 正常"
		if metrics.Disk.UsagePercent > 90 {
			diskStatus = "🔴 异常"
		} else if metrics.Disk.UsagePercent > 70 {
			diskStatus = "🟡 偏高"
		}
		b.WriteString(fmt.Sprintf("| 磁盘 | %.1f%% (%.1f/%.1f GB) | %s |\n", metrics.Disk.UsagePercent, metrics.Disk.UsedGB, metrics.Disk.TotalGB, diskStatus))
		b.WriteString("\n")
	}

	if len(reports) == 0 {
		b.WriteString("### ✅ 未检测到已知异常模式\n")
		b.WriteString("日志中未匹配到常见异常特征。建议检查系统指标是否正常。\n")
		return b.String()
	}

	b.WriteString(fmt.Sprintf("### 🔍 检测到 %d 个异常模式\n\n", len(reports)))
	for i, r := range reports {
		icon := "🔴"
		switch r.Severity {
		case "high":
			icon = "🟠"
		case "medium":
			icon = "🟡"
		case "low":
			icon = "🟢"
		}
		b.WriteString(fmt.Sprintf("#### %d. %s [%s] %s\n", i+1, icon, r.Severity, r.Pattern))
		b.WriteString(fmt.Sprintf("- **描述**: %s\n", r.Summary))
		b.WriteString(fmt.Sprintf("- **匹配数**: %d 条\n", r.Count))
		if len(r.LogEntries) > 0 {
			b.WriteString(fmt.Sprintf("- **示例日志**: `%s`\n", truncateStr(r.LogEntries[0].Raw, 150)))
		}

		// 针对性修复建议
		suggestions := getFixSuggestions(r.Pattern)
		if len(suggestions) > 0 {
			b.WriteString("- **修复建议**:\n")
			for _, s := range suggestions {
				b.WriteString(fmt.Sprintf("  - %s\n", s))
			}
		}
		b.WriteString("\n")
	}

	return b.String()
}

// AddRule 添加自定义异常规则
func (a *LogAnalyzer) AddRule(rule AnomalyRule) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.rules = append(a.rules, rule)
}

// ─── 辅助 ───

func truncateStr(s string, maxLen int) string {
	runes := []rune(s)
	if len(runes) <= maxLen {
		return s
	}
	return string(runes[:maxLen]) + "..."
}

func getFixSuggestions(pattern string) []string {
	switch pattern {
	case "OOM_Killer":
		return []string{"检查内存使用最高的进程，使用 top/任务管理器查看", "分析是否存在内存泄漏，使用 profiling 工具定位", "考虑增加物理内存或配置 swap 空间"}
	case "DiskFull":
		return []string{"清理临时文件和旧日志：`Get-ChildItem $env:TEMP -Recurse` 查看占用", "检查日志轮转配置，设置合理的保留策略", "扩展磁盘空间或迁移数据到其他分区"}
	case "ConnectionRefused":
		return []string{"确认目标服务是否正在运行：`Get-Service <服务名>`", "检查防火墙规则：`Get-NetFirewallRule`", "验证端口是否在监听：`Get-NetTCPConnection -LocalPort <端口>`"}
	case "ConnectionTimeout":
		return []string{"检查网络连通性：`Test-NetConnection <host> -Port <port>`", "排查防火墙/安全组规则是否阻止连接", "检查目标服务的连接数是否达到上限"}
	case "ServiceCrash":
		return []string{"检查 Windows 事件查看器 (eventvwr) 中的应用程序日志", "查看崩溃转储文件分析原因", "检查崩溃前的应用日志，定位触发条件"}
	case "DBConnectionFail":
		return []string{"检查数据库连接池配置：最大连接数、超时时间", "验证数据库服务状态：`Get-Service *sql*`", "检查网络连通性和防火墙规则"}
	case "FileDescriptorExhaustion":
		return []string{"检查进程打开的文件句柄数量", "增加 ulimit 限制或优化连接复用", "检查是否有连接泄漏（CLOSE_WAIT 状态堆积）"}
	default:
		return nil
	}
}
