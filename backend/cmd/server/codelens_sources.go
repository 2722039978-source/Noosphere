package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/noosphere/backend/internal/agent"
	"github.com/noosphere/backend/internal/codelens"
	appErrors "github.com/noosphere/backend/internal/errors"
	"github.com/noosphere/backend/internal/knowledge"
	"github.com/noosphere/backend/internal/workspaces"
)

var errTraceWorkerUnavailable = errors.New("CodeLens 源码服务尚未就绪")

func (app *App) traceCodeLens(ctx context.Context, ws workspaces.Workspace, query string) (codelens.TraceResult, error) {
	worker := app.CodeLens.Load()
	if worker == nil {
		return codelens.TraceResult{}, errTraceWorkerUnavailable
	}
	revision, err := codelens.IndexRevision(app.Engine, ws.ProjectID)
	if err != nil {
		return codelens.TraceResult{}, err
	}
	trace, err := worker.Trace(ctx, ws.ProjectPath, query, revision)
	if err != nil {
		return codelens.TraceResult{}, err
	}
	trace, err = agent.ScopeProjectTrace(ctx, ws, trace)
	if err != nil {
		return codelens.TraceResult{}, err
	}
	if !sameProject(app.Workspaces, ws) {
		return codelens.TraceResult{}, knowledge.ErrConflict
	}
	if current, err := codelens.IndexRevision(app.Engine, ws.ProjectID); err != nil || current != revision {
		return codelens.TraceResult{}, knowledge.ErrConflict
	}
	return trace, nil
}

func sameProject(manager *workspaces.Manager, workspace workspaces.Workspace) bool {
	current, err := manager.Workspace(workspace.ID)
	return err == nil && current.ProjectPath == workspace.ProjectPath && current.ProjectID == workspace.ProjectID
}

func decodeSourceRequest(w http.ResponseWriter, r *http.Request, value any) *appErrors.AppError {
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8192))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil || decoder.Decode(new(any)) != io.EOF {
		return appErrors.InvalidInput(appErrors.CatCodeLens, "源码查询请求格式无效")
	}
	return nil
}

func codeLensWorkspace(manager *workspaces.Manager, id string) (workspaces.Workspace, *appErrors.AppError) {
	ws, err := manager.Workspace(id)
	if errors.Is(err, workspaces.ErrNotFound) {
		return ws, appErrors.NotFound(appErrors.CatCodeLens, "工作区不存在")
	}
	if err != nil {
		return ws, appErrors.Wrap(err, appErrors.ErrInternal, appErrors.CatCodeLens, "无法读取工作区")
	}
	if ws.ProjectPath == "" {
		return ws, appErrors.InvalidInput(appErrors.CatCodeLens, "请先为工作区打开项目目录")
	}
	return ws, nil
}

func writeCodeLensSource(w http.ResponseWriter, value any) *appErrors.AppError {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(value); err != nil {
		return appErrors.Wrap(err, appErrors.ErrInternal, appErrors.CatCodeLens, "无法返回源码结果")
	}
	return nil
}

func (app *App) registerCodeLensSources() {
	manager := app.Workspaces
	app.Mux.HandleFunc("POST /api/v1/codelens/trace", appErrors.WrapHandler(func(w http.ResponseWriter, r *http.Request) *appErrors.AppError {
		var input struct {
			WorkspaceID string `json:"workspace_id"`
			Query       string `json:"query"`
		}
		if problem := decodeSourceRequest(w, r, &input); problem != nil {
			return problem
		}
		ws, problem := codeLensWorkspace(manager, input.WorkspaceID)
		if problem != nil {
			return problem
		}
		query := strings.TrimSpace(input.Query)
		if query == "" || len(query) > 1000 {
			return appErrors.InvalidInput(appErrors.CatCodeLens, "请输入 1000 字节以内的源码查询")
		}
		trace, err := app.traceCodeLens(r.Context(), ws, query)
		switch {
		case errors.Is(err, codelens.ErrTraceIndexRequired):
			return appErrors.New(appErrors.ErrConflict, appErrors.CatCodeLens, err.Error())
		case errors.Is(err, knowledge.ErrConflict):
			return appErrors.New(appErrors.ErrConflict, appErrors.CatCodeLens, "项目索引已变化，请重新查询")
		case err != nil:
			return appErrors.Wrap(err, appErrors.ErrUnavailable, appErrors.CatCodeLens, "源码追踪不可用，请检查 CodeLens 服务状态")
		}
		return writeCodeLensSource(w, trace)
	}))
	app.Mux.HandleFunc("POST /api/v1/codelens/source", appErrors.WrapHandler(func(w http.ResponseWriter, r *http.Request) *appErrors.AppError {
		var input struct {
			WorkspaceID string `json:"workspace_id"`
			File        string `json:"file"`
			StartLine   int    `json:"start_line"`
			EndLine     int    `json:"end_line"`
			SourceHash  string `json:"source_hash"`
		}
		if problem := decodeSourceRequest(w, r, &input); problem != nil {
			return problem
		}
		ws, problem := codeLensWorkspace(manager, input.WorkspaceID)
		if problem != nil {
			return problem
		}
		view, err := agent.ReadProjectSource(r.Context(), ws, input.File, input.StartLine, input.EndLine, input.SourceHash)
		if err != nil {
			return appErrors.Wrap(err, appErrors.ErrInvalidInput, appErrors.CatCodeLens, "无法读取此源码位置")
		}
		if !sameProject(manager, ws) {
			return appErrors.New(appErrors.ErrConflict, appErrors.CatCodeLens, "工作区的项目目录已变化")
		}
		return writeCodeLensSource(w, view)
	}))
	app.Mux.HandleFunc("POST /api/v1/codelens/project-search", appErrors.WrapHandler(func(w http.ResponseWriter, r *http.Request) *appErrors.AppError {
		var input struct {
			WorkspaceID string `json:"workspace_id"`
			Query       string `json:"query"`
		}
		if problem := decodeSourceRequest(w, r, &input); problem != nil {
			return problem
		}
		ws, problem := codeLensWorkspace(manager, input.WorkspaceID)
		if problem != nil {
			return problem
		}
		query := strings.TrimSpace(input.Query)
		if query == "" || len(query) > 1000 {
			return appErrors.InvalidInput(appErrors.CatCodeLens, "请输入 1000 字节以内的搜索文字")
		}
		ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
		defer cancel()
		out := agent.SearchProjectSource(ctx, ws, query)
		if !out.Success {
			return appErrors.New(appErrors.ErrInvalidInput, appErrors.CatCodeLens, out.Error)
		}
		if !sameProject(manager, ws) {
			return appErrors.New(appErrors.ErrConflict, appErrors.CatCodeLens, "工作区的项目目录已变化")
		}
		return writeCodeLensSource(w, struct {
			Results []agent.Evidence `json:"results"`
			Partial bool             `json:"partial"`
		}{out.Evidence, out.Partial})
	}))
}
