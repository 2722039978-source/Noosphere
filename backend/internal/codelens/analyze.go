package codelens

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// Analysis is the actual analyze/file protocol. The service does not attest a
// source hash or parse validity; consumers must bind controlled source bytes.
type Analysis struct {
	File      string            `json:"file"`
	Language  string            `json:"language"`
	Lines     int               `json:"lines"`
	Entities  []Entity          `json:"entities"`
	Imports   []json.RawMessage `json:"imports"`
	Calls     []json.RawMessage `json:"calls"`
	SizeBytes int               `json:"size_bytes"`
}
type Entity struct {
	Name      string `json:"name"`
	Type      string `json:"type"`
	Line      int    `json:"line"`
	EndLine   int    `json:"end_line"`
	StartByte int    `json:"start_byte"`
	EndByte   int    `json:"end_byte"`
	Scope     string `json:"scope"`
}
type HealthEvidence struct {
	Status    string   `json:"status"`
	Service   string   `json:"service"`
	Version   string   `json:"version"`
	Parser    string   `json:"parser"`
	Languages []string `json:"languages"`
}

// workerJSON is deliberately independent of the indexing proxy and its
// ModifyResponse hook. It reuses only this Adapter's private worker lifecycle
// and bearer credential, with a bounded response and a cancellable timeout.
func (a *Adapter) workerJSON(ctx context.Context, method, path string, body any, out any) ([]byte, error) {
	if a.proxy == nil || a.done == nil {
		return nil, fmt.Errorf("CodeLens worker unavailable")
	}
	select {
	case <-a.done:
		return nil, fmt.Errorf("CodeLens worker exited")
	default:
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, method, fmt.Sprintf("http://127.0.0.1:%d/api/v1/%s", a.port, path), bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+a.token)
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 5 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("CodeLens %s HTTP %d", path, resp.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, (256<<10)+1))
	if err != nil {
		return nil, err
	}
	if len(raw) > 256<<10 {
		return nil, fmt.Errorf("CodeLens response exceeds bound")
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	if err = dec.Decode(out); err != nil {
		return nil, fmt.Errorf("CodeLens JSON: %w", err)
	}
	if err = dec.Decode(new(any)); err != io.EOF {
		return nil, fmt.Errorf("CodeLens trailing JSON")
	}
	return raw, nil
}
func (a *Adapter) AnalyzeFile(ctx context.Context, root, file string) (Analysis, []byte, error) {
	var result Analysis
	raw, err := a.workerJSON(ctx, http.MethodPost, "analyze/file", struct {
		Root string `json:"project_path"`
		File string `json:"file"`
	}{root, file}, &result)
	return result, raw, err
}
func (a *Adapter) HealthEvidence(ctx context.Context) (HealthEvidence, []byte, error) {
	var result HealthEvidence
	raw, err := a.workerJSON(ctx, http.MethodGet, "health", nil, &result)
	return result, raw, err
}
