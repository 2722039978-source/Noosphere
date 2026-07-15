package agent

import (
	"fmt"
	"sync"
)

// ToolFunc 工具执行函数签名
type ToolFunc func(args map[string]any) (*ToolResult, error)

// ToolRegistry 工具注册中心——管理所有可用的 DevOps 工具
type ToolRegistry struct {
	mu    sync.RWMutex
	tools map[string]*ToolEntry
}

// ToolEntry 工具注册条目
type ToolEntry struct {
	Def ToolDef
	Fn  ToolFunc
}

// NewToolRegistry 创建工具注册中心
func NewToolRegistry() *ToolRegistry {
	return &ToolRegistry{
		tools: make(map[string]*ToolEntry),
	}
}

// Register 注册一个工具
func (r *ToolRegistry) Register(def ToolDef, fn ToolFunc) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.tools[def.Name]; exists {
		return fmt.Errorf("tool %q already registered", def.Name)
	}
	r.tools[def.Name] = &ToolEntry{Def: def, Fn: fn}
	return nil
}

// Get 获取工具
func (r *ToolRegistry) Get(name string) (*ToolEntry, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	entry, ok := r.tools[name]
	if !ok {
		return nil, fmt.Errorf("tool %q not found", name)
	}
	return entry, nil
}

// Execute 执行工具调用
func (r *ToolRegistry) Execute(call ToolCall) (*ToolResult, error) {
	entry, err := r.Get(call.Name)
	if err != nil {
		return &ToolResult{CallID: call.ID, Success: false, Error: err.Error()}, err
	}
	return entry.Fn(call.Args)
}

// ListDefs 列出所有工具定义
func (r *ToolRegistry) ListDefs() []ToolDef {
	r.mu.RLock()
	defer r.mu.RUnlock()
	defs := make([]ToolDef, 0, len(r.tools))
	for _, entry := range r.tools {
		defs = append(defs, entry.Def)
	}
	return defs
}

// ListByCategory 按类别列出
func (r *ToolRegistry) ListByCategory(cat ToolCategory) []ToolDef {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var defs []ToolDef
	for _, entry := range r.tools {
		if entry.Def.Category == cat {
			defs = append(defs, entry.Def)
		}
	}
	return defs
}

// Count 工具数量
func (r *ToolRegistry) Count() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.tools)
}
