package gateway

import (
	"context"
	"errors"
	"github.com/noosphere/backend/internal/gateway/providers"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type admissionEvaluationProvider struct {
	mockStreamProvider
	resident        bool
	foreign         bool
	calls, releases int
	evaluate        func(context.Context, providers.EvaluationRequest) (*providers.EvaluationResponse, error)
}

func (p *admissionEvaluationProvider) ChatTools(context.Context, string, []providers.ToolMessage, []providers.ToolDefinition, providers.ChatOptions) (*providers.ToolResponse, error) {
	return &providers.ToolResponse{}, nil
}

func (p *admissionEvaluationProvider) Discover(context.Context) (providers.ModelDiscovery, error) {
	return providers.ModelDiscovery{}, nil
}
func (p *admissionEvaluationProvider) Evaluate(ctx context.Context, req providers.EvaluationRequest) (*providers.EvaluationResponse, error) {
	p.calls++
	if p.evaluate != nil {
		return p.evaluate(ctx, req)
	}
	p.resident = true
	return &providers.EvaluationResponse{Output: "fixture"}, nil
}
func (p *admissionEvaluationProvider) Residency(context.Context, providers.EvaluationRequest) (providers.ModelResidencySnapshot, error) {
	s := providers.ModelResidencySnapshot{TargetResident: p.resident}
	if p.resident {
		s.Models = []providers.ResidentModel{{Name: "fixture:latest", Digest: "digest"}}
	}
	if p.foreign {
		s.Models = append(s.Models, providers.ResidentModel{Name: "external:latest", Digest: "other"})
	}
	return s, nil
}
func (p *admissionEvaluationProvider) ReleaseModel(context.Context, providers.EvaluationRequest) (providers.ModelResidencySnapshot, error) {
	p.releases++
	p.resident = false
	return providers.ModelResidencySnapshot{}, nil
}
func admissionEvaluationGateway(p providers.LLMProvider) *Gateway {
	m := ModelConfig{ID: "local", Provider: "ollama", BaseURL: "http://127.0.0.1:11434", Enabled: true}
	return &Gateway{config: &GatewayConfig{Models: []ModelConfig{m}}, providers: map[string]providers.LLMProvider{providerCacheKey(&m): p}, sem: make(chan struct{}, 10)}
}
func admissionRequest() providers.EvaluationRequest {
	return providers.EvaluationRequest{ModelName: "fixture:latest", ExpectedDigest: "digest", ExpectedMetadataHash: "metadata", ExpectedEngineVersion: "fixture", Kind: "warmup"}
}

func TestAdmissionClosePreservesLinearizedForegroundIntent(t *testing.T) {
	for _, foregroundFirst := range []bool{true, false} {
		name := "close_first"
		if foregroundFirst {
			name = "foreground_first"
		}
		t.Run(name, func(t *testing.T) {
			g := admissionEvaluationGateway(&admissionEvaluationProvider{})
			session, closeSession, err := g.BeginEvaluationSession(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			s := g.sessionFrom(session)
			if foregroundFirst {
				// Deterministically pause the foreground path after it records
				// intent, before its outside-lock CancelCause invocation.
				g.admission.mu.Lock()
				requested := s.requestPreemptionLocked()
				g.admission.mu.Unlock()
				if !requested {
					t.Fatal("open session rejected foreground intent")
				}
			}
			closeSession()
			want := context.Canceled
			if foregroundFirst {
				want = ErrForegroundPreempted
			}
			if !errors.Is(context.Cause(session), want) {
				t.Fatalf("close lost the linearized cause: got=%v want=%v", context.Cause(session), want)
			}
			g.admission.mu.Lock()
			requested := s.requestPreemptionLocked()
			reservationReleased := g.admission.session == nil
			g.admission.mu.Unlock()
			if requested || !reservationReleased {
				t.Fatal("closed idle session retained reservation or accepted late intent")
			}
			if foregroundFirst {
				s.cancel(ErrForegroundPreempted) // Resume the delayed foreground callback.
			}
			closeSession()
			if !errors.Is(context.Cause(session), want) {
				t.Fatal("repeated close changed the frozen cause")
			}
		})
	}
}

func TestAdmissionOwnershipRejectsSharedAndForeignIdentity(t *testing.T) {
	p := &admissionEvaluationProvider{resident: true}
	g := admissionEvaluationGateway(p)
	req := admissionRequest()
	session, closeSession, err := g.BeginEvaluationSession(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer closeSession()
	if _, err = g.InspectEvaluationResidency(session, "local", req); err != nil {
		t.Fatal(err)
	}
	if _, err = g.Evaluate(session, "local", req); err == nil {
		t.Fatal("shared model admitted")
	}
	if _, err = g.ReleaseEvaluationModel(session, "local", req); err == nil || p.releases != 0 {
		t.Fatal("unowned shared model unloaded")
	}
	p.resident = false
	if _, err = g.InspectEvaluationResidency(session, "local", req); err != nil {
		t.Fatal(err)
	}
	if _, err = g.Evaluate(session, "local", req); err != nil {
		t.Fatal(err)
	}
	p.foreign = true
	if _, err = g.InspectEvaluationResidency(session, "local", req); err == nil {
		t.Fatal("external occupancy was accepted as warm evidence")
	}
	if _, err = g.ReleaseEvaluationModel(session, "local", req); err == nil || p.releases != 0 {
		t.Fatal("external occupancy permitted forced unload")
	}
	p.foreign = false
	other := req
	other.Kind = "evaluation"
	other.ExpectedDigest = "different"
	if _, err = g.Evaluate(session, "local", other); err == nil {
		t.Fatal("session inferred foreign identity")
	}
	g.sessionFrom(session).cancel(ErrForegroundPreempted)
	cleanup, cancel := context.WithTimeout(context.WithoutCancel(session), time.Second)
	defer cancel()
	if _, err = g.ReleaseEvaluationModel(cleanup, "local", other); err == nil || p.releases != 0 {
		t.Fatal("foreign cleanup unloaded model")
	}
	if _, err = g.ReleaseEvaluationModel(cleanup, "local", req); err != nil || p.releases != 1 {
		t.Fatalf("owned cleanup failed: %v, %d", err, p.releases)
	}
	state := g.admissionState()
	if state.evaluationActive != 0 || state.evaluationCalls != 2 {
		t.Fatalf("actual warmup/release counts: %+v", state)
	}
}

func TestAdmissionForegroundPreemptsAndSessionCloseWaitsForActualCall(t *testing.T) {
	providerStarted := make(chan struct{})
	providerReturn := make(chan struct{})
	providerDone := make(chan struct{})
	p := &admissionEvaluationProvider{evaluate: func(context.Context, providers.EvaluationRequest) (*providers.EvaluationResponse, error) {
		close(providerStarted)
		<-providerReturn
		return &providers.EvaluationResponse{Output: "late fixture"}, nil
	}}
	g := admissionEvaluationGateway(p)
	session, closeSession, err := g.BeginEvaluationSession(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	req := admissionRequest()
	if _, err = g.InspectEvaluationResidency(session, "local", req); err != nil {
		t.Fatal(err)
	}
	go func() { g.Evaluate(session, "local", req); close(providerDone) }()
	<-providerStarted
	foregroundDone := make(chan func(), 1)
	go func() {
		release, e := g.beginInteractive(context.Background())
		if e == nil {
			foregroundDone <- release
		}
	}()
	select {
	case <-session.Done():
	case <-time.After(time.Second):
		t.Fatal("foreground did not cancel evaluation")
	}
	if !errors.Is(context.Cause(session), ErrForegroundPreempted) {
		t.Fatal(context.Cause(session))
	}
	closeSession()
	if g.admissionState().evaluationActive != 1 {
		t.Fatal("cancel-ignoring provider released actual counter")
	}
	select {
	case <-foregroundDone:
		t.Fatal("session close freed running provider")
	case <-time.After(20 * time.Millisecond):
	}
	close(providerReturn)
	<-providerDone
	select {
	case release := <-foregroundDone:
		release()
	case <-time.After(time.Second):
		t.Fatal("foreground stayed blocked")
	}
	if g.admissionState().evaluationActive != 0 {
		t.Fatal("actual counter leaked")
	}
}

func TestAdmissionInteractiveRemainsConcurrentAndEvaluationQueuesBeforeSemaphore(t *testing.T) {
	g := &Gateway{sem: make(chan struct{}, 1)}
	a, err := g.beginInteractive(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer a()
	b, err := g.beginInteractive(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer b()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	_, _, err = g.BeginEvaluationSession(ctx)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected foreground to block background: %v", err)
	}
	if len(g.sem) != 0 {
		t.Fatal("waiting evaluation monopolized legacy semaphore")
	}
	a()
	b()
	p := &admissionEvaluationProvider{}
	g = admissionEvaluationGateway(p)
	g.sem = make(chan struct{}, 1)
	g.sem <- struct{}{}
	request := admissionRequest()
	request.Kind = "evaluation"
	evaluationDone := make(chan error, 1)
	go func() { _, err := g.Evaluate(context.Background(), "local", request); evaluationDone <- err }()
	deadline := time.After(time.Second)
	for {
		g.admission.mu.Lock()
		reserved := g.admission.session != nil
		g.admission.mu.Unlock()
		if reserved {
			break
		}
		select {
		case <-deadline:
			t.Fatal("implicit evaluation never reserved")
		case <-time.After(time.Millisecond):
		}
	}
	foregroundCtx, stop := context.WithTimeout(context.Background(), time.Second)
	defer stop()
	foregroundDone, err := g.beginInteractive(foregroundCtx)
	if err != nil {
		t.Fatal(err)
	}
	defer foregroundDone()
	if err = <-evaluationDone; err == nil || p.calls != 0 || len(g.sem) != 1 {
		t.Fatalf("implicit evaluation hid foreground arrival: %v calls=%d sem=%d", err, p.calls, len(g.sem))
	}
	<-g.sem
}

func TestAdmissionSessionSerializesCallsAndDetachedCleanupOnly(t *testing.T) {
	g := &Gateway{sem: make(chan struct{}, 10)}
	session, closeSession, err := g.BeginEvaluationSession(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer closeSession()
	finish, err := g.acquireEvaluationCall(session, false)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(session, 20*time.Millisecond)
	defer cancel()
	if _, err := g.acquireEvaluationCall(ctx, false); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("overlapping session call: %v", err)
	}
	finish()
	g.sessionFrom(session).cancel(ErrForegroundPreempted)
	detached := context.WithoutCancel(session)
	if _, err := g.acquireEvaluationCall(detached, false); err == nil {
		t.Fatal("detached context restarted inference")
	}
	cleanup, err := g.acquireEvaluationCall(detached, true)
	if err != nil {
		t.Fatal(err)
	}
	cleanup()
	closeSession()
	if _, err := g.acquireEvaluationCall(detached, true); err == nil {
		t.Fatal("closed session allowed cleanup")
	}
}

func TestAdmissionCountsEveryActualEntryAndRetry(t *testing.T) {
	chatCalls := 0
	p := &admissionEvaluationProvider{}
	p.chat = func(context.Context) (*providers.ChatResponse, error) {
		chatCalls++
		if chatCalls == 1 {
			return nil, errors.New("fixture timeout")
		}
		return &providers.ChatResponse{Content: "fixture"}, nil
	}
	p.stream = chunksProvider(providers.StreamChunk{Done: true, Content: "fixture"}).stream
	g := admissionEvaluationGateway(p)
	model := &g.config.Models[0]
	if _, _, _, err := g.ChatContext(context.Background(), model, nil, "", 0, 1); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := g.Chat(model, nil, "", 0, 1); err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	g.HandleChat(w, httptest.NewRequest("POST", "/", strings.NewReader(`{"model_id":"local"}`)))
	if w.Code != 200 {
		t.Fatal(w.Code)
	}
	if g.testModelContext(context.Background(), model)["status"] != "ok" {
		t.Fatal("test model failed")
	}
	if _, err := g.ChatTools(context.Background(), model, "", nil, nil); err != nil {
		t.Fatal(err)
	}
	w = httptest.NewRecorder()
	g.HandleChatStream(w, httptest.NewRequest("POST", "/", strings.NewReader(`{"model_id":"local"}`)))
	req := admissionRequest()
	req.Kind = "evaluation"
	if _, err := g.Evaluate(context.Background(), "local", req); err != nil {
		t.Fatal(err)
	}
	state := g.admissionState()
	if state.interactiveCalls != 7 || state.evaluationCalls != 1 || state.interactiveActive != 0 || state.evaluationActive != 0 || state.foreground != 0 {
		t.Fatalf("actual invocation counters %+v (chat=%d)", state, chatCalls)
	}
}

func TestAdmissionStreamRetainsReservationUntilProducerCloses(t *testing.T) {
	producer := make(chan providers.StreamChunk, 1)
	producer <- providers.StreamChunk{Done: true, Content: "fixture"}
	started := make(chan struct{})
	cancelled := make(chan struct{})
	p := mockStreamProvider{stream: func(ctx context.Context) (<-chan providers.StreamChunk, error) {
		close(started)
		go func() { <-ctx.Done(); close(cancelled) }()
		return producer, nil
	}}
	g := streamGateway(p)
	finished := make(chan struct{})
	go func() {
		g.HandleChatStream(httptest.NewRecorder(), streamRequest(context.Background()))
		close(finished)
	}()
	<-started
	<-cancelled
	if g.admissionState().interactiveActive != 1 {
		t.Fatal("stream consumer freed still-open producer")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, _, err := g.BeginEvaluationSession(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("background interleaved with open producer: %v", err)
	}
	close(producer)
	select {
	case <-finished:
	case <-time.After(time.Second):
		t.Fatal("closed producer did not finish")
	}
	if g.admissionState().interactiveActive != 0 || len(g.sem) != 0 {
		t.Fatal("stream completion leaked permit")
	}
}
