package knowledge

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/noosphere/backend/internal/codelens"
	"github.com/noosphere/backend/internal/workspaces"
)

type SourceTraceLookup func(context.Context, workspaces.Workspace, string) (codelens.TraceResult, error)
type SourceTraceExcerpt func(context.Context, workspaces.Workspace, codelens.TraceLocation) (string, error)

func (s *Service) SetSourceTrace(lookup SourceTraceLookup, excerpt SourceTraceExcerpt) {
	s.trace = lookup
	s.read = excerpt
}

func (s *Service) TraceAvailable() bool { return s.trace != nil }

func (s *Service) Trace(ctx context.Context, ws workspaces.Workspace, query string) (codelens.TraceResult, error) {
	if s.trace == nil {
		return codelens.TraceResult{}, fmt.Errorf("CodeLens 源码追踪不可用")
	}
	return s.trace(ctx, ws, query)
}

func (s *Service) addSourceTrace(ctx context.Context, ws workspaces.Workspace, query string, limit int, add func(Citation), warn func(string)) {
	if !s.TraceAvailable() || s.read == nil || limit < 1 || len(query) > 1000 {
		return
	}
	trace, err := s.Trace(ctx, ws, query)
	if err != nil {
		if !errors.Is(err, codelens.ErrTraceIndexRequired) {
			warn("本次未取得源码关系，请在项目代码面板检查索引状态。")
		}
		return
	}
	if len(trace.Matches) == 0 {
		return
	}
	seen := map[string]bool{}
	addLocation := func(location codelens.TraceLocation, relation string) {
		key := fmt.Sprintf("%s:%d", location.File, location.StartLine)
		if limit == 0 || seen[key] {
			return
		}
		location.EndLine = min(location.EndLine, location.StartLine+20)
		excerpt, err := s.read(ctx, ws, location)
		if err != nil {
			warn("部分源码关系因来源变化而排除。")
			return
		}
		seen[key] = true
		limit--
		if relation != "" {
			excerpt += "\n" + relation
		}
		add(Citation{Kind: "code", Title: location.File, File: location.File, StartLine: location.StartLine, EndLine: location.EndLine, SourceHash: location.SourceHash, Symbol: location.Symbol, Excerpt: excerpt, ProjectID: ws.ProjectID})
	}
	addLocation(trace.Matches[0], "")
	for _, relation := range trace.Relations {
		if limit == 0 {
			break
		}
		var targets []string
		if relation.Target != nil {
			targets = append(targets, fmt.Sprintf("%s:%d", relation.Target.File, relation.Target.StartLine))
		}
		for _, candidate := range relation.Candidates {
			targets = append(targets, fmt.Sprintf("%s:%d", candidate.File, candidate.StartLine))
		}
		addLocation(relation.Source, fmt.Sprintf("[CodeLens %s · %s] %s %s", relation.Kind, relation.Status, strings.Join(targets, ", "), relation.Reason))
		if relation.Target != nil {
			addLocation(*relation.Target, "")
		}
	}
	for _, location := range trace.Matches[1:] {
		addLocation(location, "")
	}
	for _, warning := range trace.Warnings {
		warn(warning)
	}
}
