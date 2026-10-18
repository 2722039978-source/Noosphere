package providers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestStructuredToolsRoundTrip(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		if len(body["tools"].([]any)) != 1 {
			t.Error("missing tools")
		}
		messages := body["messages"].([]any)
		if calls == 1 {
			w.Write([]byte(`{"choices":[{"finish_reason":"tool_calls","message":{"role":"assistant","content":null,"tool_calls":[{"id":"call_1","type":"function","function":{"name":"project_read_file","arguments":"{\"path\":\"a.go\"}"}}]}}],"usage":{"prompt_tokens":12,"completion_tokens":4}}`))
			return
		}
		assistant := messages[len(messages)-2].(map[string]any)
		tool := messages[len(messages)-1].(map[string]any)
		if assistant["tool_calls"].([]any)[0].(map[string]any)["id"] != "call_1" || tool["tool_call_id"] != "call_1" {
			t.Error("lost call linkage")
		}
		w.Write([]byte(`{"choices":[{"finish_reason":"stop","message":{"role":"assistant","content":"Verified [E1]"}}]}`))
	}))
	defer server.Close()
	base, _ := NewOpenAI(server.URL, "")
	p := base.(*OpenAIProvider)
	defs := []ToolDefinition{{Type: "function", Function: FunctionDefinition{Name: "project_read_file", Parameters: json.RawMessage(`{"type":"object"}`)}}}
	messages := []ToolMessage{{Role: "user", Content: "inspect"}}
	first, err := p.ChatTools(context.Background(), "fixture", messages, defs, ChatOptions{MaxTokens: 100})
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Message.ToolCalls) != 1 || first.TokensOut != 4 {
		t.Fatal("tool-only response lost")
	}
	messages = append(messages, first.Message, ToolMessage{Role: "tool", ToolCallID: "call_1", Content: "source"})
	final, err := p.ChatTools(context.Background(), "fixture", messages, defs, ChatOptions{MaxTokens: 100})
	if err != nil || final.Message.Content != "Verified [E1]" {
		t.Fatalf("%+v %v", final, err)
	}
}

func TestStructuredToolsCancelInFlightAndDisableThinking(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		if body["thinking"].(map[string]any)["type"] != "disabled" {
			t.Error("thinking mode not disabled")
		}
		close(started)
		<-release
	}))
	defer server.Close()
	defer close(release)
	base, _ := NewOpenAI(server.URL, "")
	p := base.(*OpenAIProvider)
	p.directResponse = true
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { _, err := p.ChatTools(ctx, "fixture", nil, nil, ChatOptions{}); done <- err }()
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("request not received")
	}
	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("cancel accepted as success")
		}
	case <-time.After(time.Second):
		t.Fatal("model request ignored cancellation")
	}
}

func TestStructuredToolsRejectMalformedAndTruncatedReplies(t *testing.T) {
	for _, response := range []string{
		`{"choices":[{"finish_reason":"length","message":{"content":"partial"}}]}`,
		`{"choices":[{"message":{"role":"assistant","tool_calls":[{"id":"","type":"function","function":{"name":"x","arguments":"{}"}}]}}]}`,
		`{"choices":[{"message":{"role":"assistant","tool_calls":[{"id":"x","type":"function","function":{"name":"x","arguments":"{"}}]}}]}`,
		`{"choices":[]}`,
	} {
		t.Run(response[:min(30, len(response))], func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(response)) }))
			defer server.Close()
			base, _ := NewOpenAI(server.URL, "credential-fixture")
			_, err := base.(*OpenAIProvider).ChatTools(context.Background(), "fixture", nil, nil, ChatOptions{})
			if err == nil {
				t.Fatal("accepted bad reply")
			}
			if strings.Contains(err.Error(), "credential-fixture") {
				t.Fatal("credential leaked")
			}
		})
	}
}
