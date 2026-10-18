package providers

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func collectStream(t *testing.T, p LLMProvider) []StreamChunk {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	ch, err := p.ChatStream(ctx, "mock", []Message{{Role: "user", Content: "hello"}}, ChatOptions{})
	if err != nil { t.Fatal(err) }
	var chunks []StreamChunk
	for {
		select {
		case chunk, ok := <-ch:
			if !ok { return chunks }
			chunks = append(chunks, chunk)
		case <-ctx.Done():
			t.Fatal("provider stream did not finish")
		}
	}
}

func TestProviderCompletionMarkers(t *testing.T) {
	for _, tc := range []struct {
		name string
		factory ProviderFactory
		body string
	}{
		{"openai", NewOpenAI, "data:{\"choices\":[{\"delta\":{\"content\":\"你\"}}]}\n\ndata: {\"choices\":[{\"delta\":{\"content\":\"好\"}}]}\n\ndata: [DONE]\n\n"},
		{"claude", NewClaude, "event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"delta\":{\"type\":\"text_delta\",\"text\":\"你好\"}}\n\nevent: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				io.Copy(io.Discard, r.Body)
				w.Header().Set("Content-Type", "text/event-stream")
				io.WriteString(w, tc.body)
				w.(http.Flusher).Flush()
				// Completion must not wait for EOF or the upstream keep-alive timeout.
				<-r.Context().Done()
			}))
			defer server.Close()
			p, err := tc.factory(server.URL, "")
			if err != nil { t.Fatal(err) }
			chunks := collectStream(t, p)
			var tokens strings.Builder
			for _, c := range chunks { tokens.WriteString(c.Token) }
			last := chunks[len(chunks)-1]
			if tokens.String() != "你好" || !last.Done || last.Content != "你好" || last.Error != nil {
				t.Fatalf("incorrect provider completion: %+v", chunks)
			}
		})
	}
}

func TestProviderPrematureEOF(t *testing.T) {
	for _, tc := range []struct {
		name string
		factory ProviderFactory
		body string
	}{
		{"openai", NewOpenAI, "data: {\"choices\":[{\"delta\":{\"content\":\"partial\"}}]}\n\n"},
		{"claude", NewClaude, "data: {\"type\":\"content_block_delta\",\"delta\":{\"text\":\"partial\"}}\n\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { io.WriteString(w, tc.body) }))
			defer server.Close()
			p, _ := tc.factory(server.URL, "")
			chunks := collectStream(t, p)
			last := chunks[len(chunks)-1]
			if last.Done || !errors.Is(last.Error, io.ErrUnexpectedEOF) || last.Content != "partial" {
				t.Fatalf("premature EOF incorrectly completed: %+v", chunks)
			}
		})
	}
}

func TestProviderErrorEvents(t *testing.T) {
	for _, tc := range []struct {
		name string
		factory ProviderFactory
		body string
	}{
		{"openai error", NewOpenAI, "data: {\"error\":{\"message\":\"quota exhausted\"}}\n\n"},
		{"claude error", NewClaude, "data: {\"type\":\"error\",\"error\":{\"message\":\"overloaded\"}}\n\n"},
		{"openai malformed", NewOpenAI, "data: {not json}\n\n"},
		{"claude malformed", NewClaude, "data: {not json}\n\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { io.WriteString(w, tc.body) }))
			defer server.Close()
			p, _ := tc.factory(server.URL, "")
			chunks := collectStream(t, p)
			if len(chunks) != 1 || chunks[0].Error == nil || chunks[0].Done {
				t.Fatalf("upstream error was swallowed: %+v", chunks)
			}
		})
	}
}

func TestProviderCancellationReleasesFullChannel(t *testing.T) {
	for _, tc := range []struct {
		name string
		factory ProviderFactory
		event string
	}{
		{"openai", NewOpenAI, "data: {\"choices\":[{\"delta\":{\"content\":\"x\"}}]}\n\n"},
		{"claude", NewClaude, "data: {\"type\":\"content_block_delta\",\"delta\":{\"text\":\"x\"}}\n\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			upstreamClosed := make(chan struct{})
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				io.Copy(io.Discard, r.Body)
				w.Header().Set("Content-Type", "text/event-stream")
				io.WriteString(w, strings.Repeat(tc.event, 200))
				w.(http.Flusher).Flush()
				<-r.Context().Done()
				close(upstreamClosed)
			}))
			defer server.Close()
			p, _ := tc.factory(server.URL, "")
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			ch, err := p.ChatStream(ctx, "mock", nil, ChatOptions{})
			if err != nil { t.Fatal(err) }
			deadline := time.After(3 * time.Second)
			for len(ch) < cap(ch) {
				select {
				case <-deadline: t.Fatal("provider never filled its channel")
				case <-time.After(time.Millisecond):
				}
			}
			cancel()
			select {
			case <-upstreamClosed:
			case <-time.After(3*time.Second): t.Fatal("upstream connection was not cancelled")
			}
			for {
				select {
				case _, ok := <-ch:
					if !ok { return }
				case <-time.After(3*time.Second): t.Fatal("cancelled producer is blocked on send")
				}
			}
		})
	}
}

func TestProviderCancellationBeforeFirstToken(t *testing.T) {
	for _, factory := range []ProviderFactory{NewOpenAI, NewClaude, NewGemini} {
		started, closed := make(chan struct{}), make(chan struct{})
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			io.Copy(io.Discard, r.Body)
			close(started)
			<-r.Context().Done()
			close(closed)
		}))
		p, _ := factory(server.URL, "")
		t.Run(p.Name(), func(t *testing.T) {
			defer server.Close()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			finished := make(chan struct{})
			go func() {
				defer close(finished)
				ch, err := p.ChatStream(ctx, "mock", nil, ChatOptions{})
				if err == nil { for range ch {} }
			}()
			select {
			case <-started:
			case <-time.After(3*time.Second): t.Fatal("provider did not start")
			}
			cancel()
			for name, ch := range map[string]<-chan struct{}{"provider": finished, "upstream": closed} {
				select {
				case <-ch:
				case <-time.After(3*time.Second): t.Fatalf("%s failed to cancel", name)
				}
			}
		})
	}
}

func TestGeminiFallbackIncludesAllParts(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, ":generateContent") { t.Errorf("unexpected path: %s", r.URL.Path) }
		fmt.Fprint(w, `{"candidates":[{"content":{"parts":[{"text":"你好"},{"text":"世界"}]}}]}`)
	}))
	defer server.Close()
	p, _ := NewGemini(server.URL, "")
	chunks := collectStream(t, p)
	if len(chunks) != 1 || !chunks[0].Done || chunks[0].Content != "你好世界" || chunks[0].Error != nil {
		t.Fatalf("fallback lost final answer: %+v", chunks)
	}
}
