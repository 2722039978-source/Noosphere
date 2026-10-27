package providers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// All model data and generated output here are HTTP fixtures, never real model scores.
type ollamaFixture struct {
	calls       int
	showCalls   int
	template    string
	drift       bool
	chatStatus  int
	chatBody    string
	seen        map[string]any
	cloud       bool
	versionBody string
	tagsBody    string
	showBody    string
}

func fixtureProvider(t *testing.T, f *ollamaFixture) EvaluationProvider {
	t.Helper()
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/version":
			if f.versionBody != "" {
				fmt.Fprint(w, f.versionBody)
				return
			}
			fmt.Fprint(w, `{"version":"0.fixture"}`)
		case "/api/tags":
			if f.tagsBody != "" {
				fmt.Fprint(w, f.tagsBody)
				return
			}
			fmt.Fprintf(w, `{"models":[{"name":"fixture:latest","digest":"sha256:%s","size":123,"details":{"format":"gguf","family":"fixture","parameter_size":"1B","quantization_level":"Q4"}}]}`, strings.Repeat("a", 64))
		case "/api/show":
			if f.showBody != "" {
				fmt.Fprint(w, f.showBody)
				return
			}
			f.showCalls++
			template := f.template
			if f.drift && f.showCalls > 1 {
				template += "changed"
			}
			remote := ""
			if f.cloud {
				remote = `,"remote_host":"https://remote.invalid"`
			}
			fmt.Fprintf(w, `{"template":%q,"parameters":"temperature 0","model_info":{"general.architecture":"fixture"},"details":{"format":"gguf","family":"fixture","parameter_size":"1B","quantization_level":"Q4"},"capabilities":["completion"]%s}`, template, remote)
		case "/api/chat", "/api/generate":
			f.calls++
			f.seen = nil
			json.NewDecoder(r.Body).Decode(&f.seen)
			if r.Header.Get("Authorization") != "Bearer fixture-secret" {
				t.Error("configured credential missing")
			}
			if f.chatStatus != 0 {
				w.WriteHeader(f.chatStatus)
			}
			if f.chatBody != "" {
				fmt.Fprint(w, f.chatBody)
			} else {
				fmt.Fprint(w, `{"model":"fixture:latest","done":true,"done_reason":"stop","message":{"role":"assistant","content":"fixture answer"},"total_duration":123,"eval_count":2}`)
			}
		default:
			t.Errorf("unexpected path: %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(s.Close)
	p, err := NewOllama(s.URL+"/v1", "fixture-secret")
	if err != nil {
		t.Fatal(err)
	}
	return p.(EvaluationProvider)
}
func fixtureRequest(t *testing.T, p EvaluationProvider) EvaluationRequest {
	t.Helper()
	d, err := p.Discover(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	m := d.Models[0]
	return EvaluationRequest{ModelName: m.Name, ExpectedDigest: m.Digest, ExpectedMetadataHash: m.MetadataHash, ExpectedEngineVersion: m.EngineVersion, Messages: []Message{{Role: "user", Content: "fixture prompt"}}, Timeout: time.Second}
}
func evaluationCode(err error) string {
	var e *EvaluationError
	if errors.As(err, &e) {
		return e.Code
	}
	return ""
}
func TestDiscoverTagsShowVersionAndDigest(t *testing.T) {
	f := &ollamaFixture{}
	p := fixtureProvider(t, f)
	d, err := p.Discover(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	m := d.Models[0]
	if d.EngineVersion != "0.fixture" || !m.LocalVerified || m.Digest != strings.Repeat("a", 64) || len(m.MetadataHash) != 64 || len(m.ConfigHash) != 64 || len(m.TemplateHash) != 64 {
		t.Fatalf("unexpected identity: %+v", m)
	}
	f.cloud = true
	d, err = p.Discover(context.Background())
	if err != nil || d.Models[0].LocalVerified || d.Models[0].IneligibilityReason == "" {
		t.Fatalf("cloud metadata accepted: %+v %v", d, err)
	}
}
func TestExplicitZeroTemperatureAndSeedPreserved(t *testing.T) {
	f := &ollamaFixture{}
	p := fixtureProvider(t, f)
	req := fixtureRequest(t, p)
	zero := 0.0
	seed := int64(0)
	count := 0
	no := false
	req.Options = EvaluationOptions{Temperature: &zero, Seed: &seed, MaxOutputTokens: &count, Think: &no}
	r, err := p.Evaluate(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	opts := f.seen["options"].(map[string]any)
	if opts["temperature"] != float64(0) || opts["seed"] != float64(0) || opts["num_predict"] != float64(0) || f.seen["think"] != false || f.seen["stream"] != false || r.Output != "fixture answer" || r.LoadDurationNS != nil || r.EvalCount == nil || *r.EvalCount != 2 {
		t.Fatalf("lost options/metrics: %v %+v", f.seen, r)
	}
	// A plain name may resolve to the verified :latest alias. A length finish
	// retains useful text while marking truncation; reported zero differs from omitted.
	req.ModelName = "fixture"
	f.chatBody = `{"model":"fixture:latest","done":true,"done_reason":"length","message":{"content":"fixture-secret partial"},"eval_count":0,"fixture-secret":"fixture-secret"}`
	r, err = p.Evaluate(context.Background(), req)
	if err != nil || !r.Truncated || r.EvalCount == nil || *r.EvalCount != 0 || r.LoadDurationNS != nil || strings.Contains(r.Output, "fixture-secret") || strings.Contains(string(r.RawResponse), "fixture-secret") {
		t.Fatalf("truncation/redaction: %+v %v", r, err)
	}
	req.Kind = "warmup"
	req.Messages = nil
	f.chatBody = `{"model":"fixture:latest","done":true,"done_reason":"load","response":""}`
	r, err = p.Evaluate(context.Background(), req)
	if err != nil || r.Output != "" || f.seen["prompt"] != "" || f.seen["messages"] != nil {
		t.Fatalf("warmup: %v %+v", f.seen, err)
	}
}
func TestEvaluationHasNoHiddenRetry(t *testing.T) {
	for _, tc := range []struct {
		name, body, code string
		status           int
	}{
		{"oom", `{"error":"model requires more system memory; out of memory"}`, "OUT_OF_MEMORY", 500},
		{"unknown500", `{"error":"fixture-secret opaque runner error"}`, "RESOURCE_EXHAUSTION_UNKNOWN", 500},
		{"malformed", `{"done":`, "INVALID_OUTPUT", 0},
		{"notdone", `{"model":"fixture:latest","done":false}`, "INVALID_OUTPUT", 0},
		{"negative", `{"model":"fixture:latest","done":true,"message":{"content":"x"},"eval_count":-1}`, "INVALID_OUTPUT", 0},
		{"unknownmetric", `{"model":"fixture:latest","done":true,"message":{"content":"x"},"eval_count":"unknown"}`, "INVALID_OUTPUT", 0},
		{"wrongtype", `{"model":"fixture:latest","done":true,"message":{"content":"x","thinking":42}}`, "INVALID_OUTPUT", 0},
		{"toolonly", `{"model":"fixture:latest","done":true,"message":{"tool_calls":[{}]}}`, "UNSUPPORTED_CAPABILITY", 0},
		{"remote", `{"model":"fixture:latest","done":true,"remote_model":"cloud","message":{"content":"x"}}`, "UNSAFE_ENDPOINT", 0},
		{"wrongmodel", `{"model":"other","done":true,"message":{"content":"x"}}`, "MODEL_DRIFT", 0},
		{"oversize", strings.Repeat("x", 4*1024*1024+1), "RESPONSE_TOO_LARGE", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := &ollamaFixture{chatStatus: tc.status, chatBody: tc.body}
			p := fixtureProvider(t, f)
			req := fixtureRequest(t, p)
			_, err := p.Evaluate(context.Background(), req)
			if evaluationCode(err) != tc.code || f.calls != 1 || strings.Contains(err.Error(), "fixture-secret") {
				t.Fatalf("calls=%d err=%v", f.calls, err)
			}
		})
	}
}
func TestEvaluationDriftAndCancellation(t *testing.T) {
	f := &ollamaFixture{}
	p := fixtureProvider(t, f)
	req := fixtureRequest(t, p)
	f.template = "changed"
	_, err := p.Evaluate(context.Background(), req)
	if evaluationCode(err) != "MODEL_DRIFT" || f.calls != 0 {
		t.Fatalf("pre-drift: %v", err)
	}
	f.template = ""
	f.showCalls = 0
	f.drift = true
	r, err := p.Evaluate(context.Background(), req)
	if evaluationCode(err) != "MODEL_DRIFT" || r == nil || r.Output != "fixture answer" {
		t.Fatalf("post drift lost evidence: %+v %v", r, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = p.Evaluate(ctx, req)
	if evaluationCode(err) != "USER_CANCELLED" {
		t.Fatalf("cancel: %v", err)
	}
	req.ExpectedDigest = ""
	_, err = p.Evaluate(context.Background(), req)
	if evaluationCode(err) != "INVALID_REQUEST" {
		t.Fatalf("missing expected: %v", err)
	}
}
func TestNativeLocalBoundaryAndErrors(t *testing.T) {
	for _, url := range []string{"https://example.com/v1", "http://127.0.0.1/other", "http://user:secret@localhost/v1", "http://localhost/v1?q=x", "http://localhost/v1#x", "http://localhost/v1#", "http://localhost:/v1", "http://localhost/%76%31"} {
		p, _ := NewOllama(url, "")
		_, err := p.(EvaluationProvider).Discover(context.Background())
		if evaluationCode(err) != "UNSAFE_ENDPOINT" {
			t.Fatalf("%s: %v", url, err)
		}
	}
	for _, tc := range []struct {
		name    string
		handler http.HandlerFunc
		code    string
		timeout time.Duration
	}{
		{"redirect", func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, "http://example.com", 302) }, "UNSAFE_ENDPOINT", time.Second},
		{"timeout", func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() }, "INFERENCE_TIMEOUT", 20 * time.Millisecond},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := httptest.NewServer(tc.handler)
			defer s.Close()
			p, _ := NewOllama(s.URL, "")
			ctx, cancel := context.WithTimeout(context.Background(), tc.timeout)
			defer cancel()
			_, err := p.(EvaluationProvider).Discover(ctx)
			if evaluationCode(err) != tc.code {
				t.Fatalf("%v", err)
			}
		})
	}
	s := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	url := s.URL
	s.Close()
	p, _ := NewOllama(url, "")
	_, err := p.(EvaluationProvider).Discover(context.Background())
	if evaluationCode(err) != "PROVIDER_UNAVAILABLE" {
		t.Fatal(err)
	}
	// Existing local endpoints stay local even with an invalid proxy environment.
	t.Setenv("HTTP_PROXY", "http://192.0.2.1:1")
	t.Setenv("HTTPS_PROXY", "http://192.0.2.1:1")
	f := &ollamaFixture{}
	p = fixtureProvider(t, f).(LLMProvider)
	_, err = p.(EvaluationProvider).Discover(context.Background())
	if err != nil {
		t.Fatalf("proxy environment used: %v", err)
	}
}

func TestDiscoveryFailsClosedWithoutHidingLocalCandidates(t *testing.T) {
	for _, tc := range []struct {
		name     string
		f        ollamaFixture
		code     string
		eligible bool
	}{
		{"missing-version", ollamaFixture{versionBody: `{}`}, "INVALID_OUTPUT", false},
		{"missing-digest", ollamaFixture{tagsBody: `{"models":[{"name":"fixture:latest","size":123}]}`}, "", false},
		{"missing-local-metadata", ollamaFixture{showBody: `{"details":{"format":"gguf"}}`}, "", false},
		{"malformed-show", ollamaFixture{showBody: `{"template":`}, "", false},
		{"mixed-local-cloud", ollamaFixture{tagsBody: fmt.Sprintf(`{"models":[{"name":"fixture:latest","digest":"%s","size":123},{"name":"fixture-cloud:latest","remote_model":"remote","digest":"%s","size":123}]}`, strings.Repeat("a", 64), strings.Repeat("b", 64))}, "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := fixtureProvider(t, &tc.f)
			d, err := p.Discover(context.Background())
			if evaluationCode(err) != tc.code {
				t.Fatalf("%+v %v", d, err)
			}
			if err == nil && (len(d.Models) == 0 || d.Models[0].LocalVerified != tc.eligible) {
				t.Fatalf("%+v", d)
			}
			if tc.name == "mixed-local-cloud" && (len(d.Models) != 2 || d.Models[1].LocalVerified) {
				t.Fatalf("cloud hid local model: %+v", d)
			}
		})
	}
}
