// Package service — Memory Service 层
//
// 在 Engine 之上提供：
//   - 多租户/会话管理
//   - 上下文窗口管理
//   - 统一的同步/异步操作接口
package service

import (
	"fmt"
	"sync"

	"github.com/nebula-agent/nebula/engine"
	"github.com/nebula-agent/nebula/engine/manager"
)

// Service Memory Service
type Service struct {
	mu      sync.RWMutex
	engine  *engine.Engine
	sessions map[string]*engine.Session
	config   *Config
}

// Config Service 配置
type Config struct {
	MaxSessions       int
	MaxMemoriesPerSession int
}

// DefaultConfig 默认服务配置
func DefaultConfig() *Config {
	return &Config{
		MaxSessions:          1000,
		MaxMemoriesPerSession: 100000,
	}
}

// New 创建 Memory Service
func New(eng *engine.Engine, cfg *Config) *Service {
	if cfg == nil {
		cfg = DefaultConfig()
	}
	return &Service{
		engine:   eng,
		sessions: make(map[string]*engine.Session),
		config:   cfg,
	}
}

// GetSession 获取或创建会话
func (s *Service) GetSession(id string) *engine.Session {
	s.mu.Lock()
	defer s.mu.Unlock()

	if sess, ok := s.sessions[id]; ok {
		return sess
	}

	sess := s.engine.Session(id)
	s.sessions[id] = sess
	return sess
}

// DeleteSession 删除会话及其所有记忆
func (s *Service) DeleteSession(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	sess, ok := s.sessions[id]
	if !ok {
		return fmt.Errorf("session not found: %s", id)
	}

	if err := sess.Clear(); err != nil {
		return err
	}

	delete(s.sessions, id)
	return nil
}

// ListSessions 列出所有会话
func (s *Service) ListSessions() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	ids := make([]string, 0, len(s.sessions))
	for id := range s.sessions {
		ids = append(ids, id)
	}
	return ids
}

// Stats 获取服务统计
func (s *Service) Stats() *ServiceStats {
	engStats := s.engine.Stats()

	s.mu.RLock()
	defer s.mu.RUnlock()

	return &ServiceStats{
		SessionCount: len(s.sessions),
		EngineStats:  engStats,
	}
}

// ServiceStats 服务统计
type ServiceStats struct {
	SessionCount int            `json:"session_count"`
	EngineStats  *manager.Stats `json:"engine_stats"`
}

// Engine 返回底层引擎（高级用法）
func (s *Service) Engine() *engine.Engine {
	return s.engine
}
