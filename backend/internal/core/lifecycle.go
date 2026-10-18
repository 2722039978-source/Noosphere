package core

import (
	"encoding/json"
	"log"
	"sync"
)

type ModuleState int

const (
	StateStopped  ModuleState = iota
	StateStarting
	StateRunning
	StateError
)

func (s ModuleState) String() string {
	switch s {
	case StateStopped:
		return "stopped"
	case StateStarting:
		return "starting"
	case StateRunning:
		return "running"
	case StateError:
		return "error"
	default:
		return "unknown"
	}
}

func (s ModuleState) MarshalJSON() ([]byte, error) {
	return json.Marshal(s.String())
}

type ModuleInfo struct {
	Name  string      `json:"name"`
	State ModuleState `json:"state"`
	Err   string      `json:"error,omitempty"`
}

type Lifecycle struct {
	mu      sync.RWMutex
	modules map[string]*ModuleInfo
}

func New() *Lifecycle {
	return &Lifecycle{
		modules: map[string]*ModuleInfo{
			"gateway":  {Name: "gateway", State: StateStopped},
			"nebula":   {Name: "nebula", State: StateStopped},
			"devops":   {Name: "devops", State: StateStopped},
			"codelens": {Name: "codelens", State: StateStopped},
		},
	}
}

func (l *Lifecycle) SetState(name string, state ModuleState) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if m, ok := l.modules[name]; ok {
		m.State = state
		m.Err = ""
	} else {
		l.modules[name] = &ModuleInfo{Name: name, State: state}
	}
	log.Printf("[core] %s -> %s", name, state)
}

func (l *Lifecycle) SetError(name string, err error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if m, ok := l.modules[name]; ok {
		m.State = StateError
		m.Err = err.Error()
	} else {
		l.modules[name] = &ModuleInfo{Name: name, State: StateError, Err: err.Error()}
	}
	log.Printf("[core] %s -> error: %v", name, err)
}

func (l *Lifecycle) Snapshot() map[string]*ModuleInfo {
	l.mu.RLock()
	defer l.mu.RUnlock()
	out := make(map[string]*ModuleInfo, len(l.modules))
	for k, v := range l.modules {
		copy := *v
		out[k] = &copy
	}
	return out
}

func (l *Lifecycle) AllRunning() bool {
	l.mu.RLock()
	defer l.mu.RUnlock()
	for _, m := range l.modules {
		if m.State != StateRunning {
			return false
		}
	}
	return true
}
