package agent

import (
	"encoding/json"
	"io"
	"net/http"
)

func (s *Actions) RegisterRoutes(mux *http.ServeMux) {
	respond := func(w http.ResponseWriter, code int, value any) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(code)
		json.NewEncoder(w).Encode(value)
	}
	mux.HandleFunc("GET /api/workspaces/{workspace}/runs/{run}/actions", func(w http.ResponseWriter, r *http.Request) {
		run, err := s.manager.Run(r.PathValue("run"))
		if err != nil || run.WorkspaceID != r.PathValue("workspace") {
			respond(w, 404, map[string]string{"error": "任务不存在或不属于当前工作区"})
			return
		}
		respond(w, 200, map[string]any{"actions": s.List(run.WorkspaceID, run.ID)})
	})
	mux.HandleFunc("POST /api/workspaces/{workspace}/actions/{id}", func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, 1024)
		var input struct {
			Decision string `json:"decision"`
		}
		d := json.NewDecoder(r.Body)
		d.DisallowUnknownFields()
		if d.Decode(&input) != nil || d.Decode(new(any)) != io.EOF {
			respond(w, 400, map[string]string{"error": "无效操作请求"})
			return
		}
		result, err := s.Decide(r.PathValue("id"), r.PathValue("workspace"), input.Decision)
		if err != nil {
			respond(w, 409, map[string]string{"error": err.Error()})
			return
		}
		respond(w, 200, result)
	})
}
