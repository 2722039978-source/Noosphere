package workspaces

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func receive[T any](t *testing.T, ch <-chan T) T {
	t.Helper()
	select {
	case value := <-ch:
		return value
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for deterministic task barrier")
		var zero T
		return zero
	}
}

func TestReferencedRunHistoryAndContext(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tasks.sqlite")
	m, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	reference := TaskReference{ID: "eval-one", Generation: 7}
	seen := make(chan Cancellation, 1)
	m.RegisterReferenced("evaluation", "background", func(ctx context.Context, ws Workspace, input json.RawMessage, _ func(string, string, any)) (any, error) {
		seen <- Cancellation{RunID: RunID(ctx), WorkspaceID: ws.ID, Kind: "evaluation", Reference: Reference(ctx)}
		return map[string]string{"report_id": "report-one"}, nil
	}, func(context.Context, Cancellation) error { return nil })
	r, err := m.SubmitReferenced("local", "evaluation", "compare", reference, json.RawMessage(`{"evaluation_id":"eval-one"}`))
	if err != nil {
		t.Fatal(err)
	}
	got := receive(t, seen)
	if got.RunID != r.ID || got.Reference != reference || got.WorkspaceID != "local" {
		t.Fatalf("context: %+v", got)
	}
	waitRun(t, m, r.ID, "completed")
	if _, err = m.Submit("local", "evaluation", "untyped", nil); !errors.Is(err, ErrInvalid) {
		t.Fatalf("generic evaluation submission: %v", err)
	}
	if err = m.Close(); err != nil {
		t.Fatal(err)
	}
	m, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	detail, err := m.Run(r.ID)
	if err != nil || detail.ReferenceID != reference.ID || detail.ReferenceGeneration != reference.Generation {
		t.Fatalf("history: %+v %v", detail, err)
	}
	var legacy Run
	if err = json.Unmarshal([]byte(`{"id":"old","kind":"chat"}`), &legacy); err != nil || legacy.ReferenceID != "" || legacy.ReferenceGeneration != 0 {
		t.Fatalf("legacy: %+v %v", legacy, err)
	}
}

func TestTaskCenterCancelsQueuedEvaluationAndPersistsIntent(t *testing.T) {
	m := testManager(t)
	blocker := make(chan struct{})
	started := make(chan struct{}, 1)
	m.Register("index", "background", func(ctx context.Context, _ Workspace, _ json.RawMessage, _ func(string, string, any)) (any, error) {
		started <- struct{}{}
		select {
		case <-blocker:
		case <-ctx.Done():
		}
		return nil, nil
	})
	_, err := m.Submit("local", "index", "block", nil)
	if err != nil {
		t.Fatal(err)
	}
	receive(t, started)
	var ran atomic.Bool
	var persisted atomic.Bool
	m.RegisterReferenced("evaluation", "background", func(context.Context, Workspace, json.RawMessage, func(string, string, any)) (any, error) {
		ran.Store(true)
		return nil, nil
	}, func(ctx context.Context, c Cancellation) error {
		r, err := m.Run(c.RunID) // external callback must be able to reenter Manager.
		if err != nil || r.Status != "queued" || c.Reference != (TaskReference{ID: "eval", Generation: 3}) {
			return errors.New("cancel hook saw invalid snapshot")
		}
		persisted.Store(true)
		return nil
	})
	r, err := m.SubmitReferenced("local", "evaluation", "queued", TaskReference{ID: "eval", Generation: 3}, nil)
	if err != nil {
		t.Fatal(err)
	}
	// The queued cancellation must not depend on its execution handler remaining installed.
	m.mu.Lock()
	reg := m.registrations["evaluation"]
	reg.handler = nil
	m.registrations["evaluation"] = reg
	m.mu.Unlock()
	mux := http.NewServeMux()
	m.RegisterRoutes(mux)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest("POST", "/api/runs/"+r.ID+"/cancel", nil))
	if w.Code != 200 || !persisted.Load() || !strings.Contains(w.Body.String(), `"status":"cancelled"`) {
		t.Fatalf("cancel: %d %s", w.Code, w.Body.String())
	}
	close(blocker)
	// A following task proves the queue has actually skipped the cancelled handler.
	m.Register("index", "background", func(context.Context, Workspace, json.RawMessage, func(string, string, any)) (any, error) {
		return nil, nil
	})
	next, err := m.Submit("local", "index", "after", nil)
	if err != nil {
		t.Fatal(err)
	}
	waitRun(t, m, next.ID, "completed")
	if ran.Load() {
		t.Fatal("cancelled queued evaluation handler ran")
	}
}

func TestCancelPersistenceFailureIsNotSuccess(t *testing.T) {
	m := testManager(t)
	started := make(chan context.Context, 1)
	var fail atomic.Bool
	fail.Store(true)
	m.RegisterReferenced("evaluation", "background", func(ctx context.Context, _ Workspace, _ json.RawMessage, _ func(string, string, any)) (any, error) {
		started <- ctx
		<-ctx.Done()
		return nil, ctx.Err()
	}, func(context.Context, Cancellation) error {
		if fail.Load() {
			return errors.New("disk unavailable")
		}
		return nil
	})
	r, err := m.SubmitReferenced("local", "evaluation", "running", TaskReference{ID: "eval"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx := receive(t, started)
	mux := http.NewServeMux()
	m.RegisterRoutes(mux)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest("POST", "/api/runs/"+r.ID+"/cancel", nil))
	if w.Code < 500 {
		t.Fatalf("persistence failure acknowledged: %d %s", w.Code, w.Body.String())
	}
	current, err := m.Run(r.ID)
	if err != nil || current.Status != "running" || ctx.Err() != nil {
		t.Fatalf("failed cancel mutated task/context: %+v %v %v", current, err, ctx.Err())
	}
	m.mu.Lock()
	reg := m.registrations["evaluation"]
	hook := reg.cancel
	reg.cancel = nil
	m.registrations["evaluation"] = reg
	m.mu.Unlock()
	if _, err = m.Cancel(r.ID); !errors.Is(err, ErrCancellationUnavailable) {
		t.Fatalf("missing hook: %v", err)
	}
	m.mu.Lock()
	reg.cancel = hook
	m.registrations["evaluation"] = reg
	m.mu.Unlock()
	fail.Store(false)
	if _, err = m.Cancel(r.ID); err != nil {
		t.Fatal(err)
	}
	if ctx.Err() == nil {
		t.Fatal("committed cancellation did not cancel context")
	}
}

func TestPendingCancellationFencesWorkerAndLateFinish(t *testing.T) {
	m := testManager(t)
	hookStarted := make(chan Cancellation, 1)
	releaseHook := make(chan struct{})
	started := make(chan struct{}, 1)
	releaseHandler := make(chan struct{})
	m.RegisterReferenced("evaluation", "background", func(context.Context, Workspace, json.RawMessage, func(string, string, any)) (any, error) {
		started <- struct{}{}
		<-releaseHandler
		return "late success", nil
	}, func(_ context.Context, c Cancellation) error { hookStarted <- c; <-releaseHook; return nil })
	r, err := m.SubmitReferenced("local", "evaluation", "race", TaskReference{ID: "eval", Generation: 17}, nil)
	if err != nil {
		t.Fatal(err)
	}
	receive(t, started)
	cancelled := make(chan error, 1)
	go func() { _, err := m.Cancel(r.ID); cancelled <- err }()
	c := receive(t, hookStarted)
	if c.RunID != r.ID || c.Reference.Generation != 17 {
		t.Fatalf("mutable token: %+v", c)
	}
	close(releaseHandler)
	// Finish must await the pending hook; its late success cannot win.
	current, err := m.Run(r.ID)
	if err != nil || current.Status != "running" {
		t.Fatalf("pending state: %+v %v", current, err)
	}
	close(releaseHook)
	if err = receive(t, cancelled); err != nil {
		t.Fatal(err)
	}
	waitRun(t, m, r.ID, "cancelled")
}

func TestCloseWaitsOwnedWorkersAndCancelHooks(t *testing.T) {
	m := testManager(t)
	started := make(chan struct{}, 1)
	cancelled := make(chan struct{})
	cleanup := make(chan struct{})
	hookStarted := make(chan struct{})
	hookRelease := make(chan struct{})
	m.RegisterReferenced("evaluation", "background", func(ctx context.Context, _ Workspace, _ json.RawMessage, _ func(string, string, any)) (any, error) {
		started <- struct{}{}
		<-ctx.Done()
		close(cancelled)
		<-cleanup
		return nil, ctx.Err()
	}, func(context.Context, Cancellation) error { close(hookStarted); <-hookRelease; return nil })
	r, err := m.SubmitReferenced("local", "evaluation", "shutdown", TaskReference{ID: "eval"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	receive(t, started)
	cancelDone := make(chan error, 1)
	go func() { _, err := m.Cancel(r.ID); cancelDone <- err }()
	receive(t, hookStarted)
	closeDone := make(chan error, 2)
	go func() { closeDone <- m.Close() }()
	receive(t, cancelled)
	go func() { closeDone <- m.Close() }()
	select {
	case err := <-closeDone:
		t.Fatalf("Close returned before hook/handler cleanup: %v", err)
	default:
	}
	close(hookRelease)
	receive(t, cancelDone)
	select {
	case err := <-closeDone:
		t.Fatalf("Close returned before handler cleanup: %v", err)
	default:
	}
	close(cleanup)
	if err = receive(t, closeDone); err != nil {
		t.Fatal(err)
	}
	if err = receive(t, closeDone); err != nil {
		t.Fatal(err)
	}
}
