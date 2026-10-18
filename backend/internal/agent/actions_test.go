package agent

import (
	"context"
	"encoding/json"
	"github.com/noosphere/backend/internal/workspaces"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func actionFixture(t *testing.T) (*Actions, *workspaces.Manager, workspaces.Workspace, string) {
	t.Helper()
	base := t.TempDir()
	m, err := workspaces.Open(filepath.Join(base, "workspace.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { m.Close() })
	root := filepath.Join(base, "project")
	os.Mkdir(root, 0700)
	os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\n// before\n"), 0600)
	ws, err := m.SaveWorkspace(workspaces.Workspace{Name: "fixture", ProjectPath: root})
	if err != nil {
		t.Fatal(err)
	}
	store, err := OpenActions(filepath.Join(base, "actions"), m, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(store.Close)
	return store, m, ws, base
}

func TestRequiredReplacementAndInterruptedFileRecovery(t *testing.T) {
	s, _, ws, base := actionFixture(t)
	for _, raw := range []string{`{"path":"main.go","old_text":"// before"}`, `{"path":"main.go","old_text":"// before","new_text":null}`} {
		if _, err := s.Propose(context.Background(), ws, "run", "propose_file_change", json.RawMessage(raw)); err == nil {
			t.Fatal("missing replacement accepted")
		}
	}
	a, err := s.Propose(context.Background(), ws, "run", "propose_file_change", json.RawMessage(`{"path":"new.go","new_text":"package main"}`))
	if err != nil {
		t.Fatal(err)
	}
	a.Status = "applying"
	s.save(a)
	os.WriteFile(filepath.Join(ws.ProjectPath, a.Path), []byte(a.After), 0600)
	recovered, err := OpenActions(filepath.Join(base, "actions"), s.manager, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer recovered.Close()
	if recovered.List(ws.ID, "run")[0].Status != "interrupted" {
		t.Fatal("uncertain action replayed")
	}
	if _, err = recovered.Decide(a.ID, ws.ID, "approve"); err == nil {
		t.Fatal("uncertain action approved twice")
	}
	if _, err = recovered.Decide(a.ID, ws.ID, "rollback"); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(filepath.Join(ws.ProjectPath, a.Path)); !os.IsNotExist(err) {
		t.Fatal("created file not removed on rollback")
	}
}

type fixtureExecutor struct {
	mu    sync.Mutex
	state string
	count int
	hold  bool
}

func (e *fixtureExecutor) ServiceState(context.Context, string) (string, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.state, nil
}
func (e *fixtureExecutor) Execute(ctx context.Context, a Action) (string, error) {
	e.mu.Lock()
	e.count++
	hold := e.hold
	if a.Kind == "service" {
		e.state = a.Target
	}
	e.mu.Unlock()
	if hold {
		<-ctx.Done()
		return "partial output", ctx.Err()
	}
	return "fixture passed", nil
}
func waitAction(t *testing.T, s *Actions, ws, run string) Action {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		a := s.List(ws, run)[0]
		if a.Status != "running" {
			return a
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("action timeout")
	return Action{}
}
func TestApprovalIsSingleUseAndTestsCancel(t *testing.T) {
	s, _, ws, _ := actionFixture(t)
	e := &fixtureExecutor{hold: true}
	s.executor = e
	a, err := s.Propose(context.Background(), ws, "run", "propose_test", json.RawMessage(`{"runner":"go"}`))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Decide(a.ID, "foreign", "approve"); err == nil {
		t.Fatal("foreign approval accepted")
	}
	if _, err = s.Decide(a.ID, ws.ID, "approve"); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Decide(a.ID, ws.ID, "approve"); err == nil {
		t.Fatal("duplicate execution accepted")
	}
	if _, err = s.Decide(a.ID, ws.ID, "cancel"); err != nil {
		t.Fatal(err)
	}
	done := waitAction(t, s, ws.ID, "run")
	if done.Status != "failed" || !strings.Contains(done.Error, "canceled") {
		t.Fatalf("lost cancellation: %+v", done)
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.count != 1 {
		t.Fatal("wrong execution count")
	}
}
func TestServiceApprovalChecksSnapshotAndRestores(t *testing.T) {
	s, _, ws, _ := actionFixture(t)
	e := &fixtureExecutor{state: "stopped"}
	s.executor = e
	a, err := s.Propose(context.Background(), ws, "run", "propose_service_change", json.RawMessage(`{"name":"FixtureService","target":"running"}`))
	if err != nil {
		t.Fatal(err)
	}
	e.state = "running"
	if _, err = s.Decide(a.ID, ws.ID, "approve"); err == nil {
		t.Fatal("changed service accepted")
	}
	e.state = "stopped"
	if _, err = s.Decide(a.ID, ws.ID, "approve"); err != nil {
		t.Fatal(err)
	}
	if waitAction(t, s, ws.ID, "run").Status != "applied" {
		t.Fatal("service not applied")
	}
	if _, err = s.Decide(a.ID, ws.ID, "rollback"); err != nil {
		t.Fatal(err)
	}
	if waitAction(t, s, ws.ID, "run").Status != "rolled_back" {
		t.Fatal("service not restored")
	}
	state, _ := e.ServiceState(context.Background(), a.Service)
	if state != "stopped" {
		t.Fatal("wrong restored state")
	}
}
func TestReviewedFileProposalApplyConflictRollbackAndRestart(t *testing.T) {
	s, _, ws, base := actionFixture(t)
	a, err := s.Propose(context.Background(), ws, "run-fixture", "propose_file_change", json.RawMessage(`{"path":"main.go","old_text":"// before","new_text":"// after","reason":"update comment"}`))
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(filepath.Join(ws.ProjectPath, "main.go"))
	if string(raw) != "package main\n// before\n" {
		t.Fatal("proposal mutated file")
	}
	if _, err = s.Decide(a.ID, ws.ID, "approve"); err != nil {
		t.Fatal(err)
	}
	raw, _ = os.ReadFile(filepath.Join(ws.ProjectPath, "main.go"))
	if string(raw) != "package main\n// after\n" {
		t.Fatal("approval did not apply")
	}
	if _, err = s.Decide(a.ID, ws.ID, "approve"); err == nil {
		t.Fatal("duplicate approved")
	}
	os.WriteFile(filepath.Join(ws.ProjectPath, "main.go"), []byte("external edit"), 0600)
	if _, err = s.Decide(a.ID, ws.ID, "rollback"); err == nil {
		t.Fatal("rollback overwrote external edit")
	}
	os.WriteFile(filepath.Join(ws.ProjectPath, "main.go"), []byte("package main\n// after\n"), 0600)
	recovered, err := OpenActions(filepath.Join(base, "actions"), s.manager, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer recovered.Close()
	if _, err = recovered.Decide(a.ID, ws.ID, "rollback"); err != nil {
		t.Fatal(err)
	}
	raw, _ = os.ReadFile(filepath.Join(ws.ProjectPath, "main.go"))
	if string(raw) != "package main\n// before\n" {
		t.Fatal("rollback failed")
	}
}

func TestAllowedMarkupFileJournalCanReopen(t *testing.T) {
	s, _, ws, base := actionFixture(t)
	text := strings.Repeat("<&>", 20000) + "before"
	os.WriteFile(filepath.Join(ws.ProjectPath, "markup.html"), []byte(text), 0600)
	a, err := s.Propose(context.Background(), ws, "run", "propose_file_change", json.RawMessage(`{"path":"markup.html","old_text":"before","new_text":"after"}`))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Decide(a.ID, ws.ID, "approve"); err != nil {
		t.Fatal(err)
	}
	reopened, err := OpenActions(filepath.Join(base, "actions"), s.manager, nil)
	if err != nil {
		t.Fatal("accepted proposal could not reopen", err)
	}
	defer reopened.Close()
	if len(reopened.List(ws.ID, "run")) != 1 {
		t.Fatal("proposal disappeared")
	}
	if _, err = reopened.Decide(a.ID, ws.ID, "rollback"); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(ws.ProjectPath, "markup.html"))
	if err != nil || string(raw) != text {
		t.Fatal("reopened rollback did not restore markup", err)
	}
}

func TestOversizedEncodedActionRejectedBeforeSave(t *testing.T) {
	s, _, ws, base := actionFixture(t)
	input, err := json.Marshal(map[string]string{"path": "controls.txt", "old_text": "before", "new_text": "after"})
	if err != nil {
		t.Fatal(err)
	}
	before := strings.Repeat("\x01", 60000) + "before"
	if err = os.WriteFile(filepath.Join(ws.ProjectPath, "controls.txt"), []byte(before), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Propose(context.Background(), ws, "run", "propose_file_change", input); err == nil {
		t.Fatal("oversized encoded action accepted")
	}
	entries, err := os.ReadDir(filepath.Join(base, "actions"))
	if err != nil || len(entries) != 0 || len(s.List(ws.ID, "run")) != 0 {
		t.Fatal("rejected proposal left a journal", err)
	}
}

func TestJournalReservesSpaceForInterruptedRecovery(t *testing.T) {
	s, _, _, base := actionFixture(t)
	tooLarge := Action{ID: "no-recovery-room", Status: "pending", Before: strings.Repeat("x", maxActionJournalBytes-1024)}
	if err := s.save(tooLarge); err == nil {
		t.Fatal("pending record accepted without space for recovery metadata")
	}
	a := Action{ID: "near-limit", Status: "pending", Before: strings.Repeat("x", maxActionJournalBytes-8*1024-1024)}
	if err := s.save(a); err != nil {
		t.Fatal(err)
	}
	a.Status = "applying"
	if err := s.save(a); err != nil {
		t.Fatal(err)
	}
	reopened, err := OpenActions(filepath.Join(base, "actions"), s.manager, nil)
	if err != nil {
		t.Fatal("recovery metadata exceeded journal limit", err)
	}
	defer reopened.Close()
	if got := reopened.items[a.ID]; got.Status != "interrupted" || got.Error == "" {
		t.Fatal("interrupted action not recovered", got.Status)
	}
}
func TestProposalsRejectTraversalSecretsAndProjectChange(t *testing.T) {
	s, m, ws, _ := actionFixture(t)
	for _, input := range []string{`{"path":"../outside.go","new_text":"x"}`, `{"path":"models.yaml","new_text":"x"}`, `{"path":"new.go","new_text":"api_key=private-value"}`, `{"path":"main.go","old_text":"missing","new_text":"x"}`} {
		if _, err := s.Propose(context.Background(), ws, "run", "propose_file_change", json.RawMessage(input)); err == nil {
			t.Fatal("bad proposal accepted", input)
		}
	}
	a, err := s.Propose(context.Background(), ws, "run", "propose_file_change", json.RawMessage(`{"path":"new.go","new_text":"package main"}`))
	if err != nil {
		t.Fatal(err)
	}
	ws.ProjectPath = t.TempDir()
	m.SaveWorkspace(ws)
	if _, err = s.Decide(a.ID, ws.ID, "approve"); err == nil {
		t.Fatal("rebound project allowed")
	}
}
