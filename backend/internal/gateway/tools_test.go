package gateway

import "testing"

func TestUnsupportedStructuredProviderFailsBeforeNetwork(t *testing.T) {
	g := newTestGateway(t)
	for _, provider := range []string{"anthropic", "gemini", "unknown"} {
		if err := g.ToolCapability(&ModelConfig{Provider: provider, BaseURL: "http://127.0.0.1:1"}); err == nil {
			t.Fatal("unsupported provider accepted", provider)
		}
	}
}
