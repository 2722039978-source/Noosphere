package modelops

import (
	"context"
	"database/sql"
	"fmt"
	"github.com/noosphere/backend/internal/modelstore"
	"os"
	"path/filepath"
	"testing"
)

func canary(t *testing.T, s *Service, report modelstore.Report) modelstore.Release {
	r := create(t, s, report)
	r = apply(t, s, r, StartOffline)
	r = apply(t, s, r, Advance)
	r = replay(t, s, r)
	return apply(t, s, r, Advance)
}
func TestRollbackStopsNewCandidateAdmissions(t *testing.T) {
	s, st, report, path := fixture(t, false)
	r := canary(t, s, report)
	ctx := context.Background()
	assign, e := st.ListSimulationAssignments(ctx, "ws", r.ID)
	if e != nil || len(assign) != 40 {
		t.Fatal("assignments", e)
	}
	// Safe retreat does not depend on report artifacts still being readable.
	if e = os.Remove(filepath.Join(filepath.Dir(path), "model-engineering", "artifacts", report.Selected[0].OutputSHA256)); e != nil {
		t.Fatal(e)
	}
	preview, e := s.Preview(ctx, "ws", r.ID, Rollback)
	if e != nil || !preview.Decision.Allowed {
		t.Fatal("safe preview blocked", e)
	}
	previous, e := s.Apply(ctx, "ws", r.ID, Advance, r.Revision-1, fmt.Sprintf("advance-%d", r.Revision-1))
	if e != nil || previous.Revision != r.Revision {
		t.Fatal("successful receipt recalculated after evidence loss", e)
	}
	r = apply(t, s, r, Rollback)
	if r.AdmissionEnabled || r.TargetBPS != 0 || r.ServingRevisionID != "stable" {
		t.Fatal("admission remained open")
	}
	assign, e = st.ListSimulationAssignments(ctx, "ws", r.ID)
	if e != nil {
		t.Fatal(e)
	}
	for _, a := range assign {
		if a.Arm == "candidate" && a.RevokedAt == "" {
			t.Fatal("queued candidate not revoked")
		}
	}
	if _, e = s.Simulate(ctx, "ws", r.ID, SimulationRequest{Scenario: "replay_stage", VirtualSeconds: 60}, r.Revision, "late"); e == nil {
		t.Fatal("post rollback replay")
	}
}
func TestRestartPreservesStageAndAssignments(t *testing.T) {
	s, st, report, path := fixture(t, false)
	r := canary(t, s, report)
	r = replay(t, s, r)
	before, _ := ReadSimulation(r)
	if e := st.Close(); e != nil {
		t.Fatal(e)
	}
	st2, e := modelstore.Open(path)
	if e != nil {
		t.Fatal(e)
	}
	defer st2.Close()
	s2, e := New(context.Background(), st2)
	if e != nil {
		t.Fatal(e)
	}
	if e = s2.Recover(context.Background()); e != nil {
		t.Fatal(e)
	}
	r, e = st2.GetRelease(context.Background(), r.ID)
	if e != nil || r.State != "PAUSED" || r.PausedFrom != "CANARY_5" || r.AdmissionEnabled {
		t.Fatal("restart state", r, e)
	}
	after, _ := ReadSimulation(r)
	if after.ClockSeconds != before.ClockSeconds || len(after.Stages) != len(before.Stages) || after.Stages[1].Assignments[0] != before.Stages[1].Assignments[0] {
		t.Fatal("checkpoint lost")
	}
	assignments, _ := st2.ListSimulationAssignments(context.Background(), "ws", r.ID)
	for _, a := range assignments {
		if a.Arm == "candidate" && a.RevokedAt == "" {
			t.Fatal("recovery did not revoke candidate")
		}
	}
	r = apply(t, s2, r, Resume)
	p, e := s2.Preview(context.Background(), "ws", r.ID, Advance)
	if e != nil || p.Decision.Allowed {
		t.Fatal("resume reused old observation window", e)
	}
	r = replay(t, s2, r)
	r = apply(t, s2, r, Advance)
	if r.State != "CANARY_25" {
		t.Fatal(r.State)
	}
}
func TestAuditFailurePreventsTransition(t *testing.T) {
	s, st, report, path := fixture(t, false)
	r := canary(t, s, report)
	ctx := context.Background()
	db, e := sql.Open("sqlite", path)
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	if _, e = db.Exec("CREATE TRIGGER fail_audit BEFORE INSERT ON release_events BEGIN SELECT RAISE(ABORT,'audit unavailable'); END"); e != nil {
		t.Fatal(e)
	}
	if _, e = s.Apply(ctx, "ws", r.ID, Rollback, r.Revision, "audit-failure"); e == nil {
		t.Fatal("audit failure ignored")
	}
	current, _ := st.GetRelease(ctx, r.ID)
	if current.Revision != r.Revision || current.State != r.State || current.RouteEpoch != r.RouteEpoch {
		t.Fatal("partial transition")
	}
	a, _ := st.ListSimulationAssignments(ctx, "ws", r.ID)
	for _, v := range a {
		if v.Arm == "candidate" && v.RevokedAt != "" {
			t.Fatal("partial revocation")
		}
	}
	if _, e = db.Exec("DROP TRIGGER fail_audit"); e != nil {
		t.Fatal(e)
	}
	if _, e = s.Apply(ctx, "ws", r.ID, Rollback, r.Revision, "audit-failure"); e != nil {
		t.Fatal("retry after rolled back audit tx", e)
	}
}
