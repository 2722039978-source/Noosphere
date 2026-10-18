package providers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDeepSeekDirectResponseForChatAndStream(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]interface{}
		json.NewDecoder(r.Body).Decode(&body)
		thinking, ok := body["thinking"].(map[string]interface{})
		if !ok || thinking["type"] != "disabled" {
			t.Error("DeepSeek must explicitly request direct response, avoiding invisible reasoning-only replies")
		}
		if body["stream"] == true {
			w.Header().Set("Content-Type", "text/event-stream")
			w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"OK\"}}]}\n\ndata: [DONE]\n\n"))
			return
		}
		w.Write([]byte(`{"choices":[{"message":{"content":"OK"}}]}`))
	}))
	defer upstream.Close()
	factory := Get("deepseek")
	if factory == nil {
		t.Fatal("DeepSeek preset provider is missing")
	}
	p, err := factory(upstream.URL, "fixture")
	if err != nil {
		t.Fatal(err)
	}
	response, err := p.Chat(context.Background(), "deepseek-flash", nil, ChatOptions{})
	if err != nil || response.Content != "OK" {
		t.Fatal(response, err)
	}
	stream, err := p.ChatStream(context.Background(), "deepseek-flash", nil, ChatOptions{})
	if err != nil {
		t.Fatal(err)
	}
	var content string
	var done bool
	for event := range stream {
		content += event.Token
		done = done || event.Done
		if event.Error != nil {
			t.Fatal(event.Error)
		}
	}
	if content != "OK" || !done {
		t.Fatalf("incomplete stream: %q %v", content, done)
	}
}

func TestUpstreamErrorsDoNotExposeCredentials(t *testing.T) {
	const key = "fixture-secret-to-redact"
	for _, mode := range []string{"chat", "stream-http", "stream-event"} {
		t.Run(mode, func(t *testing.T) {
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if mode == "stream-event" {
					w.Header().Set("Content-Type", "text/event-stream")
					event, _ := json.Marshal(map[string]interface{}{"error": map[string]string{"message": "denied " + key}})
					w.Write([]byte("data: " + string(event) + "\n\n"))
					return
				}
				w.WriteHeader(401)
				padding := 295
				if mode == "stream-http" {
					padding = 4090
				}
				w.Write([]byte(strings.Repeat("x", padding) + key))
			}))
			defer upstream.Close()
			p, _ := NewOpenAI(upstream.URL, key)
			var message string
			if mode == "chat" {
				_, err := p.Chat(context.Background(), "test", nil, ChatOptions{})
				if err != nil {
					message = err.Error()
				}
			} else {
				events, err := p.ChatStream(context.Background(), "test", nil, ChatOptions{})
				if err != nil {
					message = err.Error()
				} else {
					for event := range events {
						if event.Error != nil {
							message = event.Error.Error()
						}
					}
				}
			}
			if message == "" || strings.Contains(message, "fixture") {
				t.Fatal("upstream error was missing or leaked the credential prefix")
			}
		})
	}
}

func TestRedactBoundedRepeatedCredential(t *testing.T) {
	const key = "fixture-secret-to-redact"
	p := &OpenAIProvider{apiKey: key}
	message := strings.Repeat(key, 400)[:4096+len(key)]
	if strings.Contains(p.redact(message), "fixture") {
		t.Fatal("bounded repeated credential leaked a truncated prefix")
	}
}
