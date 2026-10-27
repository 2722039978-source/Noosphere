package modelstore

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const policyFixture = `{"id":"policy","mode":"simulation","version":"frozen-test-v1","rollback_retention_ns":604800000000000,"release_rules":[{"id":"quality","domain":"quality","stage":"all","operator":"gte","required":1,"unit":"pass","minimum_samples":1,"observation_window_ns":0},{"id":"samples","domain":"statistics","stage":"all","operator":"gte","required":1,"unit":"pairs","minimum_samples":1,"observation_window_ns":60},{"id":"oom_free","domain":"resources","stage":"all","operator":"eq","required":true,"unit":"boolean","minimum_samples":1,"observation_window_ns":0}],"rollback_rules":[{"id":"oom","domain":"resources","stage":"all","operator":"eq","required":true,"unit":"event","minimum_samples":1,"observation_window_ns":0}]}`

func samplePolicy() ReleasePolicy {
	var p ReleasePolicy
	if err := json.Unmarshal([]byte(policyFixture), &p); err != nil {
		panic(err)
	}
	return p
}
func decisionFixture(t *testing.T, p ReleasePolicy, r Report) GateDecision {
	t.Helper()
	var d GateDecision
	raw := `{"allowed":true,"rules":[{"id":"quality","observed":1,"required":1,"unit":"pass","evidence_ids":["quality-evidence"],"status":"pass","reason":"host verdict","sample_count":1,"observation_window_ns":0},{"id":"samples","observed":1,"required":1,"unit":"pairs","evidence_ids":["paired-evidence"],"status":"pass","reason":"one pair","sample_count":1,"observation_window_ns":60},{"id":"oom_free","observed":true,"required":true,"unit":"boolean","evidence_ids":["resource-evidence"],"status":"pass","reason":"fixture observation","sample_count":1,"observation_window_ns":0}]}`
	if err := json.Unmarshal([]byte(raw), &d); err != nil {
		t.Fatal(err)
	}
	d.PolicyHash = p.Hash
	d.EvidenceHash = r.Hash
	return d
}
func releaseFixture(t *testing.T, s *Store) (ReleasePolicy, Report, Release) {
	t.Helper()
	ctx := context.Background()
	_, r := completeTest(t, s, "eval")
	p, err := s.SavePolicy(ctx, samplePolicy())
	if err != nil {
		t.Fatal(err)
	}
	rel, err := s.CreateRelease(ctx, CreateReleaseRequest{ID: "release", WorkspaceID: "ws", WorkflowID: "codelens.codetask.go.v1", FilterHash: "all", ReportID: r.ID, PolicyID: p.ID, Idempotency: Idempotency{Key: "create"}})
	if err != nil {
		t.Fatal(err)
	}
	rel, err = s.ApplyReleaseTransition(ctx, ReleaseTransition{ReleaseID: rel.ID, ExpectedRevision: rel.Revision, Command: "start_offline", Actor: "user", ReasonCode: "CONFIRMED", EvidenceHash: r.Hash, Idempotency: Idempotency{Key: "start"}})
	if err != nil {
		t.Fatal(err)
	}
	return p, r, rel
}
func TestFrozenPolicyThresholdsAndStructuredDecisionPersistence(t *testing.T) {
	s, pth := openTest(t)
	ctx := context.Background()
	p, r, rel := releaseFixture(t, s)
	var changed ReleasePolicy
	if err := json.Unmarshal([]byte(strings.Replace(policyFixture, `"required":1`, `"required":2`, 1)), &changed); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SavePolicy(ctx, changed); err == nil {
		t.Fatal("changed frozen numeric threshold accepted under same policy ID")
	}
	req := ReleaseTransition{ReleaseID: rel.ID, ExpectedRevision: rel.Revision, Command: "advance", Actor: "user", ReasonCode: "CONFIRMED", EvidenceHash: r.Hash, Decision: GateDecision{Allowed: true, PolicyHash: p.Hash, EvidenceHash: r.Hash}, Idempotency: Idempotency{Key: "empty"}}
	if _, err := s.ApplyReleaseTransition(ctx, req); !errors.Is(err, ErrInsufficientEvidence) {
		t.Fatalf("Allowed boolean without per-rule results advanced: %v", err)
	}
	req.Decision = decisionFixture(t, p, r)
	req.Idempotency.Key = "structured"
	rel, err := s.ApplyReleaseTransition(ctx, req)
	if err != nil || rel.State != "SHADOW" {
		t.Fatalf("structured advance: %+v %v", rel, err)
	}
	s.Close()
	s, err = Open(pth)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	frozen, err := s.GetPolicy(ctx, p.ID)
	if err != nil || frozen.Hash != p.Hash || len(frozen.ReleaseRules) != 3 || len(frozen.RollbackRules) != 1 || frozen.RollbackRetentionNS != 604800000000000 {
		t.Fatalf("frozen policy did not survive reopen: %+v %v", frozen, err)
	}
	for _, query := range []string{"SELECT body_json FROM release_events ORDER BY id DESC LIMIT 1", "SELECT response_json FROM idempotency_keys WHERE operation='release_command' AND key='structured'"} {
		var body string
		if err = s.db.QueryRow(query).Scan(&body); err != nil {
			t.Fatal(err)
		}
		var doc map[string]json.RawMessage
		if err = json.Unmarshal([]byte(body), &doc); err != nil {
			t.Fatal(err)
		}
		if payload, ok := doc["payload"]; ok {
			if err = json.Unmarshal(payload, &doc); err != nil {
				t.Fatal(err)
			}
		}
		var decision struct {
			Rules []struct {
				ID       string          `json:"id"`
				Observed json.RawMessage `json:"observed"`
				Required json.RawMessage `json:"required"`
				Evidence []string        `json:"evidence_ids"`
				Status   string          `json:"status"`
			} `json:"rules"`
		}
		if err = json.Unmarshal(doc["decision"], &decision); err != nil || len(decision.Rules) != 3 || string(decision.Rules[2].Observed) != "true" || string(decision.Rules[0].Required) != "1" || len(decision.Rules[0].Evidence) != 1 {
			t.Fatalf("structured evidence not persisted: %s %v", body, err)
		}
	}
}
func TestPolicyAndRuleValueValidation(t *testing.T) {
	s, _ := openTest(t)
	ctx := context.Background()
	for _, raw := range []string{`{"id":"empty","mode":"simulation","version":"v1"}`, strings.Replace(policyFixture, `"required":1`, `"required":{"hidden":1}`, 1), strings.Replace(policyFixture, `"minimum_samples":1`, `"minimum_samples":-1`, 1), strings.Replace(policyFixture, `"rollback_retention_ns":604800000000000`, `"rollback_retention_ns":-1`, 1), strings.Replace(policyFixture, `"operator":"gte"`, `"operator":"arbitrary"`, 1)} {
		var p ReleasePolicy
		if err := json.Unmarshal([]byte(raw), &p); err != nil {
			continue
		}
		if _, err := s.SavePolicy(ctx, p); err == nil {
			t.Fatalf("invalid frozen policy accepted: %s", raw)
		}
	}
}
func TestManifestIdentityLevelsRequireExactIdentity(t *testing.T) {
	for _, tc := range []struct{ name, level, digest, version, config, reason string }{{"empty_digest", "digest", "", "", "", ""}, {"tag_digest", "digest", "latest", "", "", ""}, {"short_hash", "digest", "sha256:ab", "", "", ""}, {"unknown_level", "gateway_alias", "", "", "", ""}, {"declared_tag_only", "declared", "", "latest", "", ""}, {"unknown_without_reason", "unknown", "", "", "", ""}} {
		t.Run(tc.name, func(t *testing.T) {
			m := sampleManifest()
			m.Candidate.IdentityLevel = tc.level
			m.Candidate.Digest = tc.digest
			m.Candidate.DeploymentVersion = tc.version
			m.Candidate.ConfigHash = tc.config
			m.Candidate.UnknownReason = tc.reason
			if _, _, err := CanonicalManifest(m); !errors.Is(err, ErrInvalid) {
				t.Fatalf("invalid exact identity accepted: %+v %v", m.Candidate, err)
			}
		})
	}
	for _, level := range []string{"digest", "declared", "unknown"} {
		t.Run("valid_"+level, func(t *testing.T) {
			m := sampleManifest()
			m.Candidate.IdentityLevel = level
			m.Candidate.Digest = strings.Repeat("b", 64)
			if level == "declared" {
				m.Candidate.Digest = ""
				m.Candidate.DeploymentVersion = "deployment-2026-10-24.1"
				m.Candidate.ConfigHash = strings.Repeat("c", 64)
			}
			if level == "unknown" {
				m.Candidate.Digest = ""
				m.Candidate.UnknownReason = "provider exposes only mutable tag"
			}
			if _, _, err := CanonicalManifest(m); err != nil {
				t.Fatal(err)
			}
		})
	}
}
func TestManifestFixtureEvidenceInvalidatesReportAndRelease(t *testing.T) {
	for _, mode := range []string{"missing", "corrupt", "tombstone"} {
		t.Run(mode, func(t *testing.T) {
			s, _ := openTest(t)
			ctx := context.Background()
			a, err := s.PublishArtifact(ctx, []byte("case source fixture"), ArtifactMetadata{MediaType: "text/plain", Provenance: "offline_fixture"})
			if err != nil {
				t.Fatal(err)
			}
			m := sampleManifest()
			m.Dataset.Cases[0].FixtureArtifact = a.ID
			input, err := s.PublishArtifact(ctx, []byte("separate test input"), ArtifactMetadata{MediaType: "text/plain", Provenance: "offline_fixture"})
			if err != nil {
				t.Fatal(err)
			}
			m.Dataset.Cases[0].InputArtifact = input.ID
			e, err := s.CreateEvaluation(ctx, CreateEvaluationRequest{ID: "eval", Manifest: m, Idempotency: Idempotency{Key: "create-eval"}})
			if err != nil {
				t.Fatal(err)
			}
			e, err = s.ClaimEvaluation(ctx, e.ID, "run", e.Revision)
			if err != nil {
				t.Fatal(err)
			}
			appendResult(t, s, e.ID, e.LeaseGeneration, "stable", 1, "pass")
			appendResult(t, s, e.ID, e.LeaseGeneration, "candidate", 1, "pass")
			e, _ = s.GetEvaluation(ctx, e.ID)
			r, err := s.CompleteEvaluation(ctx, e.ID, e.LeaseGeneration, e.Revision)
			if err != nil {
				t.Fatal(err)
			}
			p, err := s.SavePolicy(ctx, samplePolicy())
			if err != nil {
				t.Fatal(err)
			}
			rel, err := s.CreateRelease(ctx, CreateReleaseRequest{ID: "release", WorkspaceID: "ws", WorkflowID: m.WorkflowID, FilterHash: "all", ReportID: r.ID, PolicyID: p.ID, Idempotency: Idempotency{Key: "create-release"}})
			if err != nil {
				t.Fatal(err)
			}
			rel, err = s.ApplyReleaseTransition(ctx, ReleaseTransition{ReleaseID: rel.ID, ExpectedRevision: rel.Revision, Command: "start_offline", Actor: "user", ReasonCode: "CONFIRMED", EvidenceHash: r.Hash, Idempotency: Idempotency{Key: "start"}})
			if err != nil {
				t.Fatal(err)
			}
			switch mode {
			case "missing":
				err = os.Remove(filepath.Join(s.artifactDir, a.SHA256))
			case "corrupt":
				err = os.WriteFile(filepath.Join(s.artifactDir, a.SHA256), []byte("changed source"), 0600)
			case "tombstone":
				_, err = s.db.Exec("UPDATE artifacts SET deleted_at=? WHERE id=?", now(), a.ID)
			}
			if err != nil {
				t.Fatal(err)
			}
			got, err := s.GetReport(ctx, r.ID)
			if err != nil || got.PromotionEligible || !strings.Contains(strings.Join(got.IneligibilityReasons, ","), "ARTIFACT_MISSING:"+a.ID) {
				t.Fatalf("manifest fixture lost without invalidation: %+v %v", got, err)
			}
			if _, err = s.CreateRelease(ctx, CreateReleaseRequest{ID: "bypass", WorkspaceID: "ws", WorkflowID: m.WorkflowID, FilterHash: "other", ReportID: r.ID, PolicyID: p.ID, Idempotency: Idempotency{Key: "bypass"}}); !errors.Is(err, ErrInsufficientEvidence) {
				t.Fatalf("create bypassed missing fixture: %v", err)
			}
			if _, err = s.ApplyReleaseTransition(ctx, ReleaseTransition{ReleaseID: rel.ID, ExpectedRevision: rel.Revision, Command: "advance", Actor: "user", ReasonCode: "CONFIRMED", EvidenceHash: r.Hash, Decision: decisionFixture(t, p, r), Idempotency: Idempotency{Key: "advance"}}); !errors.Is(err, ErrInsufficientEvidence) {
				t.Fatalf("advance bypassed fixture: %v", err)
			}
			if _, err = s.ApplyReleaseTransition(ctx, ReleaseTransition{ReleaseID: rel.ID, ExpectedRevision: rel.Revision, Command: "pause", Actor: "user", ReasonCode: "MISSING_EVIDENCE", Idempotency: Idempotency{Key: "pause"}}); err != nil {
				t.Fatalf("safe pause blocked: %v", err)
			}
		})
	}
}

func TestUnknownIdentityCannotSupportReleaseEvidence(t *testing.T) {
	s, _ := openTest(t)
	ctx := context.Background()
	m := sampleManifest()
	m.Candidate.IdentityLevel = "unknown"
	m.Candidate.Digest = ""
	m.Candidate.UnknownReason = "only mutable tag available"
	e, err := s.CreateEvaluation(ctx, CreateEvaluationRequest{ID: "eval", Manifest: m, Idempotency: Idempotency{Key: "unknown"}})
	if err != nil {
		t.Fatal(err)
	}
	e, err = s.ClaimEvaluation(ctx, e.ID, "run", e.Revision)
	if err != nil {
		t.Fatal(err)
	}
	appendResult(t, s, e.ID, e.LeaseGeneration, "stable", 1, "pass")
	appendResult(t, s, e.ID, e.LeaseGeneration, "candidate", 1, "pass")
	e, _ = s.GetEvaluation(ctx, e.ID)
	r, err := s.CompleteEvaluation(ctx, e.ID, e.LeaseGeneration, e.Revision)
	if err != nil {
		t.Fatal(err)
	}
	if r.PromotionEligible || !strings.Contains(strings.Join(r.IneligibilityReasons, ","), "UNKNOWN_IDENTITY") {
		t.Fatalf("unknown identity eligible: %+v", r)
	}
}
func TestStructuredRuleResultRejectsThresholdSwapAndMissingCoverage(t *testing.T) {
	s, _ := openTest(t)
	p, r, rel := releaseFixture(t, s)
	ctx := context.Background()
	for _, mode := range []string{"missing_rule", "wrong_threshold", "wrong_unit", "unknown_observation", "insufficient_samples", "insufficient_window", "failed", "missing_evidence", "duplicate"} {
		t.Run(mode, func(t *testing.T) {
			d := decisionFixture(t, p, r)
			switch mode {
			case "missing_rule":
				d.Rules = d.Rules[:1]
			case "wrong_threshold":
				d.Rules[0].Required = NumberValue(0)
			case "wrong_unit":
				d.Rules[0].Unit = "tokens"
			case "unknown_observation":
				d.Rules[0].Observed = UnknownValue()
			case "insufficient_samples":
				d.Rules[0].SampleCount = 0
			case "insufficient_window":
				d.Rules[1].ObservationWindowNS = 0
			case "failed":
				d.Rules[0].Status = "fail"
			case "missing_evidence":
				d.Rules[0].EvidenceIDs = nil
			case "duplicate":
				d.Rules[1] = d.Rules[0]
			}
			if _, err := s.ApplyReleaseTransition(ctx, ReleaseTransition{ReleaseID: rel.ID, ExpectedRevision: rel.Revision, Command: "advance", Actor: "user", ReasonCode: "CONFIRMED", EvidenceHash: r.Hash, Decision: d, Idempotency: Idempotency{Key: mode}}); !errors.Is(err, ErrInsufficientEvidence) {
				t.Fatalf("invalid structured rule evidence accepted: %v", err)
			}
		})
	}
	// String rollback criteria are frozen as scalars too; changing a criterion
	// under the same ID is an immutable snapshot conflict, not a new version name.
	other := samplePolicy()
	other.ID = "string-policy"
	other.RollbackRules[0].Required = StringValue("OUT_OF_MEMORY")
	other.RollbackRules[0].Unit = "error_code"
	stored, err := s.SavePolicy(ctx, other)
	if err != nil {
		t.Fatal(err)
	}
	other.RollbackRules[0].Required = StringValue("INFERENCE_TIMEOUT")
	if _, err = s.SavePolicy(ctx, other); err == nil {
		t.Fatal("rollback threshold overwritten")
	}
	again, err := s.GetPolicy(ctx, stored.ID)
	if err != nil || again.RollbackRules[0].Required.Text == nil || *again.RollbackRules[0].Required.Text != "OUT_OF_MEMORY" {
		t.Fatalf("string threshold: %+v %v", again, err)
	}
}

func TestLegacyV1IdentityRemainsReadableButCannotPromote(t *testing.T) {
	s, pth := openTest(t)
	ctx := context.Background()
	_, report := completeTest(t, s, "eval")
	m := sampleManifest()
	m.Candidate.Digest = "sha256:legacy-mutable-tag"
	body, hash, err := canonical(m)
	if err != nil {
		t.Fatal(err)
	}
	// This auditable temporary v1 fixture represents a record accepted by the old
	// validator. It preserves internally consistent historical hashes and report.
	if _, err = s.db.Exec("DROP TRIGGER evaluation_manifest_immutable; DROP TRIGGER reports_no_UPDATE"); err != nil {
		t.Fatal(err)
	}
	if _, err = s.db.Exec("UPDATE evaluations SET manifest_json=?,manifest_hash=? WHERE id='eval'", string(body), hash); err != nil {
		t.Fatal(err)
	}
	report.ManifestHash = hash
	reportBody, reportHash, err := canonical(report)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.db.Exec("UPDATE reports SET body_json=?,report_hash=? WHERE id=?", string(reportBody), reportHash, report.ID); err != nil {
		t.Fatal(err)
	}
	s.Close()
	s, err = Open(pth)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	e, err := s.GetEvaluation(ctx, "eval")
	if err != nil || e.Manifest.Candidate.Digest != "sha256:legacy-mutable-tag" {
		t.Fatalf("historical v1 record became unreadable: %+v %v", e, err)
	}
	r, err := s.GetReport(ctx, report.ID)
	if err != nil || r.PromotionEligible || !strings.Contains(strings.Join(r.IneligibilityReasons, ","), "INVALID_IDENTITY") {
		t.Fatalf("historical report not auditable/ineligible: %+v %v", r, err)
	}
	if _, err = s.ResumeEvaluation(ctx, e.ID, e.Revision, e.ManifestHash); !errors.Is(err, ErrInsufficientEvidence) {
		t.Fatalf("legacy invalid identity resumed: %v", err)
	}
	p, err := s.SavePolicy(ctx, samplePolicy())
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.CreateRelease(ctx, CreateReleaseRequest{ID: "release", WorkspaceID: "ws", WorkflowID: m.WorkflowID, FilterHash: "all", ReportID: r.ID, PolicyID: p.ID, Idempotency: Idempotency{Key: "legacy"}}); !errors.Is(err, ErrInsufficientEvidence) {
		t.Fatalf("legacy record used for promotion: %v", err)
	}
	var unchanged string
	if err = s.db.QueryRow("SELECT manifest_json FROM evaluations WHERE id='eval'").Scan(&unchanged); err != nil || unchanged != string(body) {
		t.Fatal("history silently rewritten")
	}
}
