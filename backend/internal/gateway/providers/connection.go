package providers

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// UpstreamHTTPError keeps HTTP status separate from provider-specific error text.
// Diagnostics summarize the failure without returning the upstream text.
type UpstreamHTTPError struct {
	Status int
	Detail string
}

func (e *UpstreamHTTPError) Error() string { return fmt.Sprintf("upstream %d: %s", e.Status, e.Detail) }

type connectionProbeKey struct{}

func ConnectionProbeContext(ctx context.Context) context.Context {
	return context.WithValue(ctx, connectionProbeKey{}, true)
}

func sendChatRequest(client *http.Client, req *http.Request) (*http.Response, error) {
	if req.Context().Value(connectionProbeKey{}) == true {
		// Report the selected endpoint. Never forward draft credentials on redirect.
		probeClient := *client
		probeClient.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
		return probeClient.Do(req)
	}
	return client.Do(req)
}

// Ollama's compatibility API uses /v1, including when the user provided
// the address of the native API. Preserve explicitly configured proxy paths.
func OllamaCompatibilityBase(base string) string {
	base = strings.TrimRight(base, "/")
	u, err := url.Parse(base)
	if err != nil || u.Host == "" || u.RawQuery != "" || u.Fragment != "" {
		return base
	}
	if u.Path == "" || u.Path == "/api" {
		u.Path = "/v1"
		return u.String()
	}
	return base
}

func GeminiGenerateURL(base, model string) string {
	return strings.TrimRight(base, "/") + "/models/" + url.PathEscape(strings.TrimPrefix(model, "models/")) + ":generateContent"
}
