package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/noosphere/backend/internal/gateway/providers"
)

type mockStreamProvider struct {
	stream func(context.Context) (<-chan providers.StreamChunk, error)
	chat   func(context.Context) (*providers.ChatResponse, error)
}

func (p mockStreamProvider) Name() string { return "mock" }
func (p mockStreamProvider) Chat(ctx context.Context, _ string, _ []providers.Message, _ providers.ChatOptions) (*providers.ChatResponse, error) {
	return p.chat(ctx)
}
func (p mockStreamProvider) ChatStream(ctx context.Context, _ string, _ []providers.Message, _ providers.ChatOptions) (<-chan providers.StreamChunk, error) {
	return p.stream(ctx)
}

func streamGateway(p providers.LLMProvider) *Gateway {
	return &Gateway{
		config:    &GatewayConfig{DefaultModel: "mock", Models: []ModelConfig{{ID: "mock", Provider: "mock", ModelName: "mock", Enabled: true}}},
		providers: map[string]providers.LLMProvider{providerCacheKey(&ModelConfig{ID: "mock", Provider: "mock"}): p}, sem: make(chan struct{}, 10),
	}
}

func chunksProvider(chunks ...providers.StreamChunk) mockStreamProvider {
	return mockStreamProvider{stream: func(context.Context) (<-chan providers.StreamChunk, error) {
		ch := make(chan providers.StreamChunk, len(chunks))
		for _, chunk := range chunks {
			ch <- chunk
		}
		close(ch)
		return ch, nil
	}}
}

func streamRequest(ctx context.Context) *http.Request {
	return httptest.NewRequest(http.MethodPost, "/api/gateway/chat/stream", strings.NewReader(`{"messages":[{"role":"user","content":"hello"}]}`)).WithContext(ctx)
}

func readEvents(t *testing.T, w *httptest.ResponseRecorder) []map[string]string {
	t.Helper()
	var events []map[string]string
	for _, line := range strings.Split(w.Body.String(), "\n") {
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		var event map[string]string
		if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &event); err != nil {
			t.Fatal(err)
		}
		events = append(events, event)
	}
	return events
}

func awaitSignal(t *testing.T, ch <-chan struct{}, label string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(3 * time.Second):
		t.Fatalf("timed out waiting for %s", label)
	}
}

func TestStreamFinalSnapshot(t *testing.T) {
	cases := []struct {
		name   string
		chunks []providers.StreamChunk
		want   string
	}{
		{"deltas and full snapshot", []providers.StreamChunk{{Token: "你"}, {Token: "好"}, {Done: true, Content: "你好"}}, "你好"},
		{"terminal only Gemini fallback", []providers.StreamChunk{{Done: true, Content: "完整答案"}}, "完整答案"},
		{"combined delta and terminal", []providers.StreamChunk{{Token: "answer", Content: "answer", Done: true}}, "answer"},
		{"terminal without snapshot", []providers.StreamChunk{{Token: "answer"}, {Done: true}}, "answer"},
		{"authoritative snapshot", []providers.StreamChunk{{Token: "draft"}, {Done: true, Content: "final"}}, "final"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			g := streamGateway(chunksProvider(tc.chunks...))
			w := httptest.NewRecorder()
			g.HandleChatStream(w, streamRequest(context.Background()))
			events := readEvents(t, w)
			if len(events) == 0 {
				t.Fatal("no stream events")
			}
			last := events[len(events)-1]
			if last["type"] != "done" || last["status"] != "completed" || last["content"] != tc.want {
				t.Fatalf("incorrect terminal event: %v", last)
			}
			if len(g.logs) != 1 || g.logs[0].Status != "ok" || g.logs[0].ResponsePreview != tc.want {
				t.Fatalf("incorrect log (content must not be duplicated): %+v", g.logs)
			}
			if len(g.sem) != 0 {
				t.Fatal("stream leaked its concurrency slot")
			}
		})
	}
}

func TestStreamFailuresPreservePartialAnswer(t *testing.T) {
	for _, tc := range []struct {
		name, status string
		chunks       []providers.StreamChunk
	}{
		{"closed without done", "interrupted", []providers.StreamChunk{{Token: "partial"}}},
		{"provider read interruption", "interrupted", []providers.StreamChunk{{Token: "partial"}, {Error: io.ErrUnexpectedEOF}}},
		{"provider error", "error", []providers.StreamChunk{{Token: "partial"}, {Error: errors.New("upstream unavailable")}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g := streamGateway(chunksProvider(tc.chunks...))
			w := httptest.NewRecorder()
			g.HandleChatStream(w, streamRequest(context.Background()))
			events := readEvents(t, w)
			last := events[len(events)-1]
			if last["type"] != "error" || last["status"] != "failed" || last["error"] == "" {
				t.Fatalf("missing terminal error: %v", events)
			}
			if g.logs[0].Status != tc.status || g.logs[0].ResponsePreview != "partial" || g.logs[0].Error == "" {
				t.Fatalf("incorrect failure log: %+v", g.logs[0])
			}
		})
	}
}

func TestStreamCancellationBeforeFirstToken(t *testing.T) {
	started, upstreamClosed := make(chan struct{}), make(chan struct{})
	g := streamGateway(mockStreamProvider{stream: func(ctx context.Context) (<-chan providers.StreamChunk, error) {
		ch := make(chan providers.StreamChunk)
		go func() {
			defer close(ch)
			<-ctx.Done()
			close(upstreamClosed)
		}()
		close(started)
		return ch, nil
	}})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	w := httptest.NewRecorder()
	done := make(chan struct{})
	go func() { g.HandleChatStream(w, streamRequest(ctx)); close(done) }()
	awaitSignal(t, started, "provider setup")
	cancel()
	awaitSignal(t, done, "handler cancellation")
	awaitSignal(t, upstreamClosed, "upstream cancellation")
	if g.logs[0].Status != "cancelled" || strings.Contains(w.Body.String(), `"type":"done"`) {
		t.Fatalf("cancelled request was treated as successful: %+v %s", g.logs, w.Body)
	}
	if len(g.sem) != 0 {
		t.Fatal("cancelled stream leaked concurrency slot")
	}
}

func TestStreamCancellationDuringSetup(t *testing.T) {
	started := make(chan struct{})
	g := streamGateway(mockStreamProvider{stream: func(ctx context.Context) (<-chan providers.StreamChunk, error) {
		close(started)
		<-ctx.Done()
		return nil, ctx.Err()
	}})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() { g.HandleChatStream(httptest.NewRecorder(), streamRequest(ctx)); close(done) }()
	awaitSignal(t, started, "provider setup")
	cancel()
	awaitSignal(t, done, "setup cancellation")
	if g.logs[0].Status != "cancelled" || len(g.sem) != 0 {
		t.Fatalf("setup cancellation did not release/log correctly: %+v", g.logs)
	}
}

func TestQueuedStreamUsesSharedBudgetAndCancels(t *testing.T) {
	started := make(chan struct{})
	var streamCalls atomic.Int32
	g := streamGateway(mockStreamProvider{
		chat: func(ctx context.Context) (*providers.ChatResponse, error) {
			close(started)
			<-ctx.Done()
			return nil, ctx.Err()
		},
		stream: func(context.Context) (<-chan providers.StreamChunk, error) {
			streamCalls.Add(1)
			return nil, errors.New("should not run while slot held")
		},
	})
	g.sem = make(chan struct{}, 1)
	chatCtx, cancelChat := context.WithCancel(context.Background())
	defer cancelChat()
	chatDone := make(chan struct{})
	go func() {
		g.HandleChat(httptest.NewRecorder(), streamRequest(chatCtx))
		close(chatDone)
	}()
	awaitSignal(t, started, "regular chat holding slot")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	g.HandleChatStream(httptest.NewRecorder(), streamRequest(ctx))
	if streamCalls.Load() != 0 || len(g.sem) != 1 {
		t.Fatal("stream bypassed the shared limit or released someone else's slot")
	}
	cancelChat()
	awaitSignal(t, chatDone, "regular chat cancellation")
	if len(g.sem) != 0 {
		t.Fatal("regular chat failed to release shared slot")
	}
}

type failingStreamWriter struct {
	header   http.Header
	flushErr bool
}

func (w *failingStreamWriter) Header() http.Header       { return w.header }
func (w *failingStreamWriter) WriteHeader(int)           {}
func (w *failingStreamWriter) Write([]byte) (int, error) { return 0, io.ErrClosedPipe }
func (w *failingStreamWriter) Flush()                    {}
func (w *failingStreamWriter) FlushError() error {
	if w.flushErr {
		return io.ErrClosedPipe
	}
	return nil
}

func TestStreamWriteFailureCancelsProducer(t *testing.T) {
	for _, failFlush := range []bool{false, true} {
		t.Run(map[bool]string{false: "write", true: "flush"}[failFlush], func(t *testing.T) {
			cancelled := make(chan struct{})
			g := streamGateway(mockStreamProvider{stream: func(ctx context.Context) (<-chan providers.StreamChunk, error) {
				ch := make(chan providers.StreamChunk, 1)
				ch <- providers.StreamChunk{Token: "hello"}
				go func() { <-ctx.Done(); close(cancelled); close(ch) }()
				return ch, nil
			}})
			g.HandleChatStream(&failingStreamWriter{header: make(http.Header), flushErr: failFlush}, streamRequest(context.Background()))
			awaitSignal(t, cancelled, "producer cleanup after failed write")
			if g.logs[0].Status == "ok" || g.logs[0].Error == "" || len(g.sem) != 0 {
				t.Fatalf("write failure was not handled: %+v", g.logs)
			}
		})
	}
}

func TestConcurrentStreamsLogsAndModelSnapshots(t *testing.T) {
	g := streamGateway(chunksProvider(providers.StreamChunk{Token: "你好"}, providers.StreamChunk{Content: "你好", Done: true}))
	var wg sync.WaitGroup
	for i := 0; i < 40; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			g.HandleChatStream(httptest.NewRecorder(), streamRequest(context.Background()))
			g.HandleLogs(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/logs", nil))
			g.HandleStats(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/stats", nil))
			model := g.GetFirstModel()
			model.Name = "changed only in copy"
		}()
	}
	wg.Wait()
	if len(g.logs) != 40 || g.config.Models[0].Name != "" {
		t.Fatalf("lost concurrent logs or leaked model reference: %d %+v", len(g.logs), g.config.Models[0])
	}
}

func TestStreamSetupFailureLogged(t *testing.T) {
	g := streamGateway(mockStreamProvider{stream: func(context.Context) (<-chan providers.StreamChunk, error) {
		return nil, errors.New("upstream refused connection")
	}})
	w := httptest.NewRecorder()
	g.HandleChatStream(w, streamRequest(context.Background()))
	if w.Code < 400 || len(g.logs) != 1 || g.logs[0].Status != "error" || len(g.sem) != 0 {
		t.Fatalf("setup failure not recorded or released: code=%d logs=%+v", w.Code, g.logs)
	}
}
