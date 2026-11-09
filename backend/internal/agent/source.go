package agent

import (
	"context"
	"crypto/sha256"
	"fmt"
	"strings"

	"github.com/noosphere/backend/internal/codelens"
	"github.com/noosphere/backend/internal/workspaces"
)

type SourceView struct {
	File           string `json:"file"`
	StartLine      int    `json:"start_line"`
	EndLine        int    `json:"end_line"`
	SourceHash     string `json:"source_hash"`
	MatchesVersion bool   `json:"matches_version"`
	Excerpt        string `json:"excerpt,omitempty"`
}

func ReadProjectSource(ctx context.Context, ws workspaces.Workspace, file string, start, end int, expected string) (SourceView, error) {
	if start < 1 || end < start || end-start >= 200 {
		return SourceView{}, fmt.Errorf("请选择最多 200 行代码")
	}
	registry := Registry{workspace: ws}
	raw, err := registry.read(ctx, file)
	if err != nil {
		return SourceView{}, err
	}
	version := fmt.Sprintf("%x", sha256.Sum256(raw))
	if expected != "" && expected != version {
		return SourceView{File: file, StartLine: start, EndLine: end, SourceHash: version}, nil
	}
	evidence, err := fileEvidence(file, raw, start, end)
	if err != nil {
		return SourceView{}, err
	}
	return SourceView{File: file, StartLine: start, EndLine: evidence.EndLine, SourceHash: version, MatchesVersion: true, Excerpt: evidence.Excerpt}, nil
}

func ScopeProjectTrace(ctx context.Context, ws workspaces.Workspace, trace codelens.TraceResult) (codelens.TraceResult, error) {
	registry := Registry{workspace: ws}
	versions := map[string]string{}
	omitted := false
	valid := func(location codelens.TraceLocation) bool {
		version, checked := versions[location.File]
		if !checked {
			raw, err := registry.read(ctx, location.File)
			if err == nil {
				version = fmt.Sprintf("%x", sha256.Sum256(raw))
			}
			versions[location.File] = version
		}
		keep := version != "" && version == location.SourceHash
		omitted = omitted || !keep
		return keep
	}
	result := codelens.TraceResult{Revision: trace.Revision, Warnings: trace.Warnings, Matches: []codelens.TraceLocation{}, Relations: []codelens.TraceRelation{}}
	for _, item := range trace.Matches {
		if valid(item) {
			result.Matches = append(result.Matches, item)
		}
	}
	for _, relation := range trace.Relations {
		if !valid(relation.Source) {
			continue
		}
		validTargets := relation.Target == nil || valid(*relation.Target)
		for _, candidate := range relation.Candidates {
			validTargets = valid(candidate) && validTargets
		}
		if !validTargets {
			relation.Target = nil
			relation.Candidates = nil
			relation.Status = "unresolved"
			relation.Reason = "目标文件版本已变化或超出可读取的项目范围。"
		}
		result.Relations = append(result.Relations, relation)
	}
	if omitted {
		result.Warnings = append(result.Warnings, "部分关系因文件版本变化或超出可读取的项目范围已排除。")
	}
	return result, ctx.Err()
}

func (r *Registry) traceSource(ctx context.Context, query string) Outcome {
	trace, err := r.trace(ctx, r.workspace, query)
	if err != nil {
		return fail(err)
	}
	out := Outcome{Success: true}
	var summary []string
	seen := map[string]bool{}
	add := func(location codelens.TraceLocation) {
		key := fmt.Sprintf("%s:%d", location.File, location.StartLine)
		if seen[key] || len(out.Evidence) >= 10 {
			return
		}
		seen[key] = true
		raw, readErr := r.read(ctx, location.File)
		if readErr != nil || fmt.Sprintf("%x", sha256.Sum256(raw)) != location.SourceHash {
			return
		}
		evidence, readErr := fileEvidence(location.File, raw, location.StartLine, min(location.EndLine, location.StartLine+12))
		if readErr != nil {
			return
		}
		evidence.Title = location.Symbol
		out.Evidence = append(out.Evidence, evidence)
	}
	for _, relation := range trace.Relations[:min(10, len(trace.Relations))] {
		add(relation.Source)
		targets := relation.Candidates
		if relation.Target != nil {
			targets = []codelens.TraceLocation{*relation.Target}
		}
		destinations := []string{}
		for _, target := range targets {
			add(target)
			destinations = append(destinations, fmt.Sprintf("%s (%s:%d)", target.Symbol, target.File, target.StartLine))
		}
		summary = append(summary, fmt.Sprintf("%s [%s] %s (%s:%d) → %s %s", relation.Kind, relation.Status, relation.Source.Symbol, relation.Source.File, relation.Source.StartLine, strings.Join(destinations, ", "), relation.Reason))
	}
	for _, match := range trace.Matches {
		add(match)
	}
	if len(trace.Relations) > 10 {
		summary = append(summary, "Additional relations are available in the CodeLens project panel.")
	}
	if len(trace.Matches) == 0 {
		summary = append(summary, "No matching indexed locations for this query.")
	}
	out.Output = clip(redact(strings.Join(append(summary, trace.Warnings...), "\n")), 7000)
	return out
}
