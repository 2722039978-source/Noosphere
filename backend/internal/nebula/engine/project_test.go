package engine

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"
)

func TestCancelWhileWaitingToPublishKeepsOldSnapshot(t *testing.T) {
	e, err := Open(DefaultOptions(t.TempDir()))
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	if err := e.ReplacePrefix("code-a:", map[string]TextRecord{"code-a:one": {Value: []byte("old"), Text: "same"}}); err != nil {
		t.Fatal(err)
	}
	e.mu.RLock()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := e.ReplacePrefixContext(ctx, "code-a:", map[string]TextRecord{"code-a:one": {Value: []byte("new"), Text: "same"}})
		done <- err
	}()
	deadline := time.Now().Add(time.Second)
	waiting := false
	for time.Now().Before(deadline) {
		if !e.mu.TryRLock() {
			waiting = true
			break
		}
		e.mu.RUnlock()
		time.Sleep(time.Millisecond)
	}
	cancel()
	e.mu.RUnlock()
	err = <-done
	if !waiting {
		t.Fatal("did not reach publication lock")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled publication succeeded: %v", err)
	}
	if got, _ := e.Get("code-a:one"); string(got) != "old" {
		t.Fatal("cancelled result replaced previous snapshot")
	}
}

type publicationEmbedder struct {
	cancel context.CancelFunc
	bad    bool
}

func (p publicationEmbedder) Dim() int { return 128 }
func (p publicationEmbedder) Embed(string) ([]float32, error) {
	if p.cancel != nil {
		p.cancel()
	}
	if p.bad {
		return nil, errors.New("embedding failed")
	}
	return make([]float32, 128), nil
}

func TestFailedAndMidEmbeddingCancelledPublicationPreservesSnapshot(t *testing.T) {
	e, err := Open(DefaultOptions(t.TempDir()))
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	old := map[string]TextRecord{"code-a:one": {Value: []byte("old"), Text: "old function"}}
	if err := e.ReplacePrefix("code-a:", old); err != nil {
		t.Fatal(err)
	}
	for _, fail := range []bool{true, false} {
		ctx, cancel := context.WithCancel(context.Background())
		e.embedder = publicationEmbedder{cancel: cancel, bad: fail}
		_, err := e.ReplacePrefixContext(ctx, "code-a:", map[string]TextRecord{"code-a:two": {Value: []byte("new"), Text: "new function"}})
		cancel()
		if err == nil {
			t.Fatal("failure published")
		}
		if got, _ := e.Get("code-a:one"); string(got) != "old" {
			t.Fatal("previous snapshot lost")
		}
		if _, err := e.Get("code-a:two"); err == nil {
			t.Fatal("partial write")
		}
	}
}

func BenchmarkProjectIncremental(b *testing.B) {
	for _, size := range []int{100, 1000} {
		for _, change := range []bool{false, true} {
			b.Run(fmt.Sprintf("chunks_%d/change_%t", size, change), func(b *testing.B) {
				e, err := Open(DefaultOptions(b.TempDir()))
				if err != nil {
					b.Fatal(err)
				}
				defer e.Close()
				records := map[string]TextRecord{}
				for i := 0; i < size; i++ {
					text := fmt.Sprintf("function item_%d return shared_module value", i)
					records[fmt.Sprintf("code-b:%d", i)] = TextRecord{Value: []byte(text), Text: text}
				}
				if err := e.ReplacePrefix("code-b:", records); err != nil {
					b.Fatal(err)
				}
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					if change {
						text := fmt.Sprintf("function updated_%d return shared_module", i)
						records["code-b:0"] = TextRecord{Value: []byte(text), Text: text}
					}
					if _, err := e.ReplacePrefixContext(context.Background(), "code-b:", records); err != nil {
						b.Fatal(err)
					}
				}
			})
		}
	}
}

func TestIncrementalPublicationSkipsUnchangedGraphAndCancelsAtomically(t *testing.T) {
	e, err := Open(DefaultOptions(t.TempDir()))
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	records := map[string]TextRecord{"code-a:one": {Value: []byte("v1"), Text: "function compute"}}
	first, err := e.ReplacePrefixContext(context.Background(), "code-a:", records)
	if err != nil || first.EmbeddedChunks != 1 || !first.GraphRebuilt {
		t.Fatalf("first: %+v %v", first, err)
	}
	graph := e.hnsw
	same, err := e.ReplacePrefixContext(context.Background(), "code-a:", records)
	if err != nil || same.UpdatedRecords != 0 || same.ReusedRecords != 1 || e.hnsw != graph {
		t.Fatalf("noop: %+v %v", same, err)
	}
	records["code-a:one"] = TextRecord{Value: []byte("v2"), Text: "function compute"}
	meta, err := e.ReplacePrefixContext(context.Background(), "code-a:", records)
	if err != nil || meta.UpdatedRecords != 1 || meta.EmbeddedChunks != 0 || e.hnsw != graph {
		t.Fatalf("metadata: %+v %v", meta, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = e.ReplacePrefixContext(ctx, "code-a:", map[string]TextRecord{})
	if err == nil {
		t.Fatal("cancelled publication succeeded")
	}
	if got, _ := e.Get("code-a:one"); string(got) != "v2" {
		t.Fatal("cancel changed snapshot")
	}
}

func TestReplaceProjectIsIsolatedAndSurvivesRestart(t *testing.T) {
	dir := t.TempDir()
	e, err := Open(DefaultOptions(dir))
	if err != nil {
		t.Fatal(err)
	}
	if err = e.ReplacePrefix("code-a:", map[string]TextRecord{"code-a:one": {Value: []byte("old"), Text: "obsolete unique"}}); err != nil {
		t.Fatal(err)
	}
	e.PutText("code-b:one", []byte("other"), "other project")
	if err = e.ReplacePrefix("code-a:", map[string]TextRecord{"code-a:two": {Value: []byte("new"), Text: "current source"}}); err != nil {
		t.Fatal(err)
	}
	if _, err = e.Get("code-a:one"); err == nil {
		t.Fatal("stale source survived reindex")
	}
	if _, err = e.Get("code-b:one"); err != nil {
		t.Fatal("other project removed")
	}
	if err = e.ReplacePrefix("code-a:", map[string]TextRecord{"code-b:evil": {Value: []byte("bad"), Text: "bad"}}); err == nil {
		t.Fatal("cross-project write accepted")
	}
	e.Close()
	e, err = Open(DefaultOptions(dir))
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	if len(e.Records("code-a:")) != 1 {
		t.Fatal("project snapshot not restored")
	}
	if got, _ := e.Get("code-a:two"); string(got) != "new" {
		t.Fatal("restored wrong source")
	}
}
