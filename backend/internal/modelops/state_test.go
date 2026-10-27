package modelops

import (
	"github.com/noosphere/backend/internal/modelstore"
	"testing"
)

func TestStateTransitionTable(t *testing.T) {
	states := []string{"DRAFT", "OFFLINE_EVAL", "SHADOW", "CANARY_5", "CANARY_25", "CANARY_50", "PROMOTED", "PAUSED", "REJECTED", "ROLLED_BACK"}
	legal := map[string]map[Command]string{
		"DRAFT":        {StartOffline: "OFFLINE_EVAL", Reject: "REJECTED"},
		"OFFLINE_EVAL": {Advance: "SHADOW", Pause: "PAUSED", Reject: "REJECTED"},
		"SHADOW":       {Advance: "CANARY_5", Pause: "PAUSED", Reject: "REJECTED"},
		"CANARY_5":     {Advance: "CANARY_25", Pause: "PAUSED", Rollback: "ROLLED_BACK"},
		"CANARY_25":    {Advance: "CANARY_50", Pause: "PAUSED", Rollback: "ROLLED_BACK"},
		"CANARY_50":    {Advance: "PROMOTED", Pause: "PAUSED", Rollback: "ROLLED_BACK"},
		"PROMOTED":     {Pause: "ROLLED_BACK", Rollback: "ROLLED_BACK"},
		"PAUSED":       {Resume: "CANARY_25", Rollback: "ROLLED_BACK"},
	}
	for _, state := range states {
		for _, cmd := range []Command{StartOffline, Advance, Pause, Resume, Reject, Rollback, "jump"} {
			r := modelstore.Release{State: state, PausedFrom: "CANARY_25", PreviousStable: "stable"}
			next, err := Next(r, cmd)
			want, ok := legal[state][cmd]
			if (err == nil) != ok || (ok && next != want) {
				t.Fatalf("%s/%s = %s,%v want %s", state, cmd, next, err, want)
			}
		}
	}
	for _, from := range states {
		r := modelstore.Release{State: "PAUSED", PausedFrom: from, PreviousStable: "s"}
		next, err := Next(r, Resume)
		valid := from == "OFFLINE_EVAL" || from == "SHADOW" || from == "CANARY_5" || from == "CANARY_25" || from == "CANARY_50"
		if (err == nil) != valid || (valid && next != from) {
			t.Fatalf("resume %s: %s %v", from, next, err)
		}
	}
}
