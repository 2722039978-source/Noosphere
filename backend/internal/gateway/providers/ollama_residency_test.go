package providers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// These tests exercise real local HTTP, never a live Ollama daemon.
type residencyFixture struct {
	ps            string
	releaseBody   string
	releaseStatus int
	afterPS       string
	psCalls       int
	releases      int
	seen          map[string]any
	template      string
	postTemplate  string
}

func residencyProvider(t *testing.T, f *residencyFixture) (*OllamaProvider, EvaluationRequest) {
	t.Helper()
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Header.Get("Authorization") != "Bearer fixture-secret" {
			t.Error("native request lost credentials")
		}
		switch r.URL.Path {
		case "/api/version":
			fmt.Fprint(w, `{"version":"0.fixture"}`)
		case "/api/tags":
			fmt.Fprintf(w, `{"models":[{"name":"fixture:latest","digest":"sha256:%s","size":123,"details":{"format":"gguf"}}]}`, strings.Repeat("a", 64))
		case "/api/show":
			fmt.Fprintf(w, `{"template":%q,"model_info":{"general.architecture":"fixture"},"details":{"format":"gguf"}}`, f.template)
		case "/api/ps":
			if r.Method != http.MethodGet {
				t.Error("ps must be GET")
			}
			f.psCalls++
			if f.releases > 0 && f.afterPS != "" {
				fmt.Fprint(w, f.afterPS)
			} else {
				fmt.Fprint(w, f.ps)
			}
		case "/api/generate":
			if r.Method != http.MethodPost {
				t.Error("release must be POST")
			}
			f.releases++
			if f.postTemplate != "" {
				f.template = f.postTemplate
			}
			d := json.NewDecoder(r.Body)
			d.UseNumber()
			if err := d.Decode(&f.seen); err != nil {
				t.Error(err)
			}
			if f.releaseStatus != 0 {
				w.WriteHeader(f.releaseStatus)
			}
			if f.releaseBody != "" {
				fmt.Fprint(w, f.releaseBody)
			} else {
				fmt.Fprint(w, `{"model":"fixture:latest","done":true,"done_reason":"unload","response":""}`)
			}
		default:
			t.Errorf("unexpected native path %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(s.Close)
	p, err := NewOllama(s.URL+"/v1", "fixture-secret")
	if err != nil {
		t.Fatal(err)
	}
	native := p.(*OllamaProvider)
	return native, fixtureRequest(t, native)
}

func residencyCall(t *testing.T, p *OllamaProvider, req EvaluationRequest, release bool) (ModelResidencySnapshot, error) {
	t.Helper()
	capability, ok := any(p).(EvaluationResidencyProvider)
	if !ok {
		t.Fatal("native Ollama lacks the optional residency and release capability")
	}
	if release {
		return capability.ReleaseModel(context.Background(), req)
	}
	return capability.Residency(context.Background(), req)
}

func TestOllamaResidencyCapability(t *testing.T) {
	f := &residencyFixture{ps: `{"models":[{"name":"fixture:latest","model":"fixture:latest","digest":"sha256:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA","size":0,"size_vram":0,"context_length":8192,"note":"fixture-secret"}]}`}
	p, req := residencyProvider(t, f)
	req.ModelName = "fixture"
	req.Messages = nil
	req.Kind = ""
	before := time.Now()
	got, err := residencyCall(t, p, req, false)
	if err != nil {
		t.Fatal(err)
	}
	if !got.TargetResident || len(got.Models) != 1 || got.Models[0].Name != "fixture:latest" || got.Models[0].Digest != strings.Repeat("a", 64) || got.Models[0].SizeBytes == nil || *got.Models[0].SizeBytes != 0 || got.Models[0].SizeVRAMBytes == nil || *got.Models[0].SizeVRAMBytes != 0 || got.ObservedAt.Before(before) || strings.Contains(string(got.RawResponse), "fixture-secret") || !json.Valid(got.RawResponse) {
		t.Fatalf("invalid residency: %+v", got)
	}
	f.ps = `{"models":[{"model":"fixture","digest":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}]}`
	got, err = residencyCall(t, p, req, false)
	if err != nil || got.Models[0].SizeBytes != nil || got.Models[0].SizeVRAMBytes != nil {
		t.Fatalf("missing allocation became zero: %+v %v", got, err)
	}
	f.ps = `{"models":[]}`
	got, err = residencyCall(t, p, req, false)
	if err != nil || got.TargetResident || got.Models == nil || len(got.Models) != 0 {
		t.Fatalf("absence: %+v %v", got, err)
	}
}

func TestOllamaResidencyRejectsUnverifiablePS(t *testing.T) {
	const digest = `"digest":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"`
	for _, tc := range []struct{ name, body, code string }{
		{"missing", `{}`, "INVALID_OUTPUT"}, {"null", `{"models":null}`, "INVALID_OUTPUT"}, {"wronglist", `{"models":{}}`, "INVALID_OUTPUT"}, {"malformed", `{"models":`, "INVALID_OUTPUT"},
		{"entry", `{"models":[null]}`, "INVALID_OUTPUT"}, {"name", `{"models":[{"name":"bad name",` + digest + `}]}`, "INVALID_OUTPUT"},
		{"digest", `{"models":[{"name":"fixture","digest":"short"}]}`, "INVALID_OUTPUT"},
		{"negative", `{"models":[{"name":"fixture",` + digest + `,"size":-1}]}`, "INVALID_OUTPUT"},
		{"fraction", `{"models":[{"name":"fixture",` + digest + `,"size_vram":1.5}]}`, "INVALID_OUTPUT"},
		{"string", `{"models":[{"name":"fixture",` + digest + `,"size":"0"}]}`, "INVALID_OUTPUT"},
		{"nullsize", `{"models":[{"name":"fixture",` + digest + `,"size":null}]}`, "INVALID_OUTPUT"},
		{"overflow", `{"models":[{"name":"fixture",` + digest + `,"size":9223372036854775808}]}`, "INVALID_OUTPUT"},
		{"context", `{"models":[{"name":"fixture",` + digest + `,"context_length":-1}]}`, "INVALID_OUTPUT"},
		{"duplicate", `{"models":[{"name":"fixture",` + digest + `},{"name":"fixture:latest",` + digest + `}]}`, "INVALID_OUTPUT"},
		{"digestalias", `{"models":[{"name":"fixture",` + digest + `},{"name":"other",` + digest + `}]}`, "INVALID_OUTPUT"},
		{"conflictingnames", `{"models":[{"name":"fixture","model":"other",` + digest + `}]}`, "INVALID_OUTPUT"},
		{"remote", `{"models":[{"name":"other",` + digest + `,"remote_host":"remote.invalid"}]}`, "UNSAFE_ENDPOINT"},
		{"cloud", `{"models":[{"name":"other:cloud",` + digest + `}]}`, "UNSAFE_ENDPOINT"},
		{"wrongdigest", `{"models":[{"name":"fixture","digest":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"}]}`, "MODEL_DRIFT"},
		{"oversize", strings.Repeat("x", 4*1024*1024+1), "RESPONSE_TOO_LARGE"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := &residencyFixture{ps: tc.body}
			p, req := residencyProvider(t, f)
			_, err := residencyCall(t, p, req, false)
			if evaluationCode(err) != tc.code || f.releases != 0 {
				t.Fatalf("code=%s err=%v releases=%d", tc.code, err, f.releases)
			}
		})
	}
	entries := make([]string, 129)
	for i := range entries {
		entries[i] = fmt.Sprintf(`{"name":"model%d",%s}`, i, digest)
	}
	f := &residencyFixture{ps: `{"models":[` + strings.Join(entries, ",") + `]}`}
	p, req := residencyProvider(t, f)
	if _, err := residencyCall(t, p, req, false); evaluationCode(err) != "RESPONSE_TOO_LARGE" {
		t.Fatalf("unbounded model list: %v", err)
	}
	entries = entries[:128]
	for i := range entries {
		entries[i] = fmt.Sprintf(`{"name":"other%d","digest":"%064x"}`, i, i+1)
	}
	f.ps = `{"models":[` + strings.Join(entries, ",") + `]}`
	if got, err := residencyCall(t, p, req, false); err != nil || len(got.Models) != 128 || got.TargetResident {
		t.Fatalf("valid bounded list: %+v %v", got, err)
	}
}

func TestOllamaReleaseRequiresTerminalResponseAndVerifiedAbsence(t *testing.T) {
	f := &residencyFixture{ps: `{"models":[]}`, afterPS: `{"models":[]}`}
	p, req := residencyProvider(t, f)
	got, err := residencyCall(t, p, req, true)
	if err != nil || got.TargetResident || f.releases != 1 || f.psCalls != 1 || f.seen["model"] != "fixture:latest" || f.seen["prompt"] != "" || f.seen["stream"] != false || f.seen["keep_alive"] != json.Number("0") || len(f.seen) != 4 {
		t.Fatalf("release contract: %+v %+v %v", got, f.seen, err)
	}
	for _, tc := range []struct {
		name, body, ps, code string
		status               int
	}{
		{"stillresident", "", `{"models":[{"name":"fixture","digest":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}]}`, "RELEASE_UNCONFIRMED", 0},
		{"psinvalid", "", `{}`, "RELEASE_UNCONFIRMED", 0},
		{"notdone", `{"model":"fixture","done":false}`, `{"models":[]}`, "RELEASE_UNCONFIRMED", 0},
		{"wrongmodel", `{"model":"other","done":true}`, `{"models":[]}`, "RELEASE_UNCONFIRMED", 0},
		{"remote", `{"model":"fixture","done":true,"remote_model":"other"}`, `{"models":[]}`, "RELEASE_UNCONFIRMED", 0},
		{"negative", `{"model":"fixture","done":true,"eval_count":-1}`, `{"models":[]}`, "RELEASE_UNCONFIRMED", 0},
		{"wrongdigest", `{"model":"fixture","done":true,"digest":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"}`, `{"models":[]}`, "RELEASE_UNCONFIRMED", 0},
		{"wrongtext", `{"model":"fixture","done":true,"response":12}`, `{"models":[]}`, "RELEASE_UNCONFIRMED", 0},
		{"error", `{"error":"fixture-secret server failure"}`, `{"models":[]}`, "RELEASE_UNCONFIRMED", 500},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := &residencyFixture{ps: tc.ps, afterPS: tc.ps, releaseBody: tc.body, releaseStatus: tc.status}
			p, req := residencyProvider(t, f)
			_, err := residencyCall(t, p, req, true)
			if evaluationCode(err) != tc.code || f.releases != 1 || strings.Contains(err.Error(), "fixture-secret") {
				t.Fatalf("release error=%v requests=%d", err, f.releases)
			}
		})
	}
	f = &residencyFixture{ps: `{"models":[]}`, postTemplate: "changed during unload"}
	p, req = residencyProvider(t, f)
	if _, err := residencyCall(t, p, req, true); evaluationCode(err) != "RELEASE_UNCONFIRMED" || f.releases != 1 {
		t.Fatalf("post-release drift: %v", err)
	}
}

func TestOllamaResidencyIdentityAndContextGuardRelease(t *testing.T) {
	for _, release := range []bool{false, true} {
		for _, field := range []string{"digest", "metadata", "engine", "missing"} {
			t.Run(fmt.Sprintf("%v-%s", release, field), func(t *testing.T) {
				f := &residencyFixture{ps: `{"models":[]}`}
				p, req := residencyProvider(t, f)
				switch field {
				case "digest":
					req.ExpectedDigest = strings.Repeat("b", 64)
				case "metadata":
					req.ExpectedMetadataHash = strings.Repeat("b", 64)
				case "engine":
					req.ExpectedEngineVersion = "changed"
				case "missing":
					req.ExpectedDigest = ""
				}
				_, err := residencyCall(t, p, req, release)
				code := "MODEL_DRIFT"
				if field == "missing" {
					code = "INVALID_REQUEST"
				}
				if evaluationCode(err) != code || f.releases != 0 || f.psCalls != 0 {
					t.Fatalf("guard: %v releases=%d ps=%d", err, f.releases, f.psCalls)
				}
			})
		}
	}
	f := &residencyFixture{ps: `{"models":[]}`}
	p, req := residencyProvider(t, f)
	cap, ok := any(p).(EvaluationResidencyProvider)
	if !ok {
		t.Fatal("missing capability")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := cap.ReleaseModel(ctx, req); evaluationCode(err) != "USER_CANCELLED" || f.releases != 0 {
		t.Fatalf("cancel: %v", err)
	}
	req.Timeout = -time.Second
	if _, err := cap.Residency(context.Background(), req); evaluationCode(err) != "INVALID_REQUEST" {
		t.Fatalf("timeout: %v", err)
	}
}
