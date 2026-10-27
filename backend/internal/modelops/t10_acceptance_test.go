package modelops

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/noosphere/backend/internal/middleware"
	"github.com/noosphere/backend/internal/modellab"
	"github.com/noosphere/backend/internal/modelstore"
	"github.com/noosphere/backend/internal/workspaces"
)

// This acceptance test sends real loopback HTTP requests through production
// bearer auth, HTTP handlers, Manager jobs and disk-backed modelstore. Its
// evidence is offline_fixture, never a model-quality or live-traffic result.
func TestT10HTTPReleaseLifecycleAndReopen(t *testing.T) {
	ctx := context.Background()
	workspacePath := filepath.Join(t.TempDir(), "workspace.sqlite")
	manager, err := workspaces.Open(workspacePath)
	if err != nil {
		t.Fatal(err)
	}
	ws, err := manager.SaveWorkspace(workspaces.Workspace{ID: "local", Name: "TEST T10 OFFLINE FIXTURE"})
	if err != nil {
		t.Fatal(err)
	}
	svc, store, report, storePath := fixture(t, false, ws.ID)
	const testToken = "t10-synthetic-loopback-acceptance-token"
	var server *httptest.Server
	start := func() {
		svc.RegisterWorkspace(manager)
		mux := http.NewServeMux()
		svc.RegisterRoutes(mux, manager)
		modellab.New(store, nil).RegisterRoutes(mux, manager)
		manager.RegisterRoutes(mux)
		server = httptest.NewServer(middleware.BearerAuth(testToken)(mux))
	}
	stop := func() {
		server.Close()
		svc.StopAdmission()
		svc.DrainHTTP()
		if err := manager.Close(); err != nil {
			t.Fatal(err)
		}
		if err := store.Close(); err != nil {
			t.Fatal(err)
		}
	}
	start()
	t.Cleanup(func() { stop() })
	call := func(method, path string, body any, key string, want int) []byte {
		t.Helper()
		var encoded []byte
		if body != nil {
			var err error
			encoded, err = json.Marshal(body)
			if err != nil {
				t.Fatal(err)
			}
		}
		req, err := http.NewRequestWithContext(ctx, method, server.URL+path, bytes.NewReader(encoded))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Authorization", "Bearer "+testToken)
		req.Header.Set("Content-Type", "application/json")
		if key != "" {
			req.Header.Set("Idempotency-Key", key)
		}
		res, err := server.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		raw, err := io.ReadAll(res.Body)
		if err != nil || res.StatusCode != want {
			t.Fatalf("%s %s: status=%d want=%d body=%s error=%v", method, path, res.StatusCode, want, raw, err)
		}
		return raw
	}
	res, err := server.Client().Get(server.URL + "/api/workspaces/local/releases")
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unauthenticated release read: %d", res.StatusCode)
	}
	base := "/api/workspaces/" + ws.ID + "/releases"
	var release modelstore.Release
	decodeRelease := func(raw []byte) {
		t.Helper()
		if err := json.Unmarshal(raw, &release); err != nil || release.ID == "" {
			t.Fatalf("invalid release: %s %v", raw, err)
		}
	}
	decodeRelease(call("POST", base, map[string]any{"mode": "simulation", "workflow_id": Workflow, "report_id": report.ID}, "t10-create", 201))
	endpoint := base + "/" + release.ID
	states := []string{release.State}
	command := func(cmd Command, want string) {
		t.Helper()
		decodeRelease(call("POST", endpoint+"/commands", map[string]any{"command": cmd, "expected_revision": release.Revision}, fmt.Sprintf("t10-%s-%d", cmd, release.Revision), 200))
		if release.State != want {
			t.Fatalf("%s: state=%s want=%s", cmd, release.State, want)
		}
		states = append(states, release.State)
	}
	preview := func() DecisionPreview {
		t.Helper()
		var p DecisionPreview
		raw := call("POST", endpoint+"/preview", map[string]any{"command": Advance}, "", 200)
		if err := json.Unmarshal(raw, &p); err != nil {
			t.Fatal(err)
		}
		return p
	}
	assertWindowRefusal := func(wantNS int64) {
		t.Helper()
		p := preview()
		if p.Decision.Allowed {
			t.Fatal("advance bypassed missing observation window")
		}
		found := false
		for _, rule := range p.Decision.Rules {
			if rule.ID == "stage_"+release.State {
				found = rule.Status == "insufficient" && rule.ObservationWindowNS == wantNS
			}
		}
		if !found {
			t.Fatalf("missing precise observation refusal: %+v", p.Decision)
		}
		raw := call("POST", endpoint+"/commands", map[string]any{"command": Advance, "expected_revision": release.Revision}, fmt.Sprintf("t10-denied-%d", release.Revision), 409)
		if !bytes.Contains(raw, []byte(`"gates":`)) || !bytes.Contains(raw, []byte(`"status":"insufficient"`)) {
			t.Fatalf("refusal omitted actual gate facts: %s", raw)
		}
	}
	runIDs := []string{}
	replayHTTP := func(seconds int64) {
		t.Helper()
		var accepted struct {
			RunID string `json:"run_id"`
		}
		raw := call("POST", endpoint+"/simulation", map[string]any{"scenario": "replay_stage", "expected_revision": release.Revision, "seed": 7, "virtual_seconds": seconds}, fmt.Sprintf("t10-replay-%d", release.Revision), 202)
		if err := json.Unmarshal(raw, &accepted); err != nil || accepted.RunID == "" {
			t.Fatalf("accepted simulation has no actual Manager run: %s %v", raw, err)
		}
		runIDs = append(runIDs, accepted.RunID)
		deadline := time.Now().Add(3 * time.Second)
		for {
			run, err := manager.Run(accepted.RunID)
			if err != nil {
				t.Fatal(err)
			}
			if run.Status == "completed" {
				if run.Kind != "release_simulation" || run.WorkspaceID != ws.ID || run.ReferenceID != release.ID || run.ReferenceGeneration < 1 {
					t.Fatalf("Manager reference mismatch: %+v", run)
				}
				break
			}
			if run.Status == "failed" || run.Status == "cancelled" || time.Now().After(deadline) {
				t.Fatalf("actual Manager run did not complete: %+v", run)
			}
			time.Sleep(5 * time.Millisecond)
		}
		decodeRelease(call("GET", endpoint, nil, "", 200))
		sim, err := ReadSimulation(release)
		if err != nil || sim.Task == nil || sim.Task.Status != "completed" || sim.Task.RunID != accepted.RunID || sim.Source != "simulation_replay" || sim.OfflineSource != "offline_fixture" {
			t.Fatalf("checkpoint provenance/run missing: %+v %v", sim, err)
		}
		stage := sim.Stages[len(sim.Stages)-1]
		if stage.UniqueCases != 12 || stage.RealInferenceCount != 0 || stage.CandidateUserOutputs != 0 || stage.SideEffects != 0 || stage.ReportHash != report.Hash {
			t.Fatalf("replay claimed live execution or lost frozen evidence: %+v", stage)
		}
	}
	command(StartOffline, "OFFLINE_EVAL")
	command(Advance, "SHADOW")
	assertWindowRefusal(0)
	replayHTTP(30)
	assertWindowRefusal(int64(30 * time.Second))
	replayHTTP(30)
	command(Advance, "CANARY_5")
	replayHTTP(60)
	command(Pause, "PAUSED")
	if release.AdmissionEnabled || release.ServingRevisionID != "stable" || release.PausedFrom != "CANARY_5" {
		t.Fatalf("manual pause admitted candidate: %+v", release)
	}
	command(Resume, "CANARY_5")
	assertWindowRefusal(0)
	replayHTTP(60)
	beforeRestart, err := ReadSimulation(release)
	if err != nil {
		t.Fatal(err)
	}
	beforeRevision := release.Revision
	stop()
	manager, err = workspaces.Open(workspacePath)
	if err != nil {
		t.Fatal(err)
	}
	store, err = modelstore.Open(storePath)
	if err != nil {
		t.Fatal(err)
	}
	svc, err = New(ctx, store)
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.Recover(ctx); err != nil {
		t.Fatal(err)
	}
	start()
	decodeRelease(call("GET", endpoint, nil, "", 200))
	if release.State != "PAUSED" || release.PausedFrom != "CANARY_5" || release.AdmissionEnabled || release.Revision <= beforeRevision {
		t.Fatalf("reopen did not persist safe pause: %+v", release)
	}
	afterRestart, err := ReadSimulation(release)
	if err != nil || afterRestart.ClockSeconds != beforeRestart.ClockSeconds || len(afterRestart.Stages) != len(beforeRestart.Stages) {
		t.Fatalf("reopen lost checkpoint: %+v %v", afterRestart, err)
	}
	for _, runID := range runIDs {
		run, err := manager.Run(runID)
		if err != nil || run.Status != "completed" || run.ReferenceID != release.ID {
			t.Fatalf("Manager run lost after reopen: %s %+v %v", runID, run, err)
		}
	}
	assignments, err := store.ListSimulationAssignments(ctx, ws.ID, release.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range assignments {
		if a.Arm == "candidate" && a.RevokedAt == "" {
			t.Fatal("restart left old candidate assignment admitted")
		}
	}
	command(Resume, "CANARY_5")
	assertWindowRefusal(0)
	for _, next := range []string{"CANARY_25", "CANARY_50", "PROMOTED"} {
		replayHTTP(60)
		command(Advance, next)
	}
	if release.ServingRevisionID != "candidate" || release.PromotedAt == "" || release.RollbackUntil == "" {
		t.Fatalf("promotion missing candidate/retention: %+v", release)
	}
	command(Rollback, "ROLLED_BACK")
	if release.ServingRevisionID != "stable" || release.AdmissionEnabled || release.TargetBPS != 0 {
		t.Fatalf("rollback not safe: %+v", release)
	}
	finalReport := call("GET", "/api/workspaces/"+ws.ID+"/reports/"+report.ID, nil, "", 200)
	if !bytes.Contains(finalReport, []byte(report.Hash)) || !bytes.Contains(finalReport, []byte(`"source":"offline_fixture"`)) {
		t.Fatalf("report changed or lost fixture provenance: %s", finalReport)
	}
	var events struct {
		Events []struct {
			modelstore.ReleaseEvent
			Hash string `json:"event_hash"`
		} `json:"events"`
	}
	rawEvents := call("GET", endpoint+"/events?limit=100", nil, "", 200)
	if err := json.Unmarshal(rawEvents, &events); err != nil || len(events.Events) == 0 {
		t.Fatalf("missing durable audit trail: %s %v", rawEvents, err)
	}
	for i, event := range events.Events {
		if event.Hash == "" || event.Sequence < 1 || (i > 0 && event.Sequence <= events.Events[i-1].Sequence) {
			t.Fatalf("audit event missing stable hash/order: %+v", event)
		}
	}
	t.Logf("T10 actual authenticated HTTP + Manager + reopen proof: states=%s manager_runs=%d audit_events=%d report_hash=%s source=offline_fixture real_inference=0", strings.Join(states, " -> "), len(runIDs), len(events.Events), report.Hash)
	t.Logf("T10 persisted Manager references: workspace_id=%s release_id=%s kind=release_simulation run_ids=%s", ws.ID, release.ID, strings.Join(runIDs, ","))
}

func TestT10HTTPInsufficientPairsRefused(t *testing.T) {
	manager, err := workspaces.Open(filepath.Join(t.TempDir(), "workspace.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Close()
	ws, err := manager.SaveWorkspace(workspaces.Workspace{ID: "local", Name: "TEST incomplete pairs fixture"})
	if err != nil {
		t.Fatal(err)
	}
	svc, _, report, _ := fixtureInputAt(t, false, []byte("TEST OFFLINE FIXTURE incomplete paired evidence"), filepath.Join(t.TempDir(), "ops.sqlite"), ws.ID, 11)
	mux := http.NewServeMux()
	svc.RegisterRoutes(mux, manager)
	post := func(path, body, key string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", path, strings.NewReader(body))
		r.Header.Set("Idempotency-Key", key)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		return w
	}
	if report.Counts.PairedComplete != 11 || report.Counts.Scored != 23 {
		t.Fatalf("incomplete fixture must remain incomplete: %+v", report.Counts)
	}
	base := "/api/workspaces/local/releases"
	w := post(base, fmt.Sprintf(`{"mode":"simulation","workflow_id":%q,"report_id":%q}`, Workflow, report.ID), "t10-incomplete-create")
	if w.Code != 409 || !strings.Contains(w.Body.String(), `"code":"INSUFFICIENT_EVIDENCE"`) {
		t.Fatalf("11 paired cases admitted to release creation: %d %s", w.Code, w.Body.String())
	}
	get := httptest.NewRecorder()
	mux.ServeHTTP(get, httptest.NewRequest("GET", base, nil))
	if get.Code != 200 || !strings.Contains(get.Body.String(), `"releases":[]`) {
		t.Fatalf("failed create left a release: %d %s", get.Code, get.Body.String())
	}
	t.Log("T10 actual HTTP refusal: 11/12 complete pairs, 23/24 scored outputs, release creation blocked before DRAFT")
}

// This opt-in seed is retained for the actual native desktop process. No
// server, worker or app is launched here, and there is no production seed API.
// Reuse is rejected before opening either SQLite database.
func TestT10SeedNativeDesktopFixtures(t *testing.T) {
	dir := os.Getenv("NOOSPHERE_MODEL_ENGINEERING_SEED_DIR")
	if dir == "" {
		t.Skip("opt-in retained offline_fixture data for native desktop acceptance")
	}
	dir = filepath.Clean(dir)
	if !filepath.IsAbs(dir) || filepath.Dir(dir) == dir {
		t.Fatal("seed path must be an absolute dedicated directory below the volume root")
	}
	if info, err := os.Lstat(dir); err == nil {
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			t.Fatal("seed path must be an ordinary directory")
		}
	} else if !os.IsNotExist(err) {
		t.Fatal(err)
	} else if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 0 {
		t.Fatalf("refusing to reuse or modify a nonempty seed directory: %s (%v)", dir, err)
	}
	guard, err := os.OpenFile(filepath.Join(dir, "OFFLINE_FIXTURE_ONLY"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := guard.WriteString("TEST DATA: offline_fixture; synthetic outputs; unknown hardware; zero model/compiler calls.\n"); err != nil {
		guard.Close()
		t.Fatal(err)
	}
	if err := guard.Close(); err != nil {
		t.Fatal(err)
	}
	manager, err := workspaces.Open(filepath.Join(dir, "workspace.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	ws, err := manager.SaveWorkspace(workspaces.Workspace{ID: "local", Name: "TEST T10 OFFLINE FIXTURE — synthetic release rehearsal"})
	if err != nil {
		manager.Close()
		t.Fatal(err)
	}
	if err := manager.Close(); err != nil {
		t.Fatal(err)
	}
	_, store, report, _ := fixtureInputAt(t, false, []byte("TEST OFFLINE FIXTURE ONLY — synthetic input/output; no model or compiler executed"), filepath.Join(dir, "model-engineering.sqlite"), ws.ID, 12)
	// Prove eligibility without persisting a release: native UI owns creation.
	ev, err := store.GetEvaluation(t.Context(), report.EvaluationID)
	if err != nil {
		t.Fatal(err)
	}
	policy, err := store.GetPolicy(t.Context(), PolicyID)
	if err != nil {
		t.Fatal(err)
	}
	eligibility := decision(modelstore.SimulationSnapshot{Evaluation: ev, Report: report, Policy: policy, Release: modelstore.Release{Mode: "simulation", WorkflowID: Workflow, FilterHash: TaskType, StableRevisionID: ev.Manifest.Stable.ID, CandidateRevisionID: ev.Manifest.Candidate.ID}}, "OFFLINE_EVAL")
	if !eligibility.Allowed {
		t.Fatalf("seed report must pass actual offline policy: %+v", eligibility)
	}
	public := map[string]any{
		"schema_version": 1, "source": "offline_fixture", "label": "TEST OFFLINE FIXTURE ONLY", "hardware": "unknown; no hardware collection performed",
		"workspace_id": ws.ID, "evaluation_id": ev.ID, "manifest_hash": ev.ManifestHash, "report_id": report.ID, "report_hash": report.Hash,
		"stable_model_name": ev.Manifest.Stable.ModelName, "candidate_model_name": ev.Manifest.Candidate.ModelName,
		"planned_outputs": report.Counts.Planned, "scored_outputs": report.Counts.Scored, "complete_pairs": report.Counts.PairedComplete,
		"real_inference_count": 0, "compiler_call_count": 0, "release_seeded": false,
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	// Reopen validates on-disk report and content-addressed artifacts after close.
	reopened, err := modelstore.Open(filepath.Join(dir, "model-engineering.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	saved, err := reopened.GetReport(t.Context(), report.ID)
	if err != nil || saved.Hash != report.Hash {
		reopened.Close()
		t.Fatal("retained report failed reopen/hash verification", err)
	}
	if err := reopened.Close(); err != nil {
		t.Fatal(err)
	}
	raw, err := json.MarshalIndent(public, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	f, err := os.OpenFile(filepath.Join(dir, "fixture-seed.json"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.Write(append(raw, '\n')); err != nil {
		f.Close()
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	t.Logf("retained native fixture seed: directory=%s public_manifest=%s report_hash=%s source=offline_fixture real_inference=0", dir, filepath.Join(dir, "fixture-seed.json"), report.Hash)
}
