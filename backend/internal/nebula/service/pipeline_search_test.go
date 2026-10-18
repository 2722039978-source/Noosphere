package service

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/noosphere/backend/internal/nebula/engine"
)

func TestPipelineSearchScopedToSession(t *testing.T) {
	eng, err := engine.Open(engine.DefaultOptions(t.TempDir()))
	if err != nil {
		t.Fatalf("engine open: %v", err)
	}
	defer eng.Close()

	p := NewPipeline(eng)

	sessionA := "session-a"
	sessionB := "session-b"

	// Ingest overlapping content into both sessions
	entries := []struct {
		session string
		content string
	}{
		{sessionA, "The gateway handler processes chat requests and model configuration"},
		{sessionA, "Nebula memory engine stores vectors and BM25 inverted index"},
		{sessionB, "The gateway handler processes chat requests and model configuration"},
		{sessionB, "DevOps agent collects system metrics and diagnoses issues"},
	}

	for _, e := range entries {
		_, err := p.Ingest(PipelineInput{
			SessionID: e.session,
			Content:   e.content,
			Tags:      []string{"test"},
		})
		if err != nil {
			t.Fatalf("ingest %s: %v", e.session, err)
		}
		time.Sleep(time.Millisecond) // ensure unique nanosecond memory IDs
	}

	// Search scoped to session-a
	resultsA, err := p.Search(SearchRequest{
		SessionID: sessionA,
		Query:     "gateway chat handler",
		TopK:      10,
	})
	if err != nil {
		t.Fatalf("search A: %v", err)
	}
	if len(resultsA) == 0 {
		t.Fatal("expected results for session-a, got none")
	}
	for _, r := range resultsA {
		if r.Record.SessionID != sessionA {
			t.Errorf("expected session-a, got %s", r.Record.SessionID)
		}
	}

	// Search scoped to session-b
	resultsB, err := p.Search(SearchRequest{
		SessionID: sessionB,
		Query:     "gateway chat handler",
		TopK:      10,
	})
	if err != nil {
		t.Fatalf("search B: %v", err)
	}
	if len(resultsB) == 0 {
		t.Fatal("expected results for session-b, got none")
	}
	for _, r := range resultsB {
		if r.Record.SessionID != sessionB {
			t.Errorf("expected session-b, got %s", r.Record.SessionID)
		}
	}

	// Verify the two sessions return distinct result sets
	if resultsA[0].Record.SessionID == resultsB[0].Record.SessionID {
		t.Error("scoped search should return different sessions")
	}
}

func TestPipelineSearchEmptySession(t *testing.T) {
	eng, err := engine.Open(engine.DefaultOptions(t.TempDir()))
	if err != nil {
		t.Fatalf("engine open: %v", err)
	}
	defer eng.Close()

	p := NewPipeline(eng)

	// Ingest into session-a only
	_, err = p.Ingest(PipelineInput{
		SessionID: "session-a",
		Content:   "The gateway handler processes chat requests",
		Tags:      []string{"test"},
	})
	if err != nil {
		t.Fatalf("ingest: %v", err)
	}

	// Search a different (empty) session — should return no results
	results, err := p.Search(SearchRequest{
		SessionID: "session-empty",
		Query:     "gateway chat",
		TopK:      10,
	})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("expected 0 results for empty session, got %d", len(results))
	}
}

// Stress: concurrent Ingest + Search + Stats through the pipeline — the real
// API usage pattern (Nebula store + search + CodeLens writeback). Run under
// -race to validate the engine/lsm/hnsw lock chain.
func TestPipelineConcurrentIngestSearch(t *testing.T) {
	eng, err := engine.Open(engine.DefaultOptions(t.TempDir()))
	if err != nil {
		t.Fatalf("engine open: %v", err)
	}
	defer eng.Close()

	p := NewPipeline(eng)

	var wg sync.WaitGroup
	for w := 0; w < 6; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			sess := fmt.Sprintf("session-%d", w%3)
			for i := 0; i < 30; i++ {
				if _, err := p.Ingest(PipelineInput{
					SessionID: sess,
					Content:   fmt.Sprintf("worker %d memory %d: gateway retry mechanism and vector index details", w, i),
					Tags:      []string{"race"},
				}); err != nil {
					t.Errorf("ingest: %v", err)
					return
				}
				if _, err := p.Search(SearchRequest{
					SessionID: sess,
					Query:     "gateway retry",
					TopK:      5,
				}); err != nil {
					t.Errorf("search: %v", err)
					return
				}
				eng.Stats()
			}
		}(w)
	}
	wg.Wait()

	// All ingested memories must be retrievable after the storm.
	results, err := p.Search(SearchRequest{
		SessionID: "session-0",
		Query:     "gateway retry mechanism",
		TopK:      10,
	})
	if err != nil {
		t.Fatalf("final search: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected hits in session-0 after concurrent ingest")
	}
}
