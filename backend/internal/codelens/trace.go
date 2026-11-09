package codelens

import (
	"context"
	"errors"
	"net/http"
)

var ErrTraceIndexRequired = errors.New("请先更新项目索引")

type TraceLocation struct {
	Symbol     string `json:"symbol"`
	File       string `json:"file"`
	StartLine  int    `json:"start_line"`
	EndLine    int    `json:"end_line"`
	SourceHash string `json:"source_hash"`
}

type TraceRelation struct {
	Kind       string          `json:"kind"`
	Source     TraceLocation   `json:"source"`
	Target     *TraceLocation  `json:"target,omitempty"`
	Candidates []TraceLocation `json:"candidates,omitempty"`
	Status     string          `json:"status"`
	Reason     string          `json:"reason,omitempty"`
}

type TraceResult struct {
	Revision  string          `json:"revision"`
	Matches   []TraceLocation `json:"matches"`
	Relations []TraceRelation `json:"relations"`
	Warnings  []string        `json:"warnings"`
}

func (a *Adapter) Trace(ctx context.Context, root, query, revision string) (TraceResult, error) {
	var result TraceResult
	_, err := a.workerJSON(ctx, http.MethodPost, "trace", struct {
		Root     string `json:"project_path"`
		Query    string `json:"query"`
		Revision string `json:"revision"`
	}{root, query, revision}, &result)
	if err == nil && result.Revision != revision {
		return TraceResult{}, errors.New("源码追踪结果版本不匹配")
	}
	return result, err
}
