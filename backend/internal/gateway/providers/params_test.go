package providers

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

func TestNativeCloudProtocolsOmitDisabledTemperature(t *testing.T) {
	for _, tc := range []struct {
		name, path, maxKey, response string
		newProvider                  ProviderFactory
		stream                       bool
	}{
		{"claude chat", "/proxy/v1/messages", "max_tokens", `{"content":[{"type":"text","text":"ok"}]}`, NewClaude, false},
		{"claude stream", "/proxy/v1/messages", "max_tokens", "data: {\"type\":\"message_stop\"}\n\n", NewClaude, true},
		{"gemini chat", "/proxy/v1/models/fixture:generateContent", "maxOutputTokens", `{"candidates":[{"content":{"parts":[{"text":"ok"}]}}]}`, NewGemini, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != tc.path {
					t.Errorf("request path = %q, want %q", r.URL.Path, tc.path)
				}
				var body map[string]any
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Error(err)
				}
				options := body
				if tc.maxKey == "maxOutputTokens" {
					options, _ = body["generationConfig"].(map[string]any)
				}
				if _, found := options["temperature"]; found {
					t.Error("request supplied disabled temperature")
				}
				if options[tc.maxKey] != float64(64) {
					t.Errorf("request output budget = %v", options)
				}
				io.WriteString(w, tc.response)
			}))
			defer server.Close()
			provider, err := tc.newProvider(server.URL+"/proxy/v1/", "")
			if err != nil {
				t.Fatal(err)
			}
			options := ChatOptions{MaxTokens: 64, MaxTokensField: "max_completion_tokens", OmitTemperature: true}
			messages := []Message{{Role: "user", Content: "hello"}}
			if tc.stream {
				chunks, err := provider.ChatStream(context.Background(), "fixture", messages, options)
				if err != nil {
					t.Fatal(err)
				}
				for chunk := range chunks {
					if chunk.Error != nil {
						t.Fatal(chunk.Error)
					}
				}
			} else if _, err := provider.Chat(context.Background(), "fixture", messages, options); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestNativeCloudProtocolsRedactCredentialsFromErrors(t *testing.T) {
	const key = "fixture-model-secret"
	for _, tc := range []struct {
		name, reply string
		factory     ProviderFactory
		stream      bool
		status      int
	}{
		{"claude chat", "credential rejected: " + key, NewClaude, false, http.StatusUnauthorized},
		{"claude stream setup", "credential rejected: " + key, NewClaude, true, http.StatusUnauthorized},
		{"claude stream event", "data: {\"type\":\"error\",\"error\":{\"message\":\"credential rejected: " + key + "\"}}\n\n", NewClaude, true, http.StatusOK},
		{"gemini chat", "credential rejected: " + key, NewGemini, false, http.StatusUnauthorized},
	} {
		t.Run(tc.name, func(t *testing.T) {
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tc.status)
				io.WriteString(w, tc.reply)
			}))
			defer upstream.Close()
			provider, err := tc.factory(upstream.URL, key)
			if err != nil {
				t.Fatal(err)
			}
			if tc.stream {
				var chunks <-chan StreamChunk
				chunks, err = provider.ChatStream(context.Background(), "fixture", nil, ChatOptions{})
				if err == nil {
					for chunk := range chunks {
						err = chunk.Error
					}
				}
			} else {
				_, err = provider.Chat(context.Background(), "fixture", nil, ChatOptions{})
			}
			if err == nil || strings.Contains(err.Error(), key) || !strings.Contains(err.Error(), "credential rejected") {
				t.Error("expected actionable provider error with the credential masked")
			}
		})
	}
}

func TestNativeCloudProtocolsDoNotFollowDifferentOriginRedirect(t *testing.T) {
	for _, tc := range []struct {
		name    string
		factory ProviderFactory
	}{{"claude", NewClaude}, {"gemini", NewGemini}} {
		t.Run(tc.name, func(t *testing.T) {
			var unexpectedCalls atomic.Int32
			target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				unexpectedCalls.Add(1)
				w.WriteHeader(http.StatusUnauthorized)
			}))
			defer target.Close()
			original := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				http.Redirect(w, r, target.URL, http.StatusTemporaryRedirect)
			}))
			defer original.Close()
			provider, err := tc.factory(original.URL, "fixture-model-secret")
			if err != nil {
				t.Fatal(err)
			}
			_, err = provider.Chat(context.Background(), "fixture", nil, ChatOptions{})
			if unexpectedCalls.Load() != 0 {
				t.Error("chat forwarded the request to a different provider origin")
			}
			if upstream, ok := err.(*UpstreamHTTPError); !ok || upstream.Status != http.StatusTemporaryRedirect {
				t.Errorf("request error = %v, want HTTP redirect", err)
			}
		})
	}
}

func TestApplyChatOptionsSelectsTokenField(t *testing.T) {
	official := &OpenAIProvider{openAIEndpoint: true}
	thirdParty := &OpenAIProvider{}

	body := map[string]interface{}{}
	official.applyChatOptions(body, "gpt-5.5", 0.1, 100, ChatOptions{})
	if _, ok := body["max_completion_tokens"]; !ok {
		t.Fatalf("official reasoning model should use max_completion_tokens: %v", body)
	}
	if _, ok := body["temperature"]; ok {
		t.Fatalf("official reasoning model should omit temperature: %v", body)
	}

	legacy := map[string]interface{}{}
	thirdParty.applyChatOptions(legacy, "gpt-5.5", 0.1, 100, ChatOptions{})
	if legacy["max_tokens"] != 100 {
		t.Fatalf("third-party endpoint should keep max_tokens: %v", legacy)
	}
	if legacy["temperature"] != 0.1 {
		t.Fatalf("third-party endpoint should keep temperature: %v", legacy)
	}

	configured := map[string]interface{}{}
	thirdParty.applyChatOptions(configured, "anything", 0.1, 100, ChatOptions{MaxTokensField: "max_completion_tokens", OmitTemperature: true})
	if configured["max_completion_tokens"] != 100 {
		t.Fatalf("configured field should win: %v", configured)
	}
	if _, ok := configured["temperature"]; ok {
		t.Fatalf("configured omission should drop temperature: %v", configured)
	}
}

func TestAdjustChatOptionsReadsProviderHints(t *testing.T) {
	cases := []struct {
		name     string
		detail   string
		want     ChatOptions
		adjusted bool
	}{
		{
			name:     "max_tokens rejected",
			detail:   `{"error":{"message":"Unsupported parameter: 'max_tokens' is not supported with this model. Use 'max_completion_tokens' instead."}}`,
			want:     ChatOptions{MaxTokensField: "max_completion_tokens"},
			adjusted: true,
		},
		{
			name:     "temperature rejected",
			detail:   `{"error":{"message":"Unsupported value: 'temperature' does not support 0.1 with this model. Only the default (1) value is supported."}}`,
			want:     ChatOptions{OmitTemperature: true},
			adjusted: true,
		},
		{
			name:     "content error is not retried",
			detail:   `{"error":{"message":"invalid request: messages must not be empty"}}`,
			want:     ChatOptions{},
			adjusted: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, adjusted := adjustChatOptions(tc.detail, ChatOptions{})
			if adjusted != tc.adjusted {
				t.Fatalf("adjusted = %v, want %v", adjusted, tc.adjusted)
			}
			if got != tc.want {
				t.Fatalf("options = %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestChatToolsRetriesWithCompletionTokens(t *testing.T) {
	var bodies []map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode request: %v", err)
			return
		}
		bodies = append(bodies, body)
		if len(bodies) == 1 {
			w.WriteHeader(http.StatusBadRequest)
			io.WriteString(w, `{"error":{"message":"Unsupported parameter: 'max_tokens' is not supported with this model. Use 'max_completion_tokens' instead."}}`)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"choices":[{"finish_reason":"tool_calls","message":{"role":"assistant","content":"","tool_calls":[{"id":"call_1","type":"function","function":{"name":"project_list_files","arguments":"{}"}}]}}]}`)
	}))
	defer server.Close()

	provider := &OpenAIProvider{baseURL: server.URL, client: server.Client()}
	tools := []ToolDefinition{{Type: "function", Function: FunctionDefinition{Name: "project_list_files", Parameters: json.RawMessage(`{"type":"object"}`)}}}
	response, err := provider.ChatTools(context.Background(), "gpt-5.5", []ToolMessage{{Role: "user", Content: "hi"}}, tools, ChatOptions{MaxTokens: 512})
	if err != nil {
		t.Fatalf("ChatTools: %v", err)
	}
	if len(response.Message.ToolCalls) != 1 {
		t.Fatalf("tool calls = %d, want 1", len(response.Message.ToolCalls))
	}
	if len(bodies) != 2 {
		t.Fatalf("requests = %d, want 2 (one retry)", len(bodies))
	}
	if _, ok := bodies[0]["max_tokens"]; !ok {
		t.Fatalf("first request should use max_tokens: %v", bodies[0])
	}
	if _, ok := bodies[1]["max_completion_tokens"]; !ok {
		t.Fatalf("retry should use max_completion_tokens: %v", bodies[1])
	}
}

func TestChatToolsRetriesWithoutTemperature(t *testing.T) {
	var bodies []map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]interface{}
		_ = json.NewDecoder(r.Body).Decode(&body)
		bodies = append(bodies, body)
		if len(bodies) == 1 {
			w.WriteHeader(http.StatusBadRequest)
			io.WriteString(w, `{"error":{"message":"Unsupported value: 'temperature' does not support 0.1 with this model. Only the default (1) value is supported."}}`)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"choices":[{"finish_reason":"stop","message":{"role":"assistant","content":"done"}}]}`)
	}))
	defer server.Close()

	provider := &OpenAIProvider{baseURL: server.URL, client: server.Client()}
	response, err := provider.ChatTools(context.Background(), "gpt-6.1-sol", []ToolMessage{{Role: "user", Content: "hi"}}, nil, ChatOptions{MaxTokens: 256})
	if err != nil {
		t.Fatalf("ChatTools: %v", err)
	}
	if response.Message.Content != "done" {
		t.Fatalf("content = %q", response.Message.Content)
	}
	if _, ok := bodies[0]["temperature"]; !ok {
		t.Fatalf("first request should include temperature: %v", bodies[0])
	}
	if _, ok := bodies[1]["temperature"]; ok {
		t.Fatalf("retry should omit temperature: %v", bodies[1])
	}
}

func TestResponsesInputMapsTurns(t *testing.T) {
	messages := []ToolMessage{
		{Role: "system", Content: "sys"},
		{Role: "user", Content: "hi"},
		{Role: "assistant", ToolCalls: []ToolCall{{ID: "call_1", Type: "function", Function: ToolFunction{Name: "project_list_files", Arguments: "{}"}}}},
		{Role: "tool", ToolCallID: "call_1", Content: `{"ok":true}`},
	}
	items := responsesInput(messages)
	if len(items) != 4 {
		t.Fatalf("items = %d, want 4: %v", len(items), items)
	}
	call, ok := items[2].(map[string]interface{})
	if !ok || call["type"] != "function_call" || call["call_id"] != "call_1" || call["name"] != "project_list_files" {
		t.Fatalf("unexpected function_call item: %v", items[2])
	}
	output, ok := items[3].(map[string]interface{})
	if !ok || output["type"] != "function_call_output" || output["call_id"] != "call_1" {
		t.Fatalf("unexpected function_call_output item: %v", items[3])
	}
}

func TestChatToolsFallsBackToResponses(t *testing.T) {
	var paths []string
	var responsesBody map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		if r.URL.Path == "/chat/completions" {
			w.WriteHeader(http.StatusBadRequest)
			io.WriteString(w, `{"error":{"message":"Function tools with reasoning_effort are not supported for gpt-6-sol in /v1/chat/completions. To use function tools, use /v1/responses or set reasoning_effort to 'none'."}}`)
			return
		}
		_ = json.NewDecoder(r.Body).Decode(&responsesBody)
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"output":[{"type":"function_call","call_id":"call_9","name":"project_list_files","arguments":"{}"}],"usage":{"input_tokens":10,"output_tokens":5}}`)
	}))
	defer server.Close()

	provider := &OpenAIProvider{baseURL: server.URL, client: server.Client()}
	tools := []ToolDefinition{{Type: "function", Function: FunctionDefinition{Name: "project_list_files", Description: "list", Parameters: json.RawMessage(`{"type":"object"}`)}}}
	response, err := provider.ChatTools(context.Background(), "gpt-6-sol", []ToolMessage{{Role: "user", Content: "hi"}}, tools, ChatOptions{MaxTokens: 256})
	if err != nil {
		t.Fatalf("ChatTools: %v", err)
	}
	if len(response.Message.ToolCalls) != 1 || response.Message.ToolCalls[0].ID != "call_9" {
		t.Fatalf("tool calls = %+v", response.Message.ToolCalls)
	}
	if len(paths) != 2 || paths[0] != "/chat/completions" || paths[1] != "/responses" {
		t.Fatalf("paths = %v, want chat then responses", paths)
	}
	if _, ok := responsesBody["max_output_tokens"]; !ok {
		t.Fatalf("responses body should carry max_output_tokens: %v", responsesBody)
	}
	flat, ok := responsesBody["tools"].([]interface{})
	if !ok || len(flat) != 1 {
		t.Fatalf("tools = %v", responsesBody["tools"])
	}
	first, _ := flat[0].(map[string]interface{})
	if first["name"] != "project_list_files" {
		t.Fatalf("tools should be flattened: %v", first)
	}
}

func TestChatToolsContinuesResponsesWithOriginalOutputItems(t *testing.T) {
	var chatCalls, responsesCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/chat/completions" {
			chatCalls.Add(1)
			w.WriteHeader(http.StatusBadRequest)
			io.WriteString(w, `{"error":{"message":"Use /v1/responses with function tools."}}`)
			return
		}
		if r.URL.Path != "/responses" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if responsesCalls.Add(1) == 1 {
			io.WriteString(w, `{"output":[{"type":"reasoning","id":"rs_fixture","encrypted_content":"opaque-fixture","summary":[]},{"type":"function_call","id":"fc_fixture","call_id":"call_fixture","name":"lookup","arguments":"{}"}]}`)
			return
		}
		var request struct {
			Input []struct {
				Type             string `json:"type"`
				ID               string `json:"id"`
				EncryptedContent string `json:"encrypted_content"`
				CallID           string `json:"call_id"`
				Output           string `json:"output"`
			} `json:"input"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		items := request.Input
		if len(items) != 4 ||
			items[1].Type != "reasoning" || items[1].ID != "rs_fixture" || items[1].EncryptedContent != "opaque-fixture" ||
			items[2].Type != "function_call" || items[2].ID != "fc_fixture" || items[2].CallID != "call_fixture" ||
			items[3].Type != "function_call_output" || items[3].CallID != "call_fixture" || items[3].Output != "local fixture evidence" {
			w.WriteHeader(http.StatusBadRequest)
			io.WriteString(w, `{"error":{"message":"Function output requires its original reasoning and call items."}}`)
			return
		}
		io.WriteString(w, `{"output":[{"type":"message","content":[{"type":"output_text","text":"Saw local fixture evidence."}]}]}`)
	}))
	defer server.Close()

	p := &OpenAIProvider{baseURL: server.URL, client: server.Client()}
	messages := []ToolMessage{{Role: "user", Content: "Find the fixture evidence."}}
	tools := []ToolDefinition{{Type: "function", Function: FunctionDefinition{Name: "lookup", Parameters: json.RawMessage(`{"type":"object"}`)}}}
	initial, err := p.ChatTools(t.Context(), "fixture", messages, tools, ChatOptions{})
	if err != nil || len(initial.Message.ToolCalls) != 1 || initial.Message.ToolCalls[0].ID != "call_fixture" {
		t.Fatalf("initial tool turn: %+v %v", initial, err)
	}
	messages = append(messages, initial.Message, ToolMessage{Role: "tool", ToolCallID: "call_fixture", Content: "local fixture evidence"})
	final, err := p.ChatTools(t.Context(), "fixture", messages, tools, ChatOptions{})
	if err != nil || final.Message.Content != "Saw local fixture evidence." {
		t.Fatalf("Responses tool continuation: %+v %v", final, err)
	}
	if got := chatCalls.Load(); got != 1 {
		t.Fatalf("chat transport tried %d times after selecting Responses", got)
	}
}

func TestManagedDeploymentRejectsForeignLease(t *testing.T) {
	listener, err := net.ListenTCP("tcp4", &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	outbound, err := net.DialTCP("tcp4", nil, listener.Addr().(*net.TCPAddr))
	if err != nil {
		t.Fatal(err)
	}
	defer outbound.Close()
	inbound, err := listener.AcceptTCP()
	if err != nil {
		t.Fatal(err)
	}
	defer inbound.Close()
	serverPort := inbound.LocalAddr().(*net.TCPAddr).Port
	clientPort := outbound.LocalAddr().(*net.TCPAddr).Port
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "local-engines"), 0700); err != nil {
		t.Fatal(err)
	}
	group := managedGroup(uint32(os.Getpid()))
	lease := filepath.Join(dir, "local-engines", "custom-listener.json")
	for _, data := range []string{
		`{"desktop_pid":0,"pid":0,"model":"gguf-fixture"}`,
		fmt.Sprintf(`{"desktop_pid":%d,"pid":%d,"group_pid":%d,"model":"gguf-other"}`, os.Getppid(), os.Getpid(), group),
		fmt.Sprintf(`{"desktop_pid":%d,"pid":%d,"group_pid":%d,"model":"gguf-fixture"}`, os.Getppid(), os.Getppid(), group),
		fmt.Sprintf(`{"desktop_pid":%d,"pid":%d,"group_pid":%d,"model":"gguf-fixture"}`, os.Getppid(), os.Getpid(), group+1),
	} {
		if err := os.WriteFile(lease, []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
		if err := verifyModelListener(t.Context(), dir, "gguf-fixture", serverPort, clientPort, false); err == nil {
			t.Fatal("accepted a lease without a matching owned listener")
		}
	}
	if err := os.WriteFile(lease, []byte(fmt.Sprintf(`{"desktop_pid":%d,"pid":%d,"group_pid":%d,"model":"gguf-fixture"}`, os.Getppid(), os.Getpid(), group)), 0600); err != nil {
		t.Fatal(err)
	}
	if err := verifyModelListener(t.Context(), dir, "gguf-fixture", serverPort, clientPort, false); err != nil {
		t.Fatal(err)
	}
	if err := verifyModelListener(t.Context(), dir, "gguf-fixture", serverPort, serverPort, false); err == nil {
		t.Fatal("accepted a listening PID with no matching established peer")
	}
	if err := verifyModelListener(t.Context(), dir, "gguf-fixture", serverPort, clientPort, true); err == nil {
		t.Fatal("accepted image data without verified live vision capability")
	}
	if err := os.WriteFile(lease, []byte(fmt.Sprintf(`{"desktop_pid":%d,"pid":%d,"group_pid":%d,"model":"gguf-fixture","vision":true}`, os.Getppid(), os.Getpid(), group)), 0600); err != nil {
		t.Fatal(err)
	}
	if err := verifyModelListener(t.Context(), dir, "gguf-fixture", serverPort, clientPort, true); err != nil {
		t.Fatal(err)
	}
	p := &OpenAIProvider{baseURL: "http://127.0.0.1:18789/v1", client: &http.Client{}, libraryDirectory: dir}
	if p.completionClient("gguf-fixture", false) == p.client {
		t.Fatal("managed deployment bypassed the ownership check")
	}
	p.baseURL = "http://LOCALHOST.:18789/v1"
	if p.completionClient("gguf-fixture", false) == p.client {
		t.Fatal("localhost spelling bypassed the lease")
	}
	if p.completionClient("unrelated", false) != p.client {
		t.Fatal("external configuration unexpectedly changed transport")
	}
}

func TestRejectedTopPDoesNotRetryAuthFailure(t *testing.T) {
	for _, code := range []int{400, 401} {
		calls := 0
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			calls++
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
			}
			if calls == 1 {
				if _, ok := body["top_p"]; !ok {
					t.Error("explicit top_p missing")
				}
				w.WriteHeader(code)
				fmt.Fprint(w, `{"error":"unsupported parameter top_p"}`)
				return
			}
			if _, ok := body["top_p"]; ok {
				t.Error("retried with rejected top_p")
			}
			fmt.Fprint(w, `{"choices":[{"message":{"content":"ok"}}]}`)
		}))
		p, _ := NewOpenAI(server.URL, "")
		top := 0.8
		_, err := p.Chat(t.Context(), "fixture", nil, ChatOptions{TopP: &top})
		server.Close()
		if code == 400 && (err != nil || calls != 2) || code == 401 && (err == nil || calls != 1) {
			t.Fatalf("code=%d calls=%d err=%v", code, calls, err)
		}
	}
}
