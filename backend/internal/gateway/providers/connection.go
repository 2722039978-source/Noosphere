package providers

import (
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

func sendChatRequest(client *http.Client, req *http.Request) (*http.Response, error) {
	// Keep normal inference on the same endpoint validated by the connection
	// probe. Redirects could forward provider-specific headers and prompts.
	direct := *client
	direct.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return direct.Do(req)
}

// RedactCredential removes echoed credentials and a partial credential at the
// end of a bounded response body. Call before truncating for display.
func RedactCredential(message, apiKey string) string {
	if apiKey == "" {
		return message
	}
	message = strings.ReplaceAll(message, apiKey, "[redacted]")
	for size := min(len(apiKey)-1, len(message)); size > 0; size-- {
		if strings.HasSuffix(message, apiKey[:size]) {
			return message[:len(message)-size] + "[redacted]"
		}
	}
	return message
}

// Ollama's compatibility API uses /v1, including when the user provided
// the address of the native API. Preserve explicitly configured proxy paths.
func OllamaCompatibilityBase(base string) string {
	base = strings.TrimRight(base, "/")
	u, err := url.Parse(base)
	if err != nil || u.Host == "" || u.RawQuery != "" || u.Fragment != "" {
		return base
	}
	if u.Path == "" {
		return base + "/v1"
	}
	if strings.HasSuffix(u.EscapedPath(), "/api") {
		return strings.TrimSuffix(base, "/api") + "/v1"
	}
	return base
}

func GeminiGenerateURL(base, model string) string {
	return strings.TrimRight(base, "/") + "/models/" + url.PathEscape(strings.TrimPrefix(model, "models/")) + ":generateContent"
}
