// Protocol fixture for the desktop lifecycle tests. No model inference or downloads.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"github.com/noosphere/backend/internal/ownedprocess"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

func main() {
	ownedprocess.RunSupervisor()
	if len(os.Args) == 2 {
		switch os.Args[1] {
		case "--version":
			fmt.Println("version: fixture")
			return
		case "--list-devices":
			fmt.Println("Available devices:")
			return
		}
	}
	host := flag.String("host", "", "")
	port := flag.String("port", "", "")
	alias := flag.String("alias", "", "")
	weight := flag.String("m", "", "")
	slots := flag.Int("np", 0, "")
	flag.Int("c", 0, "")
	flag.Int("ngl", 0, "")
	flag.Parse()
	if *host != "127.0.0.1" || *slots != 1 || *alias == "" {
		log.Fatal("invalid local launch parameters")
	}
	if _, err := os.Stat(*weight); err != nil {
		log.Fatal(err)
	}
	switch filepath.Base(*weight) {
	case "broken.gguf":
		log.Fatal("model load failed")
	case "slow.gguf":
		time.Sleep(90 * time.Second)
	}
	http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, `{"status":"ok"}`) })
	http.HandleFunc("/props", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"modalities":{"vision":false},"build_info":"fixture"}`)
	})
	http.HandleFunc("/v1/models", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"data": []map[string]string{{"id": *alias}}})
	})
	http.HandleFunc("/v1/chat/completions", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Model  string
			Stream bool
		}
		if json.NewDecoder(r.Body).Decode(&req) != nil || req.Model != *alias || !req.Stream {
			http.Error(w, "invalid request", 400)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: "+`{"choices":[{"delta":{"content":"fixture"}}]}`+"\n\ndata: [DONE]\n\n")
	})
	http.HandleFunc("/fixture-exit", func(w http.ResponseWriter, r *http.Request) { os.Exit(7) })
	log.Fatal(http.ListenAndServe(net.JoinHostPort(*host, *port), nil))
}
