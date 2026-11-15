package providers

import (
	"context"
	"fmt"
	"io"
)

// Message represents a chat message.
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// ChatOptions configures a chat completion request.
type ChatOptions struct {
	Temperature  float64
	MaxTokens    int
	SystemPrompt string

	// MaxTokensField selects the request key for the output budget. Accepted
	// values are "max_tokens" and "max_completion_tokens"; an empty value lets
	// the provider choose from the endpoint and model name.
	MaxTokensField string
	// OmitTemperature drops the temperature field entirely. Reasoning-style
	// models reject any explicit value, and some providers reject the key.
	OmitTemperature bool
	// ToolTransport selects the structured-tool endpoint: "chat" (default,
	// /chat/completions) or "responses" (/v1/responses).
	ToolTransport string
}

// ChatResponse is the result of a non-streaming chat completion.
type ChatResponse struct {
	Content   string
	Model     string
	TokensIn  int
	TokensOut int
}

// StreamChunk carries a delta, or one terminal result. Content on Done is the
// complete authoritative answer. A closed channel without Done is an interruption.
type StreamChunk struct {
	Token   string
	Done    bool
	Content string
	Error   error
}

// sendChunk lets cancellation release producers even when the consumer stopped
// draining a full channel (for example after an HTTP client disconnects).
func sendChunk(ctx context.Context, ch chan<- StreamChunk, chunk StreamChunk) bool {
	if ctx.Err() != nil {
		return false
	}
	select {
	case ch <- chunk:
		return true
	case <-ctx.Done():
		return false
	}
}

func streamReadError(ctx context.Context, err error) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if err != nil {
		return fmt.Errorf("read stream: %w", err)
	}
	return fmt.Errorf("upstream stream ended without a completion marker: %w", io.ErrUnexpectedEOF)
}

// LLMProvider abstracts a model provider backend.
type LLMProvider interface {
	Name() string
	Chat(ctx context.Context, model string, messages []Message, opts ChatOptions) (*ChatResponse, error)
	ChatStream(ctx context.Context, model string, messages []Message, opts ChatOptions) (<-chan StreamChunk, error)
}

// ProviderFactory creates an LLMProvider from configuration.
type ProviderFactory func(baseURL, apiKey string) (LLMProvider, error)

var registry = map[string]ProviderFactory{}

func Register(name string, factory ProviderFactory) {
	registry[name] = factory
}

func Get(name string) ProviderFactory {
	return registry[name]
}

func Names() []string {
	names := make([]string, 0, len(registry))
	for n := range registry {
		names = append(names, n)
	}
	return names
}
