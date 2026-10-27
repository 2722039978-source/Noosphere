package modellab

import (
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"testing"
)

const nativeInvalidSource = "this is not Go source"

type nativeLifecycleFixture struct {
	mu       sync.Mutex
	resident string
	events   []string
	prompts  []string
}

// serve augments the existing identity HTTP fixture only when explicitly enabled.
// Formal keep_alive=0 deliberately leaves a delayed resident allocation so the
// real Gateway must exercise its owned unload and subsequent absence check.
func (f *nativeLifecycleFixture) serve(t *testing.T, w http.ResponseWriter, r *http.Request) bool {
	if r.URL.Path != "/api/ps" && r.URL.Path != "/api/generate" && r.URL.Path != "/api/chat" {
		return false
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if r.URL.Path == "/api/ps" {
		models := []map[string]any{}
		state := "empty"
		if f.resident != "" {
			state = f.resident
			models = append(models, map[string]any{"name": state, "model": state, "digest": strings.Repeat("a", 64), "size": 1024, "size_vram": 512})
		}
		f.events = append(f.events, "ps:"+state)
		json.NewEncoder(w).Encode(map[string]any{"models": models})
		return true
	}
	var req struct {
		Model     string                     `json:"model"`
		Prompt    string                     `json:"prompt"`
		Messages  []struct{ Content string } `json:"messages"`
		Stream    bool                       `json:"stream"`
		KeepAlive any                        `json:"keep_alive"`
		Options   map[string]any             `json:"options"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		t.Error(err)
		w.WriteHeader(http.StatusBadRequest)
		return true
	}
	if req.Model != "alpha:1" && req.Model != "beta:1" || req.Stream || req.Prompt != "" {
		t.Errorf("unexpected native request: %+v", req)
	}
	response := map[string]any{"model": req.Model, "done": true, "done_reason": "stop", "response": "", "total_duration": 100, "load_duration": 5, "prompt_eval_count": 2, "prompt_eval_duration": 10, "eval_count": 1, "eval_duration": 50}
	if r.URL.Path == "/api/generate" && req.KeepAlive == float64(0) {
		if f.resident != req.Model || len(req.Options) != 0 || len(req.Messages) != 0 {
			t.Errorf("unowned unload: resident=%q request=%+v", f.resident, req)
		}
		f.events = append(f.events, "release:"+req.Model)
		f.resident = ""
	} else {
		if req.Options["temperature"] != float64(0) || req.Options["seed"] != float64(7) || req.Options["num_ctx"] != float64(256) {
			t.Errorf("profile options lost across native boundary: %v", req.Options)
		}
		if r.URL.Path == "/api/generate" {
			if f.resident != "" || req.KeepAlive != "30s" || req.Options["num_predict"] != float64(1) || len(req.Messages) != 0 {
				t.Errorf("invalid warmup: resident=%q request=%+v", f.resident, req)
			}
			f.events = append(f.events, "warmup:"+req.Model)
			f.resident = req.Model
		} else {
			if f.resident != req.Model || req.KeepAlive != "0s" || req.Options["num_predict"] != float64(32) || len(req.Messages) != 1 {
				t.Errorf("formal call lacks owned warmup or frozen options: resident=%q request=%+v", f.resident, req)
			}
			f.events = append(f.events, "formal:"+req.Model)
			if len(req.Messages) == 1 {
				f.prompts = append(f.prompts, req.Messages[0].Content)
			}
			response["message"] = map[string]string{"role": "assistant", "content": nativeInvalidSource}
		}
	}
	json.NewEncoder(w).Encode(response)
	return true
}
