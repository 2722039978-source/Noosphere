// Package modelops controls only persisted synthetic workflow releases. It has
// no gateway, provider, Actions or memory-writing capability.
package modelops

import "github.com/noosphere/backend/internal/modelstore"

type Command string

const (
	StartOffline Command = "start_offline"
	Advance      Command = "advance"
	Pause        Command = "pause"
	Resume       Command = "resume"
	Reject       Command = "reject"
	Rollback     Command = "rollback"
	Workflow             = "codelens.codetask.go.v1"
	TaskType             = "codetask_go"
	PolicyID             = "simulation-demo-v1"
)

// Next accepts commands, never a caller-selected destination state.
func Next(r modelstore.Release, c Command) (string, error) {
	if r.ClosedAt != "" {
		return "", modelstore.ErrInvalid
	}
	switch c {
	case StartOffline:
		if r.State == "DRAFT" {
			return "OFFLINE_EVAL", nil
		}
	case Advance:
		next := map[string]string{"OFFLINE_EVAL": "SHADOW", "SHADOW": "CANARY_5", "CANARY_5": "CANARY_25", "CANARY_25": "CANARY_50", "CANARY_50": "PROMOTED"}
		if n := next[r.State]; n != "" {
			return n, nil
		}
	case Pause:
		if r.State == "PROMOTED" && r.PreviousStable != "" {
			return "ROLLED_BACK", nil
		}
		if activeStage(r.State) {
			return "PAUSED", nil
		}
	case Resume:
		if r.State == "PAUSED" && activeStage(r.PausedFrom) {
			return r.PausedFrom, nil
		}
	case Reject:
		if r.State == "DRAFT" || r.State == "OFFLINE_EVAL" || r.State == "SHADOW" || (r.State == "PAUSED" && (r.PausedFrom == "OFFLINE_EVAL" || r.PausedFrom == "SHADOW")) {
			return "REJECTED", nil
		}
	case Rollback:
		state := r.State
		if state == "PAUSED" {
			state = r.PausedFrom
		}
		if r.PreviousStable != "" && (state == "CANARY_5" || state == "CANARY_25" || state == "CANARY_50" || state == "PROMOTED") {
			return "ROLLED_BACK", nil
		}
	}
	return "", modelstore.ErrInvalid
}
func activeStage(s string) bool {
	return s == "OFFLINE_EVAL" || s == "SHADOW" || s == "CANARY_5" || s == "CANARY_25" || s == "CANARY_50"
}
