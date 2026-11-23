package agent

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/noosphere/backend/internal/workspaces"
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
	for _, id := range []string{"run", "run-fixture"} {
		_, execution, err := m.Start(context.Background(), workspaces.Run{ID: id, WorkspaceID: ws.ID, Kind: "agent"})
		if err != nil {
			t.Fatal(err)
		}
		if err := execution.Finish(nil, nil); err != nil {
			t.Fatal(err)
		}
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
	if err := os.WriteFile(filepath.Join(ws.ProjectPath, a.Path), []byte(a.After), 0600); err != nil {
		t.Fatal(err)
	}
	file, err := inspectFile(ws.ProjectPath, a.Path)
	if err != nil {
		t.Fatal(err)
	}
	file.parent.Close()
	a.AfterFileID = file.fileID
	if err := s.save(a); err != nil {
		t.Fatal(err)
	}
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
	mu      sync.Mutex
	state   string
	count   int
	entered chan struct{}
}

func (e *fixtureExecutor) ServiceState(context.Context, string) (string, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.state, nil
}
func (e *fixtureExecutor) Execute(ctx context.Context, a Action) (string, error) {
	e.mu.Lock()
	e.count++
	entered := e.entered
	if a.Kind == "service" {
		e.state = a.Target
	}
	e.mu.Unlock()
	if entered != nil {
		close(entered)
		<-ctx.Done()
		return "partial output", ctx.Err()
	}
	return "fixture passed", nil
}
func waitAction(t *testing.T, s *Actions, ws, run string) Action {
	t.Helper()
	return waitActionByID(t, s, ws, run, s.List(ws, run)[0].ID)
}

func waitActionByID(t *testing.T, s *Actions, ws, run, id string) Action {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		for _, a := range s.List(ws, run) {
			if a.ID == id && a.Status != "running" {
				return a
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("action timeout")
	return Action{}
}
func TestApprovalIsSingleUseAndTestsCancel(t *testing.T) {
	s, _, ws, _ := actionFixture(t)
	t.Setenv("NOOSPHERE_TEST_GO", os.Args[0])
	e := &fixtureExecutor{entered: make(chan struct{})}
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
	select {
	case <-e.entered:
	case <-time.After(3 * time.Second):
		t.Fatal("approved test did not start")
	}
	if _, err = s.Decide(a.ID, ws.ID, "cancel"); err != nil {
		t.Fatal(err)
	}
	done := waitAction(t, s, ws.ID, "run")
	if done.Status != "cancelled" || !strings.Contains(done.Error, "canceled") || done.ExitCode != nil {
		t.Fatalf("lost cancellation: %+v", done)
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.count != 1 {
		t.Fatal("wrong execution count")
	}
}

func TestFileActionRefusesReplacedDirectoryWithSameContent(t *testing.T) {
	s, _, ws, _ := actionFixture(t)
	dir := filepath.Join(ws.ProjectPath, "目录")
	create := func() {
		t.Helper()
		if err := os.Mkdir(dir, 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "café.go"), []byte("before\n"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	create()
	raw := json.RawMessage(`{"path":"目录/café.go","old_text":"before","new_text":"after"}`)
	a, err := s.Propose(context.Background(), ws, "run", "propose_file_change", raw)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(dir, dir+"-moved"); err != nil {
		t.Fatal(err)
	}
	create()
	if _, err := s.Decide(a.ID, ws.ID, "approve"); err == nil {
		t.Fatal("approved a different directory with the same file contents")
	}
}

func TestFileActionRefusesLinkedParent(t *testing.T) {
	s, _, ws, base := actionFixture(t)
	outside := filepath.Join(base, "outside")
	if err := os.Mkdir(outside, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outside, "main.go"), []byte("before\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(ws.ProjectPath, "linked")); err != nil {
		t.Skipf("fixture cannot create symbolic link: %v", err)
	}
	payload := json.RawMessage(`{"path":"linked/main.go","old_text":"before","new_text":"after"}`)
	if _, err := s.Propose(context.Background(), ws, "run", "propose_file_change", payload); err == nil {
		t.Fatal("proposal accepted a linked parent")
	}
}

func TestFileCreationNeverOverwritesExistingName(t *testing.T) {
	_, _, ws, _ := actionFixture(t)
	root, err := os.OpenRoot(ws.ProjectPath)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	err = replaceFile(root, "new.go", "proposed", 0600, true, func(string) error {
		return root.WriteFile("new.go", []byte("editor"), 0600)
	})
	if err == nil {
		t.Fatal("creation unexpectedly replaced a newly existing file")
	}
	raw, err := root.ReadFile("new.go")
	if err != nil || string(raw) != "editor" {
		t.Fatalf("editor file changed: %q, %v", raw, err)
	}
}

func TestFileCreationAppliesAndRollsBack(t *testing.T) {
	s, _, ws, _ := actionFixture(t)
	a, err := s.Propose(context.Background(), ws, "run", "propose_file_change", json.RawMessage(`{"path":"new.go","new_text":"package main"}`))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Decide(a.ID, ws.ID, "approve"); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(ws.ProjectPath, "new.go")
	raw, err := os.ReadFile(path)
	if err != nil || string(raw) != "package main" {
		t.Fatalf("created file content: %q, %v", raw, err)
	}
	if _, err = s.Decide(a.ID, ws.ID, "rollback"); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("created file still exists after rollback: %v", err)
	}
}

func TestProjectRuntimeCannotComeFromProjectOrPrivateAnalysis(t *testing.T) {
	_, _, ws, _ := actionFixture(t)
	name := filepath.Join(ws.ProjectPath, "python.exe")
	if err := os.WriteFile(name, []byte("fixture"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("NOOSPHERE_TEST_PYTHON", name)
	if _, err := discoverTestExecutable(ws.ProjectPath, "python"); err == nil {
		t.Fatal("project executable was selected")
	}
	t.Setenv("NOOSPHERE_TEST_PYTHON", os.Args[0])
	t.Setenv("NOOSPHERE_CODELENS_PYTHON", os.Args[0])
	if _, err := discoverTestExecutable(ws.ProjectPath, "python"); err == nil {
		t.Fatal("private analysis executable was selected")
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

func TestCancelledTaskCannotExecutePendingProposal(t *testing.T) {
	s, m, ws, _ := actionFixture(t)
	ctx, execution, err := m.Start(context.Background(), workspaces.Run{WorkspaceID: ws.ID, Kind: "agent"})
	if err != nil {
		t.Fatal(err)
	}
	defer execution.Finish(nil, nil)
	a, err := s.Propose(ctx, ws, execution.ID, "propose_file_change", json.RawMessage(`{"path":"cancelled.go","new_text":"package example"}`))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = m.Cancel(execution.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Decide(a.ID, ws.ID, "approve"); err == nil {
		t.Fatal("cancelled task's proposal could still execute")
	}
	if found := s.List(ws.ID, execution.ID); len(found) != 1 || found[0].BlockedReason == "" {
		t.Fatalf("missing reason in approval review: %+v", found)
	}
	if _, err = os.Stat(filepath.Join(ws.ProjectPath, a.Path)); !os.IsNotExist(err) {
		t.Fatalf("cancelled task created a file: %v", err)
	}
}

func TestRebindingSamePathDoesNotRestoreApproval(t *testing.T) {
	s, m, ws, _ := actionFixture(t)
	a, err := s.Propose(context.Background(), ws, "run", "propose_file_change", json.RawMessage(`{"path":"rebound.go","new_text":"package example"}`))
	if err != nil {
		t.Fatal(err)
	}
	unbound := ws
	unbound.ProjectPath = ""
	if _, err := m.SaveWorkspace(unbound); err != nil {
		t.Fatal(err)
	}
	if _, err := m.SaveWorkspace(ws); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Decide(a.ID, ws.ID, "approve"); err == nil {
		t.Fatal("rebinding a project reinstated an old approval")
	}
}

func TestFileApprovalRefusesSameContentReplacement(t *testing.T) {
	s, _, ws, _ := actionFixture(t)
	a, err := s.Propose(context.Background(), ws, "run", "propose_file_change", json.RawMessage(`{"path":"main.go","old_text":"before","new_text":"after"}`))
	if err != nil {
		t.Fatal(err)
	}
	name := filepath.Join(ws.ProjectPath, "main.go")
	if err := os.Rename(name, name+".previous"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(name, []byte(a.Before), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Decide(a.ID, ws.ID, "approve"); err == nil {
		t.Fatal("approval applied to a different file with the same content")
	}
	if current := s.List(ws.ID, "run")[0]; current.Status != "obsolete" || current.Error == "" {
		t.Fatalf("file conflict was not retained in the journal: %+v", current)
	}
}

func TestFileApprovalRefusesCaseOnlyRename(t *testing.T) {
	s, _, ws, _ := actionFixture(t)
	a, err := s.Propose(context.Background(), ws, "run", "propose_file_change", json.RawMessage(`{"path":"main.go","old_text":"before","new_text":"after"}`))
	if err != nil {
		t.Fatal(err)
	}
	name := filepath.Join(ws.ProjectPath, "main.go")
	intermediate := filepath.Join(ws.ProjectPath, "renamed.go")
	replacement := filepath.Join(ws.ProjectPath, "MAIN.go")
	if err := os.Rename(name, intermediate); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(intermediate, replacement); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Decide(a.ID, ws.ID, "approve"); err == nil {
		t.Fatal("approval applied after a case-only rename")
	}
}

func TestTestApprovalWaitsForProposedFileChange(t *testing.T) {
	s, _, ws, _ := actionFixture(t)
	t.Setenv("NOOSPHERE_TEST_GO", os.Args[0])
	s.executor = &fixtureExecutor{}
	edit, err := s.Propose(context.Background(), ws, "run", "propose_file_change", json.RawMessage(`{"path":"main.go","old_text":"before","new_text":"after"}`))
	if err != nil {
		t.Fatal(err)
	}
	test, err := s.Propose(context.Background(), ws, "run", "propose_test", json.RawMessage(`{"runner":"go"}`))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Decide(test.ID, ws.ID, "approve"); err == nil {
		t.Fatal("test executed before the proposed edit was approved")
	}
	if _, err = s.Decide(edit.ID, ws.ID, "approve"); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Decide(test.ID, ws.ID, "approve"); err != nil {
		t.Fatal(err)
	}
	verdict := waitActionByID(t, s, ws.ID, "run", test.ID)
	if verdict.Status != "completed" || verdict.ExitCode == nil || *verdict.ExitCode != 0 {
		t.Fatalf("missing successful test exit: %+v", verdict)
	}
}

func TestRestoredActionJournalPreservesHistoryWithoutAuthority(t *testing.T) {
	s, _, ws, base := actionFixture(t)
	for _, status := range []string{"pending", "running", "applied"} {
		a := Action{ID: status, WorkspaceID: ws.ID, RunID: "run", Status: "historical", RestoredHistory: true, RestoredStatus: status}
		if err := s.save(a); err != nil {
			t.Fatal(err)
		}
	}
	restored, err := OpenActions(filepath.Join(base, "actions"), s.manager, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	for _, a := range restored.List(ws.ID, "run") {
		if !a.RestoredHistory || a.RestoredStatus != a.ID || a.Status != "historical" {
			t.Fatalf("original record not preserved: %+v", a)
		}
		for _, decision := range []string{"approve", "cancel", "reject", "rollback"} {
			if _, err := restored.Decide(a.ID, ws.ID, decision); err == nil {
				t.Fatalf("historical action %s accepted %s", a.ID, decision)
			}
		}
	}
}

func TestFileRollbackRefusesSameContentReplacement(t *testing.T) {
	s, _, ws, _ := actionFixture(t)
	a, err := s.Propose(context.Background(), ws, "run", "propose_file_change", json.RawMessage(`{"path":"main.go","old_text":"before","new_text":"after"}`))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Decide(a.ID, ws.ID, "approve"); err != nil {
		t.Fatal(err)
	}
	name := filepath.Join(ws.ProjectPath, "main.go")
	if err := os.Rename(name, name+".previous"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(name, []byte(a.After), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Decide(a.ID, ws.ID, "rollback"); err == nil {
		t.Fatal("rollback changed a replacement file with the same content")
	}
}

func TestConcurrentFileApprovalConsumesOneProposal(t *testing.T) {
	s, _, ws, _ := actionFixture(t)
	a, err := s.Propose(context.Background(), ws, "run", "propose_file_change", json.RawMessage(`{"path":"approved.go","new_text":"package example"}`))
	if err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	results := make(chan error, 2)
	for range 2 {
		go func() {
			<-start
			_, err := s.Decide(a.ID, ws.ID, "approve")
			results <- err
		}()
	}
	close(start)
	approved := 0
	for range 2 {
		if <-results == nil {
			approved++
		}
	}
	if current := s.List(ws.ID, "run")[0]; approved != 1 || current.Status != "applied" {
		t.Fatalf("expected one successful approval, got %d at status %s", approved, current.Status)
	}
}

func TestRejectedFileProposalStaysInert(t *testing.T) {
	s, _, ws, _ := actionFixture(t)
	a, err := s.Propose(context.Background(), ws, "run", "propose_file_change", json.RawMessage(`{"path":"rejected.go","new_text":"package example"}`))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Decide(a.ID, ws.ID, "reject"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Decide(a.ID, ws.ID, "approve"); err == nil {
		t.Fatal("rejected proposal was approved")
	}
	if _, err := os.Stat(filepath.Join(ws.ProjectPath, a.Path)); !os.IsNotExist(err) {
		t.Fatalf("rejected proposal created a file: %v", err)
	}
}
