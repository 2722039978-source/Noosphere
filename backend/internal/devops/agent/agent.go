package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/noosphere/backend/internal/devops/metrics"
	"github.com/noosphere/backend/internal/devops/tools"
	"github.com/noosphere/backend/internal/nebula/engine"
)

type AgentState string

const (
	StateRunning AgentState = "running"
	StateError   AgentState = "error"
)

type ToolDef struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Category    string `json:"category"`
}

type ChatFunc func(ctx context.Context, messages []map[string]string) (string, error)

type FaultRecord struct {
	ID        string                  `json:"id"`
	Issue     string                  `json:"issue"`
	Diagnosis string                  `json:"diagnosis"`
	Metrics   *metrics.SystemSnapshot `json:"metrics"`
	Timestamp string                  `json:"timestamp"`
}

type Agent struct {
	mu        sync.RWMutex
	state     AgentState
	collector *metrics.Collector
	tools     map[string]ToolDef
	executors map[string]func(ctx context.Context, args map[string]any) tools.ToolResult
	engine    *engine.Engine
	chatFn    ChatFunc
}

func New(eng *engine.Engine, chat ChatFunc) *Agent {
	a := &Agent{state: StateRunning, collector: metrics.NewCollector(), engine: eng, chatFn: chat,
		tools: make(map[string]ToolDef), executors: make(map[string]func(ctx context.Context, args map[string]any) tools.ToolResult)}
	a.registerTools()
	log.Printf("[devops] agent initialized, %d tools", len(a.tools))
	return a
}

func (a *Agent) State() AgentState                          { a.mu.RLock(); defer a.mu.RUnlock(); return a.state }
func (a *Agent) Snapshot() (*metrics.SystemSnapshot, error) { return a.collector.Snapshot() }

func (a *Agent) ListTools() []ToolDef {
	a.mu.RLock()
	defer a.mu.RUnlock()
	list := make([]ToolDef, 0, len(a.tools))
	for _, t := range a.tools {
		list = append(list, t)
	}
	return list
}

func (a *Agent) ExecuteTool(name string, args map[string]any) tools.ToolResult {
	return a.ExecuteToolContext(context.Background(), name, args)
}
func (a *Agent) ExecuteToolContext(ctx context.Context, name string, args map[string]any) (result tools.ToolResult) {
	if err := ctx.Err(); err != nil {
		return tools.ToolResult{Error: err.Error()}
	}
	if err := validateToolArgs(name, args); err != nil {
		return tools.ToolResult{Error: err.Error()}
	}
	defer func() {
		if r := recover(); r != nil {
			result = tools.ToolResult{Success: false, Error: fmt.Sprintf("panic: %v", r)}
		}
	}()
	a.mu.RLock()
	exec, ok := a.executors[name]
	a.mu.RUnlock()
	if !ok {
		return tools.ToolResult{Success: false, Error: fmt.Sprintf("tool not found: %s", name)}
	}
	result = exec(ctx, args)
	log.Printf("[devops] tool %s: success=%v", name, result.Success)
	return result
}

func (a *Agent) Diagnose(issue string) map[string]interface{} {
	snap, _ := a.collector.Snapshot()
	toolResults := make(map[string]tools.ToolResult)
	toolResults["tasklist"] = safeExec(tools.ExecTaskList)
	toolResults["netstat"] = safeExec(tools.ExecNetStat)
	toolResults["tail_log"] = safeExec(func() tools.ToolResult { return tools.ExecTailLog("", 15) })
	result := map[string]interface{}{"issue": issue, "metrics": snap, "tools": toolResults, "diagnosed_at": snap.Timestamp}
	// AI diagnosis
	if a.chatFn != nil {
		aiDiag := a.aiDiagnose(issue, snap, toolResults)
		result["ai_diagnosis"] = aiDiag
	}
	return result
}

func (a *Agent) aiDiagnose(issue string, snap *metrics.SystemSnapshot, toolResults map[string]tools.ToolResult) string {
	// Search past faults
	pastFaults := ""
	if a.engine != nil {
		results, _ := a.engine.Search("fault:"+issue, 3)
		for _, r := range results {
			if v, err := a.engine.Get(r.Key); err == nil {
				var fr FaultRecord
				if json.Unmarshal(v, &fr) == nil {
					pastFaults += fmt.Sprintf("Past: %s → %s\n", fr.Issue, fr.Diagnosis)
				}
			}
		}
	}
	prompt := fmt.Sprintf(`You are a DevOps AI agent. Analyze this system issue:
Issue: %s
CPU: %.1f%% (%d cores) | Memory: %.1f%% (%.1f/%.1f GB) | Disk: %.1f%% | Processes: %d
Past similar faults: %s
Give a concise diagnosis (cause, impact, solution) in 2-3 sentences.`, issue, snap.CPU.UsagePercent, snap.CPU.Cores,
		snap.Memory.UsagePercent, snap.Memory.UsedGB, snap.Memory.TotalGB, snap.Disk.UsagePercent, snap.Processes, pastFaults)
	msgs := []map[string]string{{"role": "user", "content": prompt}}
	resp, err := a.chatFn(context.Background(), msgs)
	if err != nil {
		return fmt.Sprintf("AI diagnosis unavailable: %v", err)
	}
	// Store fault
	if a.engine != nil {
		fr := FaultRecord{ID: fmt.Sprintf("fault-%d", time.Now().UnixNano()), Issue: issue, Diagnosis: resp, Metrics: snap, Timestamp: time.Now().UTC().Format(time.RFC3339)}
		data, _ := json.Marshal(fr)
		a.engine.Put("fault:"+fr.ID, data)
	}
	return resp
}

func safeExec(fn func() tools.ToolResult) (result tools.ToolResult) {
	defer func() {
		if r := recover(); r != nil {
			result = tools.ToolResult{Success: false, Error: fmt.Sprintf("panic: %v", r)}
		}
	}()
	return fn()
}

func (a *Agent) registerTools() {
	a.register("system_info", "System information", "system", func(ctx context.Context, args map[string]any) tools.ToolResult {
		return tools.ExecSystemInfoContext(ctx)
	})
	a.register("tasklist", "List running processes (top 20)", "system", func(ctx context.Context, args map[string]any) tools.ToolResult { return tools.ExecTaskListContext(ctx) })
	a.register("netstat", "Network connections", "system", func(ctx context.Context, args map[string]any) tools.ToolResult { return tools.ExecNetStatContext(ctx) })
	a.register("tail_log", "Read last N lines of log", "log", func(ctx context.Context, args map[string]any) tools.ToolResult {
		path, _ := args["path"].(string)
		lines := 20
		if n, ok := args["lines"].(float64); ok {
			lines = int(n)
		}
		return tools.ExecTailLogContext(ctx, path, lines)
	})
	a.register("search_log", "Search log for keyword", "log", func(ctx context.Context, args map[string]any) tools.ToolResult {
		path, _ := args["path"].(string)
		keyword, _ := args["keyword"].(string)
		return tools.ExecSearchLogContext(ctx, path, keyword)
	})
	a.register("service_list", "List Windows services", "service", func(ctx context.Context, args map[string]any) tools.ToolResult {
		return tools.ExecServiceListContext(ctx)
	})
	a.register("service_status", "Check service status", "service", func(ctx context.Context, args map[string]any) tools.ToolResult {
		name, _ := args["name"].(string)
		return tools.ExecServiceStatusContext(ctx, name)
	})
}

func (a *Agent) register(name, desc, category string, exec func(context.Context, map[string]any) tools.ToolResult) {
	a.tools[name] = ToolDef{Name: name, Description: desc, Category: category}
	a.executors[name] = exec
}
