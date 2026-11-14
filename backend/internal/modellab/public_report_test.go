package modellab

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/noosphere/backend/internal/gateway"
	"github.com/noosphere/backend/internal/gateway/providers"
	"github.com/noosphere/backend/internal/modelstore"
	"github.com/noosphere/backend/internal/workspaces"
)

func TestPublicReviewerCoreUsesFrozenRolesAndContextTiers(t *testing.T) {
	d, err := LoadDataset("reviewer-core-v1")
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Cases()) != 14 || d.workflowID() != ReviewerWorkflowID {
		t.Fatal("core workload changed")
	}
	for _, ref := range d.Cases() {
		e, _ := d.Evidence(ref)
		if e.ID == "paired-filter-zh" {
			for _, answer := range []struct {
				text string
				pass bool
			}{{`{"codes":["A2","C4"]}`, true}, {`{"codes":["A2","WRONG"]}`, false}, {`{"codes":["A2","C4"],"codes":["A2","C4"]}`, false}, {`{"codes":["A2","C4"]} prose`, false}} {
				r, err := scoreReviewer(t.Context(), ref, answer.text)
				if err != nil || r.Passed != answer.pass {
					t.Fatalf("strict correctness: %+v %v", r, err)
				}
			}
		}
		if e.ID == "dialogue-latest-state" {
			f, err := buildReviewerContext(t.Context(), d, ref)
			if err != nil {
				t.Fatal(err)
			}
			var messages []providers.Message
			if json.Unmarshal([]byte(f.Prompt), &messages) != nil || len(messages) != 5 || messages[0].Role != "system" || messages[2].Role != "assistant" {
				t.Fatal("frozen role history missing")
			}
		}
	}
	profile := modelstore.InferenceProfile{Requested: modelstore.Options{NumCtx: modelstore.Int(2048), MaxOutput: modelstore.Int(256)}}
	if err := validateReviewerProfile(d, profile); err != nil {
		t.Fatal(err)
	}
	long, err := LoadDataset("reviewer-context-4k-v1")
	if err != nil {
		t.Fatal(err)
	}
	if validateReviewerProfile(long, profile) == nil {
		t.Fatal("long context silently admitted at 2048")
	}
	if digest(textDatasetSource) != "625b360dfbcf17a3d3ae58517417151d007475da8ca73c401bb6895612d4ac47" || digest(textScorerSource) != "96c327e72e48653448179743e615358fbfe82a3009e2f63c5df782cb4a3814e4" || digest(textContextSource) != "87f15469be34100bc91a8b2d5c02a3862dc9fd0238612c984b5caa0ee3117409" || digest(scorerSource) != "6a679f7243e3cac32312f33cd68148fe7418b0310eb4799c38a00d55c6aaacb9" {
		t.Fatal("historical benchmark bytes changed")
	}
}

// Synthetic HTTP is explicitly offline_fixture. This public gate exercises the
// real identity-bound provider, two native protocol turns, worker and report.
func TestPublicNativeToolsExecuteSafeFeedbackAndKeepUnsupportedUnmeasured(t *testing.T) {
	for _, mode := range []string{"native", "text-imitation", "unsupported", "tool-template-unsupported", "malformed", "context-limit"} {
		t.Run(mode, func(t *testing.T) {
			dir := t.TempDir()
			weights := filepath.Join(dir, "fixture.gguf")
			if err := os.WriteFile(weights, []byte("GGUF public pure-tool fixture"), 0600); err != nil {
				t.Fatal(err)
			}
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				switch r.URL.Path {
				case "/props":
					json.NewEncoder(w).Encode(map[string]any{"model_path": weights, "build_info": "native-tool-fixture", "chat_template": "fixture", "default_generation_settings": map[string]any{"n_ctx": 2048}})
				case "/v1/models":
					w.Write([]byte(`{"data":[{"id":"fixture","owned_by":"llamacpp"}]}`))
				case "/apply-template":
					if mode == "tool-template-unsupported" {
						w.WriteHeader(400)
						w.Write([]byte(`{"error":"tools unavailable in template"}`))
						return
					}
					w.Write([]byte(`{"prompt":"synthetic public tool template"}`))
				case "/tokenize":
					tokenCount := 100
					if mode == "context-limit" {
						tokenCount = 1900
					}
					json.NewEncoder(w).Encode(map[string]any{"tokens": make([]int, tokenCount)})
				case "/v1/chat/completions":
					calls.Add(1)
					if mode == "unsupported" {
						w.WriteHeader(400)
						w.Write([]byte(`{"error":"tools unavailable"}`))
						return
					}
					if mode == "malformed" {
						w.Write([]byte(`{"choices":`))
						return
					}
					var body struct {
						Messages []providers.ToolMessage    `json:"messages"`
						Tools    []providers.ToolDefinition `json:"tools"`
						Choice   string                     `json:"tool_choice"`
						Stream   bool                       `json:"stream"`
					}
					if json.NewDecoder(r.Body).Decode(&body) != nil || len(body.Tools) != 2 || body.Stream {
						t.Error("missing actual tool protocol")
					}
					if mode == "text-imitation" {
						w.Write([]byte(`{"model":"fixture","choices":[{"message":{"content":"{\"name\":\"inventory_lookup\"}"},"finish_reason":"stop"}]}`))
						return
					}
					inventory := len(body.Messages) > 1 && strings.Contains(body.Messages[1].Content, "PENCIL")
					message := map[string]any{}
					finish := "tool_calls"
					if body.Choice == "required" {
						name, args := "convert_length", `{"value":350,"from":"cm","to":"m"}`
						if inventory {
							name, args = "inventory_lookup", `{"sku":"PENCIL"}`
						}
						message["tool_calls"] = []providers.ToolCall{{ID: "call_fixed", Type: "function", Function: providers.ToolFunction{Name: name, Arguments: args}}}
					} else {
						if body.Choice != "none" || len(body.Messages) != 4 || body.Messages[3].Role != "tool" || body.Messages[3].ToolCallID != "call_fixed" {
							t.Errorf("feedback role or association lost: %+v", body)
						}
						want, answer := `{"value":3.5,"unit":"m"}`, `{"meters":3.5}`
						if inventory {
							want, answer = `{"sku":"PENCIL","available":12}`, `{"sku":"PENCIL","available":12}`
						}
						if len(body.Messages) == 4 && body.Messages[3].Content != want {
							t.Errorf("tool feedback was invented: %q", body.Messages[3].Content)
						}
						message["content"], finish = answer, "stop"
					}
					json.NewEncoder(w).Encode(map[string]any{"model": "fixture", "choices": []any{map[string]any{"message": message, "finish_reason": finish}}})
				default:
					w.WriteHeader(404)
				}
			}))
			defer server.Close()
			config := filepath.Join(dir, "models.yaml")
			if err := os.WriteFile(config, []byte("default_model: source\nproviders:\n  - id: source\n    name: fixture\n    type: openai_compatible\n    base_url: "+server.URL+"/v1\n    model: fixture\n    max_tokens: 2048\n    enabled: true\n"), 0600); err != nil {
				t.Fatal(err)
			}
			store, err := modelstore.Open(filepath.Join(dir, "engineering.sqlite"))
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			manager, err := workspaces.Open(filepath.Join(dir, "workspace.sqlite"))
			if err != nil {
				t.Fatal(err)
			}
			lab := New(store, gateway.New(config))
			if err = lab.ConfigureRuntime(RuntimeConfig{Workspaces: manager}); err != nil {
				t.Fatal(err)
			}
			lab.runtime.fixture = true
			defer func() { lab.Stop(); manager.Close(); lab.Close() }()
			manager.RegisterReferenced("evaluation", "background", func(ctx context.Context, ws workspaces.Workspace, raw json.RawMessage, emit func(string, string, any)) (any, error) {
				var input struct {
					EvaluationID string `json:"evaluation_id"`
				}
				if err := json.Unmarshal(raw, &input); err != nil {
					return nil, err
				}
				return nil, lab.Execute(ctx, input.EvaluationID, workspaces.RunID(ctx), emit)
			}, lab.CancelRun)
			discovery, err := lab.Discover(t.Context(), "source")
			if err != nil || len(discovery.Models) != 1 {
				t.Fatalf("discovery: %v", err)
			}
			model, err := lab.Register(t.Context(), "source", "fixture", discovery.Models[0].MetadataHash)
			if err != nil {
				t.Fatal(err)
			}
			profile, err := lab.SaveProfile(t.Context(), modelstore.InferenceProfile{Requested: modelstore.Options{Temperature: modelstore.Float(0), Seed: modelstore.Int(7), NumCtx: modelstore.Int(2048), MaxOutput: modelstore.Int(256), RetryLimit: modelstore.Int(0)}})
			if err != nil {
				t.Fatal(err)
			}
			ev, err := lab.CreateEvaluation(t.Context(), "local", EvaluationSpec{EvaluationMode: "baseline", StableRevisionID: model.ID, CandidateRevisionID: model.ID, ProfileID: profile.ID, DatasetID: NativeToolsID, WorkflowID: NativeToolsWorkflowID, IdempotencyKey: "native-protocol"})
			if err != nil {
				t.Fatal(err)
			}
			deadline := time.Now().Add(30 * time.Second)
			for time.Now().Before(deadline) {
				ev, err = store.GetEvaluation(t.Context(), ev.ID)
				if err != nil {
					t.Fatal(err)
				}
				if ev.Status != "queued" && ev.Status != "running" {
					break
				}
				time.Sleep(10 * time.Millisecond)
			}
			report, err := store.LatestReport(t.Context(), ev.ID)
			if err != nil {
				t.Fatal(err)
			}
			if mode == "native" {
				if report.Counts.Pass != 4 || report.Counts.Scored != 4 || calls.Load() != 8 {
					failed, _ := store.ListAttempts(t.Context(), ev.ID)
					t.Fatalf("tool outcomes: %+v calls=%d attempts=%+v", report.Counts, calls.Load(), failed)
				}
			} else if mode == "malformed" || mode == "context-limit" {
				if report.Counts.Infra != 4 || report.Counts.Scored != 0 {
					t.Fatalf("malformed transport scored: %+v", report.Counts)
				}
				if mode == "context-limit" && calls.Load() != 0 {
					t.Fatal("oversized native tool request dispatched inference")
				}
			} else {
				if report.Counts.Scored != 0 || report.Counts.Infra != 0 || report.Counts.Interrupted != 4 {
					t.Fatalf("unsupported counted as model failure: %+v", report.Counts)
				}
			}
			attempts, err := store.ListAttempts(t.Context(), ev.ID)
			if err != nil {
				t.Fatal(err)
			}
			for _, a := range attempts {
				if mode == "native" {
					raw, err := store.ReadArtifact(t.Context(), a.OutputArtifact)
					if err != nil {
						t.Fatal(err)
					}
					var trace NativeToolTranscript
					if json.Unmarshal(raw, &trace) != nil || len(trace.Calls) != 1 || len(trace.Feedback) == 0 || trace.FinalOutput == "" {
						t.Fatal("native transcript not preserved")
					}
				} else if mode != "malformed" && mode != "context-limit" && a.Result.ErrorStage != "capability" {
					t.Fatal("unmeasured reason missing")
				}
			}
		})
	}
}

// The HTTP daemon is synthetic, but discovery, local file identity, Gateway
// admission, inference parsing, worker persistence and report selection are real.
// A terminal answer with no final text is a scored model failure; a malformed
// protocol or changed identity must remain infrastructure evidence.
func TestPublicLocalCompatibleTerminalEmptyAnswerKeepsScoredDenominator(t *testing.T) {
	for _, tc := range []struct {
		name, body, status string
		scored, infra      int
		truncated          bool
	}{
		{"reasoning-length", `{"model":"fixture","choices":[{"message":{"role":"assistant","content":"","reasoning_content":"先分析任务，但没有最终答案"},"finish_reason":"length"}]}`, "completed", 32, 0, true},
		{"reasoning-stop", `{"model":"fixture","choices":[{"message":{"role":"assistant","content":"","reasoning_content":"只有推理，没有最终答案"},"finish_reason":"stop"}]}`, "completed", 32, 0, false},
		{"wrong-content-type", `{"model":"fixture","choices":[{"message":{"content":{},"reasoning_content":"invalid protocol"},"finish_reason":"stop"}]}`, "incomplete", 0, 32, false},
		{"malformed-json", `{"model":"fixture","choices":`, "incomplete", 0, 32, false},
		{"identity-drift", `{"model":"other","choices":[{"message":{"content":"","reasoning_content":"wrong model"},"finish_reason":"length"}]}`, "interrupted", 0, 1, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			weights := filepath.Join(dir, "fixture.gguf")
			if err := os.WriteFile(weights, []byte("GGUFsynthetic public protocol test"), 0600); err != nil {
				t.Fatal(err)
			}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				switch r.URL.Path {
				case "/props":
					json.NewEncoder(w).Encode(map[string]any{"model_path": weights, "build_info": "public-fixture", "chat_template": "fixture", "default_generation_settings": map[string]any{"n_ctx": 2048}})
				case "/v1/models":
					json.NewEncoder(w).Encode(map[string]any{"data": []any{map[string]any{"id": "fixture", "owned_by": "llamacpp", "meta": map[string]any{"n_params": 100, "ftype": "Q4_K_M"}}}})
				case "/v1/chat/completions":
					w.Write([]byte(tc.body))
				default:
					w.WriteHeader(http.StatusNotFound)
				}
			}))
			t.Cleanup(server.Close)
			config := filepath.Join(dir, "models.yaml")
			if err := os.WriteFile(config, []byte("default_model: source\nproviders:\n  - id: source\n    name: fixture\n    type: openai_compatible\n    base_url: "+server.URL+"/v1\n    model: fixture\n    max_tokens: 2048\n    enabled: true\n"), 0600); err != nil {
				t.Fatal(err)
			}
			store, err := modelstore.Open(filepath.Join(dir, "engineering.sqlite"))
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { store.Close() })
			manager, err := workspaces.Open(filepath.Join(dir, "workspace.sqlite"))
			if err != nil {
				t.Fatal(err)
			}
			lab := New(store, gateway.New(config))
			if err := lab.ConfigureRuntime(RuntimeConfig{Workspaces: manager}); err != nil {
				t.Fatal(err)
			}
			lab.runtime.fixture = true
			t.Cleanup(func() { lab.Stop(); manager.Close(); lab.Close() })
			manager.RegisterReferenced("evaluation", "background", func(ctx context.Context, ws workspaces.Workspace, raw json.RawMessage, emit func(string, string, any)) (any, error) {
				var input struct {
					EvaluationID string `json:"evaluation_id"`
				}
				if err := json.Unmarshal(raw, &input); err != nil {
					return nil, err
				}
				return nil, lab.Execute(ctx, input.EvaluationID, workspaces.RunID(ctx), emit)
			}, lab.CancelRun)
			discovery, err := lab.Discover(t.Context(), "source")
			if err != nil || len(discovery.Models) != 1 {
				t.Fatalf("discovery: %+v %v", discovery, err)
			}
			model, err := lab.Register(t.Context(), "source", "fixture", discovery.Models[0].MetadataHash)
			if err != nil {
				t.Fatal(err)
			}
			profile, err := lab.SaveProfile(t.Context(), modelstore.InferenceProfile{Requested: modelstore.Options{Temperature: modelstore.Float(0), Seed: modelstore.Int(7), NumCtx: modelstore.Int(256), MaxOutput: modelstore.Int(32), RetryLimit: modelstore.Int(0)}})
			if err != nil {
				t.Fatal(err)
			}
			ev, err := lab.CreateEvaluation(t.Context(), "local", EvaluationSpec{EvaluationMode: "baseline", StableRevisionID: model.ID, CandidateRevisionID: model.ID, ProfileID: profile.ID, DatasetID: TextDatasetID, WorkflowID: TextWorkflowID, IdempotencyKey: "protocol-test"})
			if err != nil {
				t.Fatal(err)
			}
			deadline := time.Now().Add(45 * time.Second)
			for time.Now().Before(deadline) {
				ev, err = store.GetEvaluation(t.Context(), ev.ID)
				if err != nil {
					t.Fatal(err)
				}
				if ev.Status != "queued" && ev.Status != "running" {
					break
				}
				time.Sleep(10 * time.Millisecond)
			}
			attempts, err := store.ListAttempts(t.Context(), ev.ID)
			if err != nil || ev.Status != tc.status {
				t.Fatalf("status=%s want=%s attempts=%d err=%v", ev.Status, tc.status, len(attempts), err)
			}
			if tc.name == "identity-drift" {
				if len(attempts) != 1 || attempts[0].State != "failed" || attempts[0].Verdict != "infra_error" || attempts[0].ErrorCode != "MODEL_DRIFT" {
					t.Fatalf("identity drift scored: %+v", attempts)
				}
				return
			}
			report, err := store.LatestReport(t.Context(), ev.ID)
			if err != nil || report.Counts.Scored != tc.scored || report.Counts.Fail != tc.scored || report.Counts.Infra != tc.infra || report.Counts.Pass != 0 {
				t.Fatalf("model output changed correctness denominator: %+v %v", report.Counts, err)
			}
			if tc.scored == 0 {
				return
			}
			for _, attempt := range attempts {
				if attempt.State != "completed" || attempt.Verdict != "invalid_output" || attempt.Result.Truncated != tc.truncated || attempt.OutputArtifact == "" {
					t.Fatalf("terminal empty answer misclassified: %+v", attempt)
				}
				preserved := false
				for _, id := range attempt.Result.EvidenceArtifacts {
					raw, err := store.ReadArtifact(t.Context(), id)
					if err != nil {
						t.Fatal(err)
					}
					var phase phaseEvidence
					if json.Unmarshal(raw, &phase) == nil && phase.Kind == "formal" && phase.Response != nil && phase.Response.Thinking != "" && phase.Response.Output == "" && phase.ErrorCode == "" {
						preserved = true
					}
				}
				if !preserved {
					t.Fatal("raw reasoning/terminal response not preserved")
				}
			}
		})
	}
}

// This public gate needs no model server, Go compiler, or CodeLens worker.
// It covers the shipped source identity, actual content scoring, strict JSON,
// frozen prompts, and per-workflow readiness using the production components.
func TestPublicLocalTextSuiteScoresContentAndRequiresNoGo(t *testing.T) {
	d, err := LoadDataset(TextDatasetID)
	if err != nil {
		t.Fatal(err)
	}
	revision := d.Revision()
	if len(revision.Cases) != 16 || revision.SourceRevisionID != revision.ID || revision.SourceContentHash != revision.ContentHash || revision.ScorerHash == "" || revision.PromptTemplateHash == "" {
		t.Fatalf("missing reproducible text identity: %+v", revision)
	}
	counts := map[string]int{}
	refs := map[string]CaseRef{}
	for _, ref := range d.Cases() {
		refs[ref.ID()] = ref
		e, err := d.Evidence(ref)
		if err != nil {
			t.Fatal(err)
		}
		counts[e.Category]++
		frozen, err := buildTextContext(t.Context(), d, ref)
		if err != nil || validateFrozen(d, ref, frozen) != nil || frozen.Public.CodeLensVersion != "" {
			t.Fatalf("text prompt required code analysis: %+v %v", frozen, err)
		}
	}
	if counts["chinese_instruction"] != 8 || counts["structured_json"] != 8 {
		t.Fatal(counts)
	}
	for _, tc := range []struct{ id, output, status, code string }{
		{"cn-filter-sort", "2,4", "passed", ""},
		{"cn-filter-sort", "2,4,9", "failed", "WRONG_ANSWER"},
		{"json-inventory", `{"paid":false,"quantity":3,"item":"笔记本"}`, "passed", ""},
		{"json-inventory", `{"paid":false,"quantity":4,"item":"笔记本"}`, "failed", "WRONG_ANSWER"},
		{"json-inventory", `{"paid":false,"quantity":3,"item":"笔记本","quantity":3}`, "failed", "INVALID_OUTPUT"},
		{"json-aggregate", `{"total":17,"by_type":{"book":3,"pen":0,"pen":5}}`, "failed", "INVALID_OUTPUT"},
		{"json-inventory", `{"paid":false,"quantity":3,"item":"笔记本"} explanation`, "failed", "INVALID_OUTPUT"},
		{"json-inventory", `{"paid":false,"quantity":`, "failed", "INVALID_OUTPUT"},
	} {
		result, err := scoreText(t.Context(), refs[tc.id], tc.output)
		if err != nil || result.Status != tc.status || result.ErrorCode != tc.code || len(result.Rules) != 2 || result.Passed != (tc.status == "passed") {
			t.Fatalf("wrong deterministic score: %+v %v", result, err)
		}
	}
	lab := &Service{runtime: &evaluationRuntime{config: RuntimeConfig{Budget: DefaultBudget(), CollectEnvironment: func(context.Context) (modelstore.EnvironmentSnapshot, error) {
		return modelstore.EnvironmentSnapshot{OS: "test", Arch: "test", CollectorVersion: "public-test"}, nil
	}}}}
	if err := lab.runtime.requireDatasetRuntime(d); err != nil {
		t.Fatalf("text suite requires Go: %v", err)
	}
	mux := http.NewServeMux()
	lab.RegisterRoutes(mux, nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest("GET", "/api/model-lab/environment", nil))
	var result struct {
		Preflight struct {
			Available bool `json:"available"`
			Workflows map[string]struct {
				Available bool `json:"available"`
			} `json:"workflows"`
		} `json:"preflight"`
	}
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &result) != nil || result.Preflight.Available || !result.Preflight.Workflows[TextWorkflowID].Available || result.Preflight.Workflows[DatasetWorkflowID].Available {
		t.Fatalf("wrong workflow prerequisites: %d %s", w.Code, w.Body.String())
	}
}

// 输入记录明确使用 offline_fixture；测试执行真实 SQLite、artifact 和报告计算。
func publicReportRecords(t *testing.T, incomplete bool, unavailableMetric string) (*Service, modelstore.Evaluation) {
	t.Helper()
	ctx := context.Background()
	store, err := modelstore.Open(filepath.Join(t.TempDir(), "reports.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	hash := func(value string) string {
		sum := sha256.Sum256([]byte(value))
		return hex.EncodeToString(sum[:])
	}
	publish := func(content string) modelstore.Artifact {
		artifact, err := store.PublishArtifact(ctx, []byte(content), modelstore.ArtifactMetadata{MediaType: "text/plain", Provenance: "offline_fixture"})
		if err != nil {
			t.Fatal(err)
		}
		return artifact
	}
	input, output := publish("报告聚合输入记录"), publish("报告聚合输出记录")
	m := modelstore.Manifest{
		SchemaVersion: 1, EncoderVersion: modelstore.EncoderVersion,
		WorkspaceID: "public-workspace", WorkflowID: "codelens.codetask.go.v1",
		Source: "offline_fixture", MemoryMode: "not_applicable",
		Stable:      modelstore.ModelRevision{ID: "stable", GatewayModelID: "local", Provider: "ollama", ModelName: "example:1", IdentityLevel: "digest", Digest: hash("version-one")},
		Candidate:   modelstore.ModelRevision{ID: "candidate", GatewayModelID: "local", Provider: "ollama", ModelName: "example:2", IdentityLevel: "digest", Digest: hash("version-two")},
		Profile:     modelstore.InferenceProfile{ID: "profile"},
		Dataset:     modelstore.DatasetRevision{ID: "dataset", BenchmarkID: "go", Version: "v1", ContentHash: hash("dataset"), ScorerVersion: "host-v1"},
		Environment: modelstore.EnvironmentSnapshot{ID: "environment", OS: "windows", Arch: "amd64", CollectorVersion: "v1"},
	}
	for i := 0; i < 12; i++ {
		m.Dataset.Cases = append(m.Dataset.Cases, modelstore.Case{ID: fmt.Sprintf("case%02d", i), Category: "logic", InputArtifact: input.ID, Expected: "42"})
	}
	evaluation, err := store.CreateEvaluation(ctx, modelstore.CreateEvaluationRequest{ID: "evaluation", Manifest: m, Idempotency: modelstore.Idempotency{Key: "create"}})
	if err != nil {
		t.Fatal(err)
	}
	evaluation, err = store.ClaimEvaluation(ctx, evaluation.ID, "run", evaluation.Revision)
	if err != nil {
		t.Fatal(err)
	}
	for i, cs := range m.Dataset.Cases {
		for _, arm := range []string{"stable", "candidate"} {
			if incomplete && i == 11 && arm == "candidate" {
				continue
			}
			a := modelstore.Attempt{
				ID: cs.ID + "-" + arm, EvaluationID: evaluation.ID, CaseID: cs.ID, Arm: arm,
				AttemptNo: 1, LeaseGeneration: evaluation.LeaseGeneration,
				State: "completed", Verdict: "pass", InputArtifact: input.ID, OutputArtifact: output.ID,
				Result: modelstore.Result{InferenceCompleted: true}, Metrics: emptyMetrics(),
			}
			a.Metrics.WallNS = observedMetric(float64(i+1)*100, "ns", "host_monotonic")
			if arm == "candidate" {
				a.Metrics.WallNS.Value = modelstore.Float(float64(12-i) * 100)
			}
			a.Metrics.LoadingProtocol = LoadingProtocol
			a.Metrics.WarmVerified = true
			a.Metrics.WarmupArtifact = publish("预热记录 " + a.ID).ID
			if (i == 0 && arm == "candidate") || i == 2 {
				a.Verdict = "compile_fail"
			}
			if i == 1 && arm == "stable" {
				a.Verdict = "functional_fail"
			}
			if incomplete && arm == "candidate" {
				switch i {
				case 8:
					a.State, a.Verdict, a.ErrorCode = "failed", "infra_error", "INFERENCE_TIMEOUT"
					a.Result.InferenceCompleted = false
				case 9:
					a.State, a.Verdict = "cancelled", "not_scored"
					a.Result.InferenceCompleted = false
				case 10:
					a.State, a.Verdict = "interrupted", "not_scored"
					a.Result.InferenceCompleted = false
				}
			}
			if i == 0 && arm == "candidate" {
				switch unavailableMetric {
				case "missing":
					a.Metrics.WallNS = unknownMetric("ns", "未采集")
				case "zero":
					a.Metrics.WallNS.Value = modelstore.Float(0)
				case "wrong_unit":
					a.Metrics.WallNS.Unit = "ms"
				case "incomplete":
					a.Result.InferenceCompleted = false
				}
			}
			if _, err := store.AppendAttempt(ctx, a); err != nil {
				t.Fatal(err)
			}
		}
	}
	return New(store, nil), evaluation
}

func publicBuildReport(t *testing.T, service *Service, evaluation modelstore.Evaluation) (modelstore.Report, Comparison) {
	t.Helper()
	report, err := service.BuildReport(context.Background(), evaluation.ID)
	if err != nil {
		t.Fatal(err)
	}
	comparison, err := ReadComparison(report)
	if err != nil {
		t.Fatal(err)
	}
	return report, comparison
}

func TestPublicReportAggregatesScoredAndIncompleteAttempts(t *testing.T) {
	for _, incomplete := range []bool{false, true} {
		t.Run(fmt.Sprintf("incomplete_%t", incomplete), func(t *testing.T) {
			service, evaluation := publicReportRecords(t, incomplete, "")
			report, comparison := publicBuildReport(t, service, evaluation)
			if report.Counts.Planned != 24 || report.Counts.Fail != 4 || comparison.Source != "offline_fixture" {
				t.Fatalf("报告总数或来源不正确: %+v", report)
			}
			wantScored, wantPairs, wantFailures, wantStatus := 24, 12, 4, "complete"
			wantMatrix := [4]int{9, 1, 1, 1}
			if incomplete {
				wantScored, wantPairs, wantFailures, wantStatus = 20, 8, 8, "incomplete"
				wantMatrix = [4]int{5, 1, 1, 1}
				if report.Counts.Infra != 1 || report.Counts.Cancelled != 1 || report.Counts.Interrupted != 1 || report.Counts.Unknown != 1 || report.PromotionEligible {
					t.Fatalf("未完成记录被计入已评分结果: %+v", report.Counts)
				}
			}
			if report.Counts.Scored != wantScored || report.Counts.PairedComplete != wantPairs || report.PairMatrix != wantMatrix || len(comparison.Failures) != wantFailures || comparison.Status != wantStatus {
				t.Fatalf("聚合计数不正确: report=%+v comparison=%+v", report.Counts, comparison)
			}
			if comparison.Loss.N != wantPairs || comparison.Loss.Rate == nil || *comparison.Loss.Rate != 1/float64(wantPairs) || comparison.Categories["logic"].PairMatrix != wantMatrix {
				t.Fatalf("成对样本的分母不正确: %+v", comparison)
			}
			if comparison.Completion.Rate == nil || *comparison.Completion.Rate != float64(wantScored)/24 {
				t.Fatalf("完成比例不正确: %+v", comparison.Completion)
			}
			coverage := comparison.MetricCoverage["candidate"]["available_vram_bytes"]
			if coverage.Samples != 0 || coverage.Missing != 12 || len(coverage.MissingReasons) == 0 {
				t.Fatalf("未知显存被当作有效数值: %+v", coverage)
			}
			stored, err := service.store.GetReport(context.Background(), report.ID)
			if err != nil || stored.Hash != report.Hash || string(stored.Analysis) != string(report.Analysis) {
				t.Fatalf("报告保存后内容不一致: %v", err)
			}
		})
	}
}

func TestPublicReportLatencyRequiresCompleteComparablePairs(t *testing.T) {
	for _, unavailable := range []string{"", "missing", "zero", "wrong_unit", "incomplete"} {
		t.Run("metric_"+unavailable, func(t *testing.T) {
			service, evaluation := publicReportRecords(t, false, unavailable)
			_, comparison := publicBuildReport(t, service, evaluation)
			performance := comparison.Performance
			if unavailable == "" {
				want := (6.0/7 + 7.0/6) / 2
				if performance.Status != "sufficient" || performance.SampleCount != 12 || performance.MedianRatio.Value == nil || math.Abs(*performance.MedianRatio.Value-want) > 1e-12 {
					t.Fatalf("应计算十二组成对比值的中位数: %+v", performance)
				}
			} else if performance.Status != "insufficient" || performance.SampleCount != 11 || performance.MedianRatio.Value != nil || len(performance.Reasons) == 0 {
				t.Fatalf("不完整指标不能产生总体性能比值: %+v", performance)
			}
		})
	}
}

func TestPublicReportRetryUsesLatestAttemptAndPreservesHistory(t *testing.T) {
	ctx := context.Background()
	service, evaluation := publicReportRecords(t, false, "")
	original, _ := publicBuildReport(t, service, evaluation)
	current, err := service.store.GetEvaluation(ctx, evaluation.ID)
	if err != nil {
		t.Fatal(err)
	}
	current, err = service.store.ResumeEvaluation(ctx, current.ID, current.Revision, current.ManifestHash)
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.store.AppendAttempt(ctx, modelstore.Attempt{
		ID: "retry", EvaluationID: current.ID, CaseID: "case03", Arm: "candidate",
		AttemptNo: 2, LeaseGeneration: current.LeaseGeneration,
		State: "interrupted", Verdict: "not_scored", InputArtifact: current.Manifest.Dataset.Cases[3].InputArtifact,
	})
	if err != nil {
		t.Fatal(err)
	}
	updated, comparison := publicBuildReport(t, service, current)
	if updated.Counts.Scored != 23 || updated.Counts.PairedComplete != 11 || len(updated.Superseded) != 1 || comparison.Arms["candidate"].N != 11 || comparison.Status != "incomplete" {
		t.Fatalf("重试记录没有取代原样本: %+v", updated)
	}
	selectedRetry := false
	for _, attempt := range updated.Selected {
		if attempt.ID == "retry" {
			selectedRetry = true
		}
		if attempt.ID == "case03-candidate" {
			t.Fatal("旧的成功记录仍被选中")
		}
	}
	if !selectedRetry {
		t.Fatal("新报告缺少当前重试记录")
	}
	history, err := service.store.GetReport(ctx, original.ID)
	if err != nil || history.Hash != original.Hash || string(history.Analysis) != string(original.Analysis) || history.PromotionEligible {
		t.Fatalf("历史报告内容改变或仍可晋级: %+v %v", history, err)
	}
	all, err := service.store.ListAttempts(ctx, current.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Compare(current, original, all, nil); !errors.Is(err, modelstore.ErrInvalid) {
		t.Fatalf("不能用旧选择集合重新计算当前报告: %v", err)
	}
}
