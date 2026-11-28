package renderpack

import (
	"context"
	"encoding/json"
	"net/http"
	"time"
)

func RegisterRoutes(mux *http.ServeMux, dataDir string) {
	mux.HandleFunc("GET /api/agent/render-pack", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"packs": Status(dataDir)})
	})
	mux.HandleFunc("POST /api/agent/render-pack", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Kind string `json:"kind"`
		}
		d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024))
		d.DisallowUnknownFields()
		if err := d.Decode(&body); err != nil {
			http.Error(w, "无效排版器请求", 400)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 15*time.Minute)
		defer cancel()
		if e := Install(ctx, dataDir, body.Kind); e != nil {
			http.Error(w, e.Error(), 409)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"packs": Status(dataDir)})
	})
}
