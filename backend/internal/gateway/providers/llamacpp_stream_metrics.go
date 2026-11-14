package providers

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"mime"
	"net"
	"net/http"
	"net/http/httptrace"
	"strings"
	"time"
)

type llamaStreamChunk struct {
	Model   string          `json:"model"`
	Error   json.RawMessage `json:"error"`
	Timings map[string]any  `json:"timings"`
	Usage   map[string]any  `json:"usage"`
	Choices []struct {
		Index  int     `json:"index"`
		Finish *string `json:"finish_reason"`
		Delta  struct {
			Role      string          `json:"role"`
			Content   string          `json:"content"`
			Reasoning string          `json:"reasoning_content"`
			ToolCalls json.RawMessage `json:"tool_calls"`
		} `json:"delta"`
	} `json:"choices"`
}

// Keep the raw JSON events in the normalized terminal response as evidence.
// The non-streaming adapter and its historical runs use their original path.
func (p *LocalCompatibleProvider) readLlamaStream(ctx context.Context, client *http.Client, origin, model string, body map[string]any, start time.Time) (map[string]any, json.RawMessage, *int64, *int64, *ProcessUsage, error) {
	problem := func(err error) (map[string]any, json.RawMessage, *int64, *int64, *ProcessUsage, error) {
		return nil, nil, nil, nil, nil, err
	}
	body["stream"] = true
	body["stream_options"] = map[string]bool{"include_usage": true}
	body["timings_per_token"] = false
	payload, err := json.Marshal(body)
	if err != nil || len(payload) > 1<<20 {
		return problem(evaluationError("INVALID_REQUEST", "invalid native stream request"))
	}
	var endpoint *net.TCPAddr
	// GotConn is synchronous for HTTP/1.1. Resolve the actual connected IP and
	// port, independent of URL spelling and any local DNS address family.
	trace := &httptrace.ClientTrace{GotConn: func(info httptrace.GotConnInfo) {
		endpoint, _ = info.Conn.RemoteAddr().(*net.TCPAddr)
	}}
	req, err := http.NewRequestWithContext(httptrace.WithClientTrace(ctx, trace), http.MethodPost, origin+"/v1/chat/completions", bytes.NewReader(payload))
	if err != nil {
		return problem(evaluationError("INVALID_REQUEST", "invalid native stream endpoint"))
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	if p.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+p.apiKey)
	}
	resp, err := client.Do(req)
	if err != nil {
		return problem(EvaluationContextError(err))
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return problem(&EvaluationError{Code: "PROVIDER_UNAVAILABLE", Message: "native provider rejected streaming request", HTTPStatus: resp.StatusCode})
	}
	media, _, _ := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	if media != "text/event-stream" {
		return problem(evaluationError("INVALID_OUTPUT", "native provider did not return an event stream"))
	}
	finishUsage := observeListenerUsage(ctx, endpoint)
	defer finishUsage()
	return p.decodeLlamaStream(ctx, resp.Body, model, start, finishUsage)
}

func (p *LocalCompatibleProvider) decodeLlamaStream(ctx context.Context, source io.Reader, model string, start time.Time, finishUsage func() *ProcessUsage) (map[string]any, json.RawMessage, *int64, *int64, *ProcessUsage, error) {
	limited := &io.LimitedReader{R: source, N: evaluationResponseLimit/2 + 1}
	scanner := bufio.NewScanner(limited)
	scanner.Buffer(make([]byte, 4096), 256<<10)
	var content, reasoning strings.Builder
	var events []json.RawMessage
	var timings, usage map[string]any
	var firstToken, firstAnswer *int64
	var finish string
	var done bool
	invalid := func(message string) (map[string]any, json.RawMessage, *int64, *int64, *ProcessUsage, error) {
		return nil, nil, nil, nil, nil, evaluationError("INVALID_OUTPUT", message)
	}
	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		if !bytes.HasPrefix(line, []byte("data:")) {
			continue
		}
		data := bytes.TrimSpace(line[5:])
		if bytes.Equal(data, []byte("[DONE]")) {
			done = true
			break
		}
		if len(events) >= 16384 {
			return invalid("native stream contains too many events")
		}
		var chunk llamaStreamChunk
		decoder := json.NewDecoder(bytes.NewReader(data))
		decoder.UseNumber()
		if decoder.Decode(&chunk) != nil || decoder.Decode(new(any)) != io.EOF || len(chunk.Error) != 0 && !bytes.Equal(chunk.Error, []byte("null")) {
			return invalid("invalid native stream event")
		}
		if chunk.Model != model {
			return nil, nil, nil, nil, nil, evaluationError("MODEL_DRIFT", "native stream model differs from registered identity")
		}
		if len(chunk.Choices) > 1 {
			return invalid("native stream must have one choice")
		}
		if len(chunk.Choices) == 1 {
			choice := chunk.Choices[0]
			delta := choice.Delta
			if finish != "" || choice.Index != 0 || len(delta.ToolCalls) != 0 && string(delta.ToolCalls) != "null" && string(delta.ToolCalls) != "[]" {
				return invalid("invalid terminal text stream")
			}
			elapsed := int64(time.Since(start))
			if firstToken == nil && (delta.Content != "" || delta.Reasoning != "") {
				firstToken = &elapsed
			}
			if firstAnswer == nil && delta.Content != "" {
				firstAnswer = &elapsed
			}
			content.WriteString(delta.Content)
			reasoning.WriteString(delta.Reasoning)
			if choice.Finish != nil {
				finish = *choice.Finish
				if finish != "stop" && finish != "length" {
					return invalid("unknown native stream stop reason")
				}
			}
		}
		if chunk.Timings != nil {
			timings = chunk.Timings
		}
		if chunk.Usage != nil {
			usage = chunk.Usage
		}
		if p.apiKey != "" {
			var value any
			if json.Unmarshal(data, &value) != nil {
				return invalid("invalid native stream evidence")
			}
			if redactNativeStrings(value, p.apiKey) {
				data, _ = json.Marshal(value)
			}
		}
		events = append(events, bytes.Clone(data))
	}
	if ctx.Err() != nil {
		return nil, nil, nil, nil, nil, EvaluationContextError(ctx.Err())
	}
	if !done || finish == "" || scanner.Err() != nil || limited.N == 0 {
		return invalid("native stream ended without bounded terminal evidence")
	}
	result := map[string]any{
		"model": model, "timings": timings, "usage": usage,
		"choices": []any{map[string]any{"finish_reason": finish, "message": map[string]any{"content": p.redact(content.String()), "reasoning_content": p.redact(reasoning.String())}}},
	}
	saved := make(map[string]any, len(result)+1)
	for key, value := range result {
		saved[key] = value
	}
	saved["stream_events"] = events
	raw, err := json.Marshal(saved)
	if err != nil || len(raw) > evaluationResponseLimit {
		return invalid("native stream evidence exceeds size limit")
	}
	return result, raw, firstToken, firstAnswer, finishUsage(), nil
}
