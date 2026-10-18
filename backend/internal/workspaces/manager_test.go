package workspaces

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func testManager(t *testing.T) *Manager {
	t.Helper()
	m, e := Open(filepath.Join(t.TempDir(), "workspace.sqlite"))
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { m.Close() })
	return m
}
func waitRun(t *testing.T, m *Manager, id, status string) Run {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		r, e := m.Run(id)
		if e != nil {
			t.Fatal(e)
		}
		if r.Status == status {
			return r
		}
		time.Sleep(5 * time.Millisecond)
	}
	r, _ := m.Run(id)
	t.Fatalf("run %s status %s, expected %s", id, r.Status, status)
	return Run{}
}

// Removing durable writes or startup recovery must lose the persisted result or leave unfinished work running.
func TestDurableResultsAndRestartRecovery(t *testing.T) {
	p := filepath.Join(t.TempDir(), "workspace.sqlite")
	m, e := Open(p)
	if e != nil {
		t.Fatal(e)
	}
	_, ex, e := m.Start(context.Background(), Run{ID: "done", Kind: "chat"})
	if e != nil {
		t.Fatal(e)
	}
	ex.Emit("thinking", "checkpoint", map[string]string{"preview": "hello"})
	if e = ex.Finish(map[string]string{"answer": "saved"}, nil); e != nil {
		t.Fatal(e)
	}
	_, pending, e := m.Start(context.Background(), Run{ID: "unfinished", Kind: "chat"})
	if e != nil {
		t.Fatal(e)
	}
	// A separate database open models restart recovery before the old process can finish.
	if e = m.Close(); e != nil {
		t.Fatal(e)
	}
	if e = pending.Finish("late", nil); !errors.Is(e, ErrClosed) {
		t.Fatalf("late finish: %v", e)
	}
	m, e = Open(p)
	if e != nil {
		t.Fatal(e)
	}
	defer m.Close()
	r, e := m.Run("done")
	if e != nil || r.Status != "completed" || string(r.Result) != `{"answer":"saved"}` {
		t.Fatalf("result: %+v %v", r, e)
	}
	r, e = m.Run("unfinished")
	if e != nil || r.Status != "interrupted" {
		t.Fatalf("recovery: %+v %v", r, e)
	}
	ev, e := m.Events("done", 0)
	if e != nil || len(ev) < 3 {
		t.Fatalf("events: %+v %v", ev, e)
	}
	for i := 1; i < len(ev); i++ {
		if ev[i].Seq <= ev[i-1].Seq {
			t.Fatal("non-monotonic events")
		}
	}
}

// A late success must never turn a cancelled run into a completed run or replace its result.
func TestCancelAndParentContextProtectTerminalState(t *testing.T) {
	m := testManager(t)
	parent, cancel := context.WithCancel(context.Background())
	ctx, ex, e := m.Start(parent, Run{ID: "cancelled", Kind: "chat"})
	if e != nil {
		t.Fatal(e)
	}
	cancel()
	select {
	case <-ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("context not cancelled")
	}
	waitRun(t, m, ex.ID, "cancelled")
	if e = ex.Finish("late", nil); e != nil {
		t.Fatal(e)
	}
	ex.Emit("late", "late", nil)
	r, _ := m.Run(ex.ID)
	if r.Status != "cancelled" || string(r.Result) != "null" {
		t.Fatalf("late write: %+v", r)
	}
	if _, _, e = m.Start(context.Background(), Run{ID: ex.ID, Kind: "chat"}); !errors.Is(e, ErrConflict) {
		t.Fatalf("duplicate accepted: %v", e)
	}
}

// Changing roots must invalidate old project bindings, and results stay in the submitting workspace.
func TestWorkspaceIsolationAndRootCAS(t *testing.T) {
	m := testManager(t)
	root := t.TempDir()
	ws, e := m.SaveWorkspace(Workspace{Name: "one", ProjectPath: root})
	if e != nil {
		t.Fatal(e)
	}
	if e = m.BindProject(ws.ID, root, "project-one"); e != nil {
		t.Fatal(e)
	}
	if _, e = m.SaveWorkspace(Workspace{Name: "duplicate", ProjectPath: root}); !errors.Is(e, ErrConflict) {
		t.Fatalf("duplicate root: %v", e)
	}
	ws.ProjectPath = t.TempDir()
	ws.ProjectID = "forged"
	ws, e = m.SaveWorkspace(ws)
	if e != nil || ws.ProjectID != "" {
		t.Fatalf("root reset: %+v %v", ws, e)
	}
	if e = m.BindProject(ws.ID, root, "stale"); !errors.Is(e, ErrConflict) {
		t.Fatalf("stale bind: %v", e)
	}
	_, ex, e := m.Start(context.Background(), Run{WorkspaceID: ws.ID, Kind: "chat"})
	if e != nil {
		t.Fatal(e)
	}
	ex.Finish("one", nil)
	local, _ := m.Runs("local")
	runs, _ := m.Runs(ws.ID)
	if len(local) != 0 || len(runs) != 1 {
		t.Fatalf("cross-workspace results: %d %d", len(local), len(runs))
	}
	if _, e = m.SaveWorkspace(Workspace{Name: "bad", ProjectPath: filepath.Join(root, "missing")}); !errors.Is(e, ErrInvalid) {
		t.Fatalf("invalid root: %v", e)
	}
}

// Saturating background work must leave interactive capacity, and rejected queue entries must not persist.
func TestBoundedSchedulerAndQueuedCancellation(t *testing.T) {
	m := testManager(t)
	started := make(chan struct{}, 1)
	release := make(chan struct{})
	defer close(release)
	m.Register("index", "background", func(ctx context.Context, ws Workspace, in json.RawMessage, emit func(string, string, any)) (any, error) {
		started <- struct{}{}
		select {
		case <-release:
		case <-ctx.Done():
		}
		return "indexed", nil
	})
	m.Register("diagnosis", "interactive", func(ctx context.Context, ws Workspace, in json.RawMessage, emit func(string, string, any)) (any, error) {
		return "ready", nil
	})
	first, e := m.Submit("local", "index", "first", nil)
	if e != nil {
		t.Fatal(e)
	}
	<-started
	var queued []Run
	for i := 0; i < 32; i++ {
		r, e := m.Submit("local", "index", "queued", nil)
		if e != nil {
			t.Fatal(e)
		}
		queued = append(queued, r)
	}
	if _, e = m.Submit("local", "index", "overflow", nil); !errors.Is(e, ErrBusy) {
		t.Fatalf("unbounded queue: %v", e)
	}
	diag, e := m.Submit("local", "diagnosis", "quick", nil)
	if e != nil {
		t.Fatal(e)
	}
	waitRun(t, m, diag.ID, "completed")
	if _, e = m.Cancel(queued[0].ID); e != nil {
		t.Fatal(e)
	}
	if _, e = m.Cancel(first.ID); e != nil {
		t.Fatal(e)
	}
	waitRun(t, m, queued[0].ID, "cancelled")
	runs, _ := m.Runs("local")
	if len(runs) != 34 {
		t.Fatalf("rejected run persisted: %d", len(runs))
	}
}

func TestExternalAdmissionAndResultBounds(t *testing.T) {
	m := testManager(t)
	_, a, e := m.Start(context.Background(), Run{Kind: "chat"})
	if e != nil {
		t.Fatal(e)
	}
	_, b, e := m.Start(context.Background(), Run{Kind: "chat"})
	if e != nil {
		t.Fatal(e)
	}
	if _, _, e = m.Start(context.Background(), Run{Kind: "chat"}); !errors.Is(e, ErrBusy) {
		t.Fatalf("unbounded external admission: %v", e)
	}
	if e = a.Finish(strings.Repeat("x", 2*1024*1024), nil); !errors.Is(e, ErrInvalid) {
		t.Fatalf("oversized result accepted: %v", e)
	}
	waitRun(t, m, a.ID, "failed")
	b.Finish(nil, nil)
	_, c, e := m.Start(context.Background(), Run{Kind: "chat"})
	if e != nil {
		t.Fatal(e)
	}
	c.Finish(nil, nil)
}

// Busy diagnostic workers must not reject a user's independently bounded chat request.
func TestChatHasReservedAdmission(t *testing.T) {
	m := testManager(t)
	started := make(chan struct{}, 2)
	release := make(chan struct{})
	defer close(release)
	m.Register("diagnosis", "interactive", func(ctx context.Context, _ Workspace, _ json.RawMessage, _ func(string, string, any)) (any, error) {
		started <- struct{}{}
		select {
		case <-release:
		case <-ctx.Done():
		}
		return nil, nil
	})
	for i := 0; i < 2; i++ {
		if _, e := m.Submit("local", "diagnosis", "busy", nil); e != nil {
			t.Fatal(e)
		}
	}
	for i := 0; i < 2; i++ {
		select {
		case <-started:
		case <-time.After(time.Second):
			t.Fatal("worker not started")
		}
	}
	_, ex, e := m.Start(context.Background(), Run{Kind: "chat"})
	if e != nil {
		t.Fatalf("diagnosis blocks chat: %v", e)
	}
	ex.Finish(nil, nil)
}

// Recovery must happen on Open even when graceful Close never ran.
func TestAbruptRestartRecoversWithoutReplay(t *testing.T) {
	p := filepath.Join(t.TempDir(), "workspace.sqlite")
	m, e := Open(p)
	if e != nil {
		t.Fatal(e)
	}
	_, ex, e := m.Start(context.Background(), Run{Kind: "chat"})
	if e != nil {
		t.Fatal(e)
	}
	m.mu.Lock()
	m.closed = true
	m.cancel()
	e = m.db.Close()
	m.mu.Unlock()
	if e != nil {
		t.Fatal(e)
	}
	replacement, e := Open(p)
	if e != nil {
		t.Fatal(e)
	}
	defer replacement.Close()
	r, e := replacement.Run(ex.ID)
	if e != nil || r.Status != "interrupted" {
		t.Fatalf("startup recovery: %+v %v", r, e)
	}
	events, e := replacement.Events(ex.ID, 0)
	if e != nil || len(events) != 2 || events[1].Phase != "interrupted" {
		t.Fatalf("recovery event: %+v %v", events, e)
	}
	ex.Finish(nil, nil)
}

func TestTaskListsOmitHeavyResultsButDetailsKeepThem(t *testing.T) {
	m := testManager(t)
	_, ex, e := m.Start(context.Background(), Run{Kind: "chat"})
	if e != nil {
		t.Fatal(e)
	}
	result := strings.Repeat("large answer", 10000)
	if e = ex.Finish(result, nil); e != nil {
		t.Fatal(e)
	}
	runs, e := m.Runs("local")
	if e != nil || len(runs) != 1 {
		t.Fatalf("list: %v %v", runs, e)
	}
	if len(runs[0].Result) > 4 {
		t.Fatal("task polling transfers the entire answer")
	}
	detail, e := m.Run(ex.ID)
	if e != nil || len(detail.Result) < len(result) {
		t.Fatal("detail lost persisted answer")
	}
}

func TestDeadlineIsFailureNotUserCancellation(t *testing.T) {
	m := testManager(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	_, ex, e := m.Start(ctx, Run{Kind: "chat"})
	if e != nil {
		t.Fatal(e)
	}
	<-ctx.Done()
	r := waitRun(t, m, ex.ID, "failed")
	if !strings.Contains(r.Error, "deadline") {
		t.Fatalf("missing timeout reason: %+v", r)
	}
	ex.Finish(nil, ctx.Err())
	m.Register("index", "background", func(context.Context, Workspace, json.RawMessage, func(string, string, any)) (any, error) {
		return nil, context.DeadlineExceeded
	})
	queued, e := m.Submit("local", "index", "timeout", nil)
	if e != nil {
		t.Fatal(e)
	}
	waitRun(t, m, queued.ID, "failed")
}
