package providers

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

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
