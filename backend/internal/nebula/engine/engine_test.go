package engine

import (
	"sync"
	"testing"

	"github.com/noosphere/backend/internal/nebula/engine/embed"
)

// Regression: Engine.Embed used to read e.embedder without holding the lock,
// racing with SetEmbedder swapping the embedder. Run under -race.
func TestEmbedConcurrentWithSetEmbedder(t *testing.T) {
	e, err := Open(DefaultOptions(t.TempDir()))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer e.Close()

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := 0; i < 500; i++ {
			if _, err := e.Embed("concurrent embed probe text"); err != nil {
				t.Errorf("embed: %v", err)
				return
			}
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < 10; i++ {
			e.SetEmbedder(embed.NewMockEmbedder(128))
		}
	}()
	wg.Wait()
}

// Concurrent Put / SearchScoped / Stats on one engine exercises the full lock
// chain (engine -> lsm, engine -> hnsw) under -race.
func TestConcurrentPutSearchStats(t *testing.T) {
	e, err := Open(DefaultOptions(t.TempDir()))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer e.Close()

	var wg sync.WaitGroup
	for w := 0; w < 4; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for i := 0; i < 50; i++ {
				key := "session-x:mem"
				if err := e.PutText(key, []byte("value"), "gateway retry and vector index content"); err != nil {
					t.Errorf("put: %v", err)
					return
				}
				if _, err := e.SearchScoped("gateway retry", 5, "session-x:"); err != nil {
					t.Errorf("search: %v", err)
					return
				}
				e.Stats()
			}
		}(w)
	}
	wg.Wait()
}
