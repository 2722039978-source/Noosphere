package gateway

import (
	"context"
	"encoding/json"
	stderrors "errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/noosphere/backend/internal/errors"
	"github.com/noosphere/backend/internal/gateway/providers"
	"github.com/noosphere/backend/internal/workspaces"
)

// acquireCall shares one concurrency budget between streaming and regular calls.
// Both a queued request and its eventual upstream request obey the caller's context.
func (g *Gateway) acquireCall(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	timer := time.NewTimer(60 * time.Second)
	defer timer.Stop()
	select {
	case g.sem <- struct{}{}:
		if err := ctx.Err(); err != nil {
			<-g.sem
			return err
		}
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return errors.New(errors.ErrUnavailable, errors.CatGateway, "too many concurrent requests")
	}
}

func (g *Gateway) appendLog(entry CallLog, credential string) {
	entry.RequestPreview = providers.RedactCredential(entry.RequestPreview, credential)
	entry.ResponsePreview = providers.RedactCredential(entry.ResponsePreview, credential)
	entry.Error = providers.RedactCredential(entry.Error, credential)
	g.mu.Lock()
	defer g.mu.Unlock()
	g.logs = append(g.logs, entry)
	if len(g.logs) > 500 {
		g.logs = g.logs[len(g.logs)-500:]
	}
}

func streamLogStatus(err error) string {
	switch {
	case err == nil:
		return "ok"
	case stderrors.Is(err, context.Canceled):
		return "cancelled"
	case stderrors.Is(err, io.ErrUnexpectedEOF):
		return "interrupted"
	default:
		return "error"
	}
}

func (g *Gateway) HandleChatStream(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		RequestID      string                    `json:"request_id"`
		WorkspaceID    string                    `json:"workspace_id"`
		SessionID      string                    `json:"session_id"`
		Messages       []map[string]string       `json:"messages"`
		ModelID        string                    `json:"model_id"`
		Project        string                    `json:"project"`
		SystemPrompt   string                    `json:"system_prompt"`
		Temperature    float64                   `json:"temperature"`
		MaxTokens      int                       `json:"max_tokens"`
		AttachmentTurn workspaces.AttachmentTurn `json:"attachment_turn"`
		Preview        bool                      `json:"preview"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		errors.WriteHTTP(w, r, errors.InvalidInput(errors.CatGateway, "invalid request body"))
		return
	}
	if body.Preview && (strings.TrimSpace(body.ModelID) == "" || body.SessionID != "" || body.AttachmentTurn.MessageID != "" ||
		len(body.AttachmentTurn.IDs) != 0 || body.AttachmentTurn.Question != "") {
		errors.WriteHTTP(w, r, errors.InvalidInput(errors.CatGateway, "模型试聊需要明确选择模型，且仅接收文本"))
		return
	}
	model, modelErr := g.SessionModel(body.WorkspaceID, body.SessionID, body.ModelID)
	if modelErr != nil {
		errors.WriteHTTP(w, r, errors.InvalidInput(errors.CatGateway, modelErr.Error()))
		return
	}
	if err := model.credentialError(); err != nil {
		errors.WriteHTTP(w, r, err)
		return
	}
	if body.Preview {
		endpoints, problem := connectionEndpoints(model)
		if problem != "" || !endpoints.local {
			errors.WriteHTTP(w, r, errors.InvalidInput(errors.CatGateway, "试聊仅支持本机回环模型，请重新选择连接"))
			return
		}
	}
	if _, ok := w.(http.Flusher); !ok {
		errors.WriteHTTP(w, r, errors.Internal(errors.CatGateway, "streaming not supported"))
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 120*time.Second)
	defer cancel()
	messages, attachments, err := g.resolveChatAttachments(ctx, body.WorkspaceID, body.SessionID, body.AttachmentTurn, body.Messages)
	if err != nil {
		errors.WriteHTTP(w, r, errors.InvalidInput(errors.CatGateway, err.Error()))
		return
	}
	start := time.Now()
	var full strings.Builder
	var callErr error
	var execution *workspaces.Execution
	if g.workspaceManager != nil && !body.Preview {
		var err error
		ctx, execution, err = g.workspaceManager.Start(ctx, workspaces.Run{ID: body.RequestID, WorkspaceID: body.WorkspaceID, SessionID: body.SessionID, Kind: "chat", Title: "AI 对话", ModelID: model.ID})
		if err != nil {
			errors.WriteHTTP(w, r, errors.Wrap(err, errors.ErrUnavailable, errors.CatGateway, "cannot start chat task"))
			return
		}
		execution.Emit("queued", "等待模型调用额度", nil)
		if len(attachments.Citations) > 0 {
			execution.Emit("attachments", "已准备附件引用", attachments)
		}
		defer func() {
			if err := execution.Finish(map[string]any{"content": full.String(), "model_id": model.ID}, callErr); err != nil {
				log.Printf("[workspaces] could not finish chat task %s: %v", execution.ID, err)
			}
		}()
	}
	if !body.Preview {
		defer func() {
			entry := CallLog{
				ID:      fmt.Sprintf("log-%d", time.Now().UnixNano()),
				ModelID: model.ID, Provider: model.Provider, Project: body.Project,
				RequestPreview: g.preview(body.Messages), ResponsePreview: previewText(full.String()),
				Status: streamLogStatus(callErr), LatencyMs: float64(time.Since(start)) / float64(time.Millisecond),
				Timestamp: time.Now().Format(time.RFC3339),
			}
			if callErr != nil {
				entry.Error = callErr.Error()
			}
			g.appendLog(entry, model.APIKey)
		}()
	}

	foregroundDone, err := g.beginInteractive(ctx)
	if err != nil {
		callErr = err
		if r.Context().Err() == nil {
			errors.WriteHTTP(w, r, errors.Wrap(err, errors.ErrUnavailable, errors.CatGateway, "stream admission failed"))
		}
		return
	}
	defer foregroundDone()
	if callErr = g.acquireCall(ctx); callErr != nil {
		if r.Context().Err() == nil {
			errors.WriteHTTP(w, r, errors.Wrap(callErr, errors.ErrUnavailable, errors.CatGateway, "stream queue failed"))
		}
		return
	}
	defer func() { <-g.sem }()
	if execution != nil {
		execution.Emit("waiting", "正在连接模型", nil)
	}
	provider, err := g.getOrCreateProvider(model)
	if err != nil {
		callErr = err
		errors.WriteHTTP(w, r, errors.Wrap(err, errors.ErrInternal, errors.CatGateway, "provider init failed"))
		return
	}
	pMsgs := make([]providers.Message, len(messages))
	for i, m := range messages {
		pMsgs[i] = providers.Message{Role: m["role"], Content: m["content"]}
	}
	temp, maxT := body.Temperature, body.MaxTokens
	if temp == 0 {
		temp = 0.1
	}
	if maxT == 0 {
		maxT = 4096
	}
	if model.MaxTokens > 0 {
		maxT = model.MaxTokens
	}
	if body.Preview && (maxT < 1 || maxT > 768) {
		maxT = 768
	}
	finishInference := g.countInference(false)
	streamCtx, stopStream := context.WithCancel(ctx)
	ch, err := provider.ChatStream(streamCtx, model.ModelName, pMsgs, providers.ChatOptions{
		SystemPrompt: body.SystemPrompt, Temperature: temp, MaxTokens: maxT,
		MaxTokensField: model.MaxTokensKey(), OmitTemperature: !model.TemperatureEnabled(),
	})
	if err != nil {
		stopStream()
		finishInference()
		callErr = err
		if ctx.Err() != nil {
			callErr = ctx.Err()
		}
		if r.Context().Err() == nil {
			errors.WriteHTTP(w, r, errors.Wrap(callErr, errors.ErrUnavailable, errors.CatGateway, "stream setup failed"))
		}
		return
	}
	// 关闭生产者并等待通道结束后释放推理额度，任务状态保留原始请求结果。
	defer func() {
		stopStream()
		for range ch {
		}
		finishInference()
	}()
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	controller := http.NewResponseController(w)
	writeEvent := func(event map[string]string) error {
		// Bound a slow/disconnected client's write, so it cannot hold an upstream slot forever.
		_ = controller.SetWriteDeadline(time.Now().Add(10 * time.Second))
		data, err := json.Marshal(event)
		if err != nil {
			return err
		}
		if _, err := fmt.Fprintf(w, "data: %s\n\n", data); err != nil {
			return err
		}
		return controller.Flush()
	}
	defer controller.SetWriteDeadline(time.Time{})
	_ = controller.SetWriteDeadline(time.Now().Add(10 * time.Second))
	if err := controller.Flush(); err != nil {
		callErr = err
		return
	}
	fail := func(err error) {
		callErr = err
		if ctx.Err() != nil {
			callErr = ctx.Err()
		}
		if r.Context().Err() == nil {
			status := "failed"
			if stderrors.Is(callErr, context.Canceled) {
				status = "cancelled"
			}
			_ = writeEvent(map[string]string{"type": "error", "error": callErr.Error(), "status": status})
		}
	}
	firstToken := true
	for {
		select {
		case <-ctx.Done():
			fail(ctx.Err())
			return
		case chunk, ok := <-ch:
			if ctx.Err() != nil {
				fail(ctx.Err())
				return
			}
			if !ok {
				fail(fmt.Errorf("stream ended without a completion marker: %w", io.ErrUnexpectedEOF))
				return
			}
			if chunk.Error != nil {
				fail(chunk.Error)
				return
			}
			if chunk.Token != "" {
				if firstToken && execution != nil {
					execution.Emit("streaming", "正在生成回复", nil)
				}
				firstToken = false
				full.WriteString(chunk.Token)
				if callErr = writeEvent(map[string]string{"type": "token", "token": chunk.Token}); callErr != nil {
					return
				}
			}
			if chunk.Done {
				// Content is an authoritative final snapshot, never an additional delta.
				if chunk.Content != "" {
					full.Reset()
					full.WriteString(chunk.Content)
				}
				if execution != nil {
					if err := execution.Finish(map[string]any{"content": full.String(), "model_id": model.ID}, nil); err != nil {
						// Finish releases its context. Preserve the storage error instead
						// of reporting that internal release as a user cancellation.
						callErr = fmt.Errorf("save chat result: %w", err)
						_ = writeEvent(map[string]string{"type": "error", "error": callErr.Error(), "status": "failed"})
						return
					}
					committed, err := g.workspaceManager.Run(execution.ID)
					if err != nil || committed.Status != "completed" {
						// A concurrent task-center cancellation may have won the commit.
						callErr = context.Canceled
						status := "cancelled"
						if err != nil {
							callErr = err
							status = "failed"
						} else if committed.Error != "" {
							callErr = fmt.Errorf("%s", committed.Error)
							status = "failed"
						}
						_ = writeEvent(map[string]string{"type": "error", "error": callErr.Error(), "status": status})
						return
					}
				}
				callErr = writeEvent(map[string]string{"type": "done", "content": full.String(), "status": "completed"})
				return
			}
		}
	}
}

func previewText(content string) string {
	runes := []rune(content)
	if len(runes) > 200 {
		return string(runes[:200])
	}
	return content
}
