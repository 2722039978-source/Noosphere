package modelops

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/noosphere/backend/internal/middleware"
	"github.com/noosphere/backend/internal/modellab"
	"github.com/noosphere/backend/internal/modelstore"
	"github.com/noosphere/backend/internal/workspaces"
)

func TestHTTPOwnedEvidenceAndBoundedHistory(t *testing.T) {
	manager, err := workspaces.Open(filepath.Join(t.TempDir(), "workspace.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Close()
	ws, err := manager.SaveWorkspace(workspaces.Workspace{Name: "owner"})
	if err != nil {
		t.Fatal(err)
	}
	other, err := manager.SaveWorkspace(workspaces.Workspace{Name: "other"})
	if err != nil {
		t.Fatal(err)
	}
	_, st, report, path := fixture(t, false, ws.ID)
	mux := http.NewServeMux()
	modellab.New(st, nil).RegisterRoutes(mux, manager)
	get := func(path string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		return w
	}
	base := "/api/workspaces/" + ws.ID
	w := get(base + "/evaluations/eval?limit=1")
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var detail struct {
		Attempts   []map[string]any  `json:"attempts"`
		NextCursor string            `json:"next_cursor"`
		Progress   modelstore.Counts `json:"progress"`
	}
	if err = json.Unmarshal(w.Body.Bytes(), &detail); err != nil {
		t.Fatal(err)
	}
	if len(detail.Attempts) != 1 || detail.NextCursor == "" || detail.Progress.Scored != 24 || detail.Progress.PairedComplete != 12 {
		t.Fatal(detail)
	}
	if _, ok := detail.Attempts[0]["result"]; ok {
		t.Fatal("detail leaked full attempt output")
	}
	first := detail.Attempts[0]["id"]
	w = get(base + "/evaluations/eval?limit=1&cursor=" + detail.NextCursor)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	json.Unmarshal(w.Body.Bytes(), &detail)
	if detail.Attempts[0]["id"] == first {
		t.Fatal("cursor repeated a row")
	}
	w = get(base + "/reports/" + report.ID)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"report_hash":"`+report.Hash+`"`) || !strings.Contains(w.Body.String(), `"promotion_eligibility":`) {
		t.Fatal(w.Code, w.Body.String())
	}
	w = get(base + "/evaluations/eval/cases/case00?limit=1")
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"value":null`) || !strings.Contains(w.Body.String(), `"content":"synthetic input/output"`) {
		t.Fatal(w.Code, w.Body.String())
	}
	w = get("/api/workspaces/" + other.ID + "/evaluations/eval/cases/case00")
	if w.Code != 404 {
		t.Fatal(w.Code, w.Body.String())
	}
	// A foreign corrupt report must remain an ownership404; its evidence error
	// must not be observable through another workspace's URL.
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err = db.Exec("DROP TRIGGER reports_no_UPDATE"); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec("UPDATE reports SET body_json='{}' WHERE id=?", report.ID); err != nil {
		t.Fatal(err)
	}
	w = get("/api/workspaces/" + other.ID + "/reports/" + report.ID)
	if w.Code != 404 {
		t.Fatalf("foreign damaged report: %d %s", w.Code, w.Body.String())
	}
}

func TestHTTPForeignDamagedResourceOwnershipBeforeDecode(t *testing.T) {
	manager, err := workspaces.Open(filepath.Join(t.TempDir(), "workspace.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Close()
	ws, err := manager.SaveWorkspace(workspaces.Workspace{Name: "owner"})
	if err != nil {
		t.Fatal(err)
	}
	other, err := manager.SaveWorkspace(workspaces.Workspace{Name: "other"})
	if err != nil {
		t.Fatal(err)
	}
	svc, st, report, path := fixture(t, false, ws.ID)
	release := create(t, svc, report)
	mux := http.NewServeMux()
	modellab.New(st, nil).RegisterRoutes(mux, manager)
	svc.RegisterRoutes(mux, manager)
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err = db.Exec("DROP TRIGGER evaluation_manifest_immutable"); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec("UPDATE evaluations SET manifest_json='[]' WHERE id='eval'"); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec("UPDATE releases SET body_json='[]' WHERE id=?", release.ID); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ method, path string }{
		{"GET", "/evaluations/eval"}, {"GET", "/evaluations/eval/cases/case00"}, {"POST", "/evaluations/eval/commands"},
		{"GET", "/releases/" + release.ID}, {"POST", "/releases/" + release.ID + "/commands"}, {"GET", "/releases/" + release.ID + "/events"},
	} {
		for _, owner := range []struct {
			id     string
			status int
		}{{other.ID, 404}, {ws.ID, 503}} {
			t.Run(tc.method+tc.path+owner.id, func(t *testing.T) {
				w := httptest.NewRecorder()
				mux.ServeHTTP(w, httptest.NewRequest(tc.method, "/api/workspaces/"+owner.id+tc.path, strings.NewReader(`{}`)))
				if w.Code != owner.status {
					t.Fatalf("damaged resource ownership: %d %s", w.Code, w.Body.String())
				}
			})
		}
	}
}

func TestHTTPCaseRedactsOnlyPrivateFrozenProtocol(t *testing.T) {
	manager, err := workspaces.Open(filepath.Join(t.TempDir(), "workspace.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Close()
	ws, err := manager.SaveWorkspace(workspaces.Workspace{Name: "owner"})
	if err != nil {
		t.Fatal(err)
	}
	input := []byte(`{"schema_version":1,"context":{"public":{"file":"cases/case00/faulty.go"},"prompt":"public fixture prompt","context_hash":"public-hash","analysis_response_hash":"analysis-hash","health_response_hash":"health-hash","analysis_response":{"file":"C:\\private\\fixture.go","token":"protocol-secret"},"health_response":{"environment":"private-host-detail"}}}`)
	_, st, _, _ := fixtureInput(t, false, input, ws.ID)
	mux := http.NewServeMux()
	modellab.New(st, nil).RegisterRoutes(mux, manager)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest("GET", "/api/workspaces/"+ws.ID+"/evaluations/eval/cases/case00?limit=1", nil))
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	for _, private := range []string{"private", "protocol-secret", "analysis_response\\\":", "health_response\\\":"} {
		if strings.Contains(w.Body.String(), private) {
			t.Fatalf("private frozen protocol leaked %q: %s", private, w.Body.String())
		}
	}
	if !strings.Contains(w.Body.String(), `"redacted":true`) || !strings.Contains(w.Body.String(), "analysis-hash") || !strings.Contains(w.Body.String(), "public fixture prompt") {
		t.Fatal(w.Body.String())
	}
	ev, err := st.GetEvaluation(t.Context(), "eval")
	if err != nil {
		t.Fatal(err)
	}
	stored, err := st.ReadArtifact(t.Context(), ev.Manifest.Dataset.Cases[0].InputArtifact)
	if err != nil || string(stored) != string(input) {
		t.Fatal("immutable frozen evidence changed", err)
	}
}

// This opt-in harness serves actual authenticated production routes and durable
// Manager/ModelOps workers. It never invokes an inference or compiler fixture.
func TestModelEngineeringDesktopHarness(t *testing.T) {
	readyPath := os.Getenv("NOOSPHERE_MODEL_ENGINEERING_READY_FILE")
	if readyPath == "" {
		t.Skip("opt-in actual Rust transport harness")
	}
	if !filepath.IsAbs(readyPath) {
		t.Fatal("ready file must be absolute")
	}
	manager, e := workspaces.Open(filepath.Join(t.TempDir(), "workspace.sqlite"))
	if e != nil {
		t.Fatal(e)
	}
	defer manager.Close()
	ws, e := manager.SaveWorkspace(workspaces.Workspace{Name: "Synthetic desktop transport fixture"})
	if e != nil {
		t.Fatal(e)
	}
	svc, st, report, _ := fixture(t, false, ws.ID)
	svc.RegisterWorkspace(manager)
	rel := apply(t, svc, apply(t, svc, create(t, svc, report), StartOffline), Advance)
	mux := http.NewServeMux()
	svc.RegisterRoutes(mux, manager)
	modellab.New(st, nil).RegisterRoutes(mux, manager)
	manager.RegisterRoutes(mux)
	var secret [24]byte
	if _, e = rand.Read(secret[:]); e != nil {
		t.Fatal(e)
	}
	token := hex.EncodeToString(secret[:])
	server := httptest.NewServer(middleware.BearerAuth(token)(mux))
	defer server.Close()
	u, _ := url.Parse(server.URL)
	_, portText, _ := net.SplitHostPort(u.Host)
	port, _ := strconv.Atoi(portText)
	ready := struct {
		Port        int    `json:"port"`
		WorkspaceID string `json:"workspace_id"`
		ReleaseID   string `json:"release_id"`
		Revision    int64  `json:"revision"`
		ReportID    string `json:"report_id"`
		Token       string `json:"token"`
	}{port, ws.ID, rel.ID, rel.Revision, report.ID, token}
	raw, _ := json.Marshal(ready)
	f, e := os.OpenFile(readyPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = f.Write(raw); e != nil {
		f.Close()
		t.Fatal(e)
	}
	f.Close()
	defer os.Remove(readyPath)
	defer os.Remove(readyPath + ".stop")
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	deadline := time.NewTimer(50 * time.Second)
	defer deadline.Stop()
	for {
		select {
		case <-ticker.C:
			if _, e = os.Stat(readyPath + ".stop"); e != nil {
				continue
			}
			current, e := st.GetRelease(context.Background(), rel.ID)
			if e != nil {
				t.Fatal(e)
			}
			simulation, e := ReadSimulation(current)
			if e != nil || simulation.Task == nil || simulation.Task.Status != "completed" || simulation.Task.RunID == "" || simulation.Source != "simulation_replay" || simulation.OfflineSource != "offline_fixture" {
				t.Fatalf("persisted simulation incomplete: %+v %v", simulation, e)
			}
			run, e := manager.Run(simulation.Task.RunID)
			if e == nil && (run.Status == "queued" || run.Status == "running") {
				continue
			}
			if e != nil || run.Status != "completed" {
				t.Fatal(run.Status, e)
			}
			saved, e := st.GetReport(context.Background(), report.ID)
			if e != nil || saved.Hash != report.Hash {
				t.Fatal("immutable report changed", e)
			}
			t.Logf("actual desktop transport job persisted: kind=%s state=%s source=%s offline_source=%s revision=%d report_hash=%s", run.Kind, run.Status, simulation.Source, simulation.OfflineSource, current.Revision, saved.Hash)
			return
		case <-deadline.C:
			t.Fatal("actual desktop transport did not complete within50s")
		}
	}
}

func TestHTTPReleaseControlReceiptsAndGates(t *testing.T) {
	manager, e := workspaces.Open(filepath.Join(t.TempDir(), "ws.sqlite"))
	if e != nil {
		t.Fatal(e)
	}
	defer manager.Close()
	ws, e := manager.SaveWorkspace(workspaces.Workspace{Name: "one", ProjectPath: t.TempDir()})
	if e != nil {
		t.Fatal(e)
	}
	svc, st, report, _ := fixture(t, false, ws.ID)
	svc.RegisterWorkspace(manager)
	mux := http.NewServeMux()
	svc.RegisterRoutes(mux, manager)
	call := func(path, body, key string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", path, strings.NewReader(body))
		r.Header.Set("Idempotency-Key", key)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		return w
	}
	base := "/api/workspaces/" + ws.ID + "/releases"
	if w := call(base, `{"mode":"live","workflow_id":"`+Workflow+`","report_id":"`+report.ID+`"}`, "live"); w.Code != 422 {
		t.Fatal(w.Code, w.Body.String())
	}
	body := `{"mode":"simulation","workflow_id":"` + Workflow + `","report_id":"` + report.ID + `"}`
	first := call(base, body, "create-http")
	if first.Code != 201 {
		t.Fatal(first.Code, first.Body.String())
	}
	again := call(base, body, "create-http")
	if again.Code != 201 || again.Body.String() != first.Body.String() {
		t.Fatal(again.Code, again.Body.String())
	}
	var rel struct {
		ID       string `json:"id"`
		Revision int64  `json:"revision"`
	}
	json.Unmarshal(first.Body.Bytes(), &rel)
	endpoint := base + "/" + rel.ID + "/commands"
	w := call(endpoint, fmt.Sprintf(`{"command":"start_offline","expected_revision":%d}`, rel.Revision), "start")
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	conflict := call(endpoint, fmt.Sprintf(`{"command":"advance","expected_revision":%d}`, rel.Revision), "stale")
	if conflict.Code != 409 || !strings.Contains(conflict.Body.String(), `"current_revision":2`) {
		t.Fatal(conflict.Code, conflict.Body.String())
	}
	if replay := call(endpoint, fmt.Sprintf(`{"command":"advance","expected_revision":%d}`, rel.Revision), "stale"); replay.Code != 409 {
		t.Fatal(replay.Code)
	}
	w = call(endpoint, `{"command":"advance","expected_revision":2}`, "advance")
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	denied := call(endpoint, `{"command":"advance","expected_revision":3}`, "gate")
	if denied.Code != 409 || !strings.Contains(denied.Body.String(), `"gates":[`) || !strings.Contains(denied.Body.String(), `"status":"insufficient"`) {
		t.Fatal(denied.Code, denied.Body.String())
	}
	current, _ := st.GetRelease(t.Context(), rel.ID)
	if current.State != "SHADOW" {
		t.Fatal(current)
	}
	cross := call("/api/workspaces/other/releases/"+rel.ID+"/commands", `{"command":"pause","expected_revision":3}`, "cross")
	if cross.Code != 404 {
		t.Fatal(cross.Code, cross.Body.String())
	}
}
