// Package livecanary routes only explicitly submitted workspace canary requests.
// Ordinary chat, Agent sessions and Gateway defaults are never intercepted.
package livecanary

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/noosphere/backend/internal/gateway"
	"github.com/noosphere/backend/internal/gateway/providers"
	"github.com/noosphere/backend/internal/modellab"
	"github.com/noosphere/backend/internal/modelstore"
	"github.com/noosphere/backend/internal/workspaces"
)

type Service struct {
	store    *modelstore.Store
	lab      *modellab.Service
	gateway  *gateway.Gateway
	now      func() time.Time
	mu       sync.Mutex
	closed   bool
	calls    map[string]activeCall
	work     sync.WaitGroup
	httpWork sync.WaitGroup
}
type activeCall struct {
	release string
	cancel  context.CancelFunc
}
type CreateSpec struct {
	Title       string                `json:"title"`
	StableID    string                `json:"stable_revision_id"`
	CandidateID string                `json:"candidate_revision_id"`
	ProfileID   string                `json:"profile_id"`
	TargetBPS   int                   `json:"target_bps"`
	Policy      modelstore.LivePolicy `json:"policy"`
}
type ArmStats struct {
	Requests        int      `json:"requests"`
	Successful      int      `json:"successful"`
	Errors          int      `json:"errors"`
	Pending         int      `json:"pending"`
	Feedback        int      `json:"feedback"`
	BadFeedback     int      `json:"bad_feedback"`
	LatencySamples  int      `json:"latency_samples"`
	MeanLatencyMS   *float64 `json:"mean_latency_ms"`
	ErrorRate       *float64 `json:"error_rate"`
	BadFeedbackRate *float64 `json:"bad_feedback_rate"`
}
type Detail struct {
	Release      modelstore.LiveRelease  `json:"release"`
	Arms         map[string]ArmStats     `json:"arms"`
	Decision     modelstore.LiveDecision `json:"decision"`
	EffectiveBPS int                     `json:"effective_bps"`
	Running      int                     `json:"running"`
	Limitations  []string                `json:"limitations"`
}

func New(ctx context.Context, store *modelstore.Store, lab *modellab.Service, g *gateway.Gateway) (*Service, error) {
	if store == nil || lab == nil || g == nil {
		return nil, modelstore.ErrInvalid
	}
	s := &Service{store: store, lab: lab, gateway: g, now: time.Now, calls: map[string]activeCall{}}
	if err := store.RecoverLive(ctx, time.Now()); err != nil {
		return nil, err
	}
	return s, nil
}
func DefaultPolicy() modelstore.LivePolicy {
	return modelstore.LivePolicy{WindowSeconds: 60, MinimumSamples: 5, MinimumFeedback: 2, MaximumErrorRate: .1, MaximumBadFeedbackRate: .2, MaximumLatencyRatio: 1.5, ConsecutiveErrors: 2}
}
func validPolicy(p modelstore.LivePolicy) bool {
	return p.WindowSeconds >= 60 && p.WindowSeconds <= 86400 && p.MinimumSamples >= 2 && p.MinimumSamples <= 500 && p.MinimumFeedback >= 1 && p.MinimumFeedback <= p.MinimumSamples && p.MaximumErrorRate >= 0 && p.MaximumErrorRate <= .5 && !math.IsNaN(p.MaximumErrorRate) && p.MaximumBadFeedbackRate >= 0 && p.MaximumBadFeedbackRate <= .5 && !math.IsNaN(p.MaximumBadFeedbackRate) && p.MaximumLatencyRatio >= 1 && p.MaximumLatencyRatio <= 10 && !math.IsNaN(p.MaximumLatencyRatio) && p.ConsecutiveErrors >= 1 && p.ConsecutiveErrors <= 10
}
func livePercent(v int) bool { return v == 500 || v == 2500 || v == 5000 }
func (s *Service) verify(ctx context.Context, m modelstore.ModelRevision) error {
	if m.IdentityLevel != "digest" || m.MetadataHash == "" || m.EndpointFingerprint == "" || m.EngineVersion == "" {
		return modelstore.ErrInsufficientEvidence
	}
	fingerprint, err := s.gateway.EvaluationSourceFingerprint(m.GatewayModelID)
	if err != nil {
		return err
	}
	if fingerprint != m.EndpointFingerprint {
		return modelstore.ErrModelDrift
	}
	discovery, err := s.gateway.InspectEvaluationModels(ctx, m.GatewayModelID)
	if err != nil {
		return err
	}
	for _, v := range discovery.Models {
		if v.Name == m.ModelName && v.LocalVerified && v.Digest == m.Digest && v.MetadataHash == m.MetadataHash && v.EngineVersion == m.EngineVersion {
			return nil
		}
	}
	return modelstore.ErrModelDrift
}
func (s *Service) Create(ctx context.Context, ws, key string, spec CreateSpec) (modelstore.LiveRelease, error) {
	var out modelstore.LiveRelease
	spec.Title = strings.TrimSpace(spec.Title)
	if strings.TrimSpace(spec.Title) == "" || len(spec.Title) > 200 || !validPolicy(spec.Policy) || !livePercent(spec.TargetBPS) || spec.StableID == spec.CandidateID {
		return out, modelstore.ErrInvalid
	}
	stable, err := s.lab.GetModel(ctx, spec.StableID)
	if err != nil {
		return out, err
	}
	candidate, err := s.lab.GetModel(ctx, spec.CandidateID)
	if err != nil {
		return out, err
	}
	if stable.Digest == candidate.Digest {
		return out, fmt.Errorf("%w: live canary requires distinct weights", modelstore.ErrInvalid)
	}
	profile, err := s.lab.GetProfile(ctx, spec.ProfileID)
	if err != nil {
		return out, err
	}
	if profile.Requested.NumCtx == nil || profile.Requested.MaxOutput == nil || *profile.Requested.NumCtx < 1 || *profile.Requested.NumCtx > 8192 || *profile.Requested.MaxOutput < 1 || *profile.Requested.MaxOutput > 2048 {
		return out, modelstore.ErrInvalid
	}
	// No inference is sent during creation. Start/resume and each dispatch recheck identity.
	for _, m := range []modelstore.ModelRevision{stable, candidate} {
		if m.IdentityLevel != "digest" || m.MetadataHash == "" || m.EndpointFingerprint == "" {
			return out, modelstore.ErrInsufficientEvidence
		}
	}
	stamp := s.now().UTC().Format(time.RFC3339Nano)
	out = modelstore.LiveRelease{ID: "live_" + modelstore.LiveHash([]string{ws, key})[:32], WorkspaceID: ws, Mode: "live", Title: strings.TrimSpace(spec.Title), Stable: stable, Candidate: candidate, Profile: profile, Policy: spec.Policy, State: "DRAFT", TargetBPS: spec.TargetBPS, Revision: 1, RouteEpoch: 1, CreatedAt: stamp, UpdatedAt: stamp}
	out.ConfigurationHash = modelstore.LiveHash(struct {
		Spec              CreateSpec
		Stable, Candidate modelstore.ModelRevision
		Profile           modelstore.InferenceProfile
	}{spec, stable, candidate, profile})
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return out, workspaces.ErrClosed
	}
	return s.store.CreateLive(ctx, out, key)
}
func effective(r modelstore.LiveRelease) int {
	if r.State == "ACTIVE" || r.State == "PROMOTED" {
		return r.TargetBPS
	}
	return 0
}
func stats(snap modelstore.LiveSnapshot) map[string]ArmStats {
	out := map[string]ArmStats{"stable": {}, "candidate": {}}
	sums := map[string]int64{}
	times := map[string]int{}
	for _, o := range snap.Observations {
		a := out[o.Arm]
		a.Requests++
		if o.Status == "running" {
			a.Pending++
		} else if o.Status == "succeeded" {
			a.Successful++
			if o.WallNS > 0 {
				sums[o.Arm] += o.WallNS
				times[o.Arm]++
			}
		} else {
			a.Errors++
		}
		if o.Status == "succeeded" && o.Rating != "" {
			a.Feedback++
			if o.Rating == "bad" {
				a.BadFeedback++
			}
		}
		out[o.Arm] = a
	}
	for key, a := range out {
		if a.Successful+a.Errors > 0 {
			a.ErrorRate = modelstore.Float(float64(a.Errors) / float64(a.Successful+a.Errors))
		}
		if a.Feedback > 0 {
			a.BadFeedbackRate = modelstore.Float(float64(a.BadFeedback) / float64(a.Feedback))
		}
		if times[key] > 0 {
			a.LatencySamples = times[key]
			a.MeanLatencyMS = modelstore.Float(float64(sums[key]) / float64(times[key]) / 1e6)
		}
		out[key] = a
	}
	return out
}
func decision(snap modelstore.LiveSnapshot, now time.Time) modelstore.LiveDecision {
	p := snap.Release.Policy
	arms := stats(snap)
	d := modelstore.LiveDecision{Allowed: true, Rules: []modelstore.LiveRule{}, EvidenceHash: modelstore.LiveHash(snap.Observations), ObservedAt: now.UTC().Format(time.RFC3339Nano), RouteEpoch: snap.Release.RouteEpoch, Observations: append([]modelstore.LiveObservation{}, snap.Observations...)}
	add := func(id string, value *float64, required float64, samples int, pass bool, reason string) {
		status := "pass"
		if value == nil {
			status = "insufficient"
		} else if !pass {
			status = "fail"
		}
		if status != "pass" {
			d.Allowed = false
		}
		d.Rules = append(d.Rules, modelstore.LiveRule{ID: id, Status: status, Reason: reason, Observed: value, Required: required, Samples: samples})
	}
	start, err := time.Parse(time.RFC3339Nano, snap.Release.WindowStartedAt)
	var elapsed *float64
	if err == nil && !now.Before(start) {
		elapsed = modelstore.Float(now.Sub(start).Seconds())
	}
	add("wall_clock_window", elapsed, float64(p.WindowSeconds), len(snap.Observations), elapsed != nil && *elapsed >= float64(p.WindowSeconds), "当前分流阶段必须达到真实墙钟观测时长；虚拟时间与模拟统计不计入。")
	for _, key := range []string{"stable", "candidate"} {
		a := arms[key]
		n := a.Successful + a.Errors
		add(key+"_samples", modelstore.Float(float64(n)), float64(p.MinimumSamples), n, n >= p.MinimumSamples, "每一侧都需要足量实际完成请求。")
		v := a.ErrorRate
		if n < p.MinimumSamples {
			v = nil
		}
		add(key+"_error_rate", v, p.MaximumErrorRate, n, v != nil && *v <= p.MaximumErrorRate, "错误、中断计入可用性；不会当作人工质量评分。")
		add(key+"_feedback_count", modelstore.Float(float64(a.Feedback)), float64(p.MinimumFeedback), a.Feedback, a.Feedback >= p.MinimumFeedback, "质量反馈必须绑定同一成功请求；无反馈保持未知。")
		q := a.BadFeedbackRate
		if a.Feedback < p.MinimumFeedback {
			q = nil
		}
		add(key+"_bad_feedback_rate", q, p.MaximumBadFeedbackRate, a.Feedback, q != nil && *q <= p.MaximumBadFeedbackRate, "人工反馈不等于客观通用能力评分。")
	}
	var ratio *float64
	a, b := arms["stable"], arms["candidate"]
	if a.LatencySamples >= p.MinimumSamples && b.LatencySamples >= p.MinimumSamples && a.MeanLatencyMS != nil && b.MeanLatencyMS != nil && *a.MeanLatencyMS > 0 {
		ratio = modelstore.Float(*b.MeanLatencyMS / *a.MeanLatencyMS)
	}
	add("candidate_latency_ratio", ratio, p.MaximumLatencyRatio, b.LatencySamples, ratio != nil && *ratio <= p.MaximumLatencyRatio, "比较当前阶段成功请求的端到端平均时延；不是同提示配对性能基准。")
	add("no_inflight_requests", modelstore.Float(float64(snap.Running)), 0, snap.Running, snap.Running == 0, "阶段推进须等待已准入请求结束。")
	return d
}
func autoPause(snap modelstore.LiveSnapshot) string {
	if snap.Release.State != "ACTIVE" && snap.Release.State != "PROMOTED" {
		return ""
	}
	p := snap.Release.Policy
	arms := stats(snap)
	for _, o := range snap.Observations {
		if o.ErrorCode == "MODEL_DRIFT" {
			return o.Arm + "_identity_drift"
		}
	}
	consecutive := 0
	for i := len(snap.Observations) - 1; i >= 0; i-- {
		o := snap.Observations[i]
		if o.Arm != "candidate" || o.Status == "running" {
			continue
		}
		if o.Status == "succeeded" {
			break
		}
		consecutive++
	}
	if consecutive >= p.ConsecutiveErrors {
		return "candidate_consecutive_errors"
	}
	candidate := arms["candidate"]
	if candidate.Successful+candidate.Errors >= p.MinimumSamples && candidate.ErrorRate != nil && *candidate.ErrorRate > p.MaximumErrorRate {
		return "candidate_error_rate"
	}
	if candidate.Feedback >= p.MinimumFeedback && candidate.BadFeedbackRate != nil && *candidate.BadFeedbackRate > p.MaximumBadFeedbackRate {
		return "candidate_quality_feedback"
	}
	stable := arms["stable"]
	if candidate.LatencySamples >= p.MinimumSamples && stable.LatencySamples >= p.MinimumSamples && candidate.MeanLatencyMS != nil && stable.MeanLatencyMS != nil && *stable.MeanLatencyMS > 0 && *candidate.MeanLatencyMS / *stable.MeanLatencyMS > p.MaximumLatencyRatio {
		return "candidate_latency_regression"
	}
	return ""
}
func (s *Service) Get(ctx context.Context, ws, id string) (Detail, error) {
	snap, err := s.store.LiveSnapshot(ctx, ws, id)
	if err != nil {
		return Detail{}, err
	}
	return Detail{Release: snap.Release, Arms: stats(snap), Decision: decision(snap, s.now()), EffectiveBPS: effective(snap.Release), Running: snap.Running, Limitations: []string{"仅此工作区明确选用的真实灰度入口参与路由，普通聊天与 Agent 保留既有模型绑定。", "两个模型处理不同的真实输入；时延门禁是运行保护，不是同任务性能排名。", "质量来自本机操作者对成功请求的反馈，未反馈与缺失指标不会补零。", "单个实际请求最多等待 120 秒，包含准入、身份检查和推理。", "记录本地保存请求与输出；每个工作区最多 32 个发布，每个发布最多 1000 个请求。"}}, nil
}
func (s *Service) Command(ctx context.Context, ws, id, command, key string, revision int64) (modelstore.LiveRelease, error) {
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	intentHash := modelstore.LiveHash([]any{command, revision})
	if prior, found, err := s.store.LiveIntent(ctx, ws, id, "command", key, intentHash); found || err != nil {
		return prior.Release, err
	}
	// Identity I/O happens outside the service admission lock; CAS below prevents a stale enable.
	if command == "start" || command == "resume" || command == "advance" {
		snap, err := s.store.LiveSnapshot(ctx, ws, id)
		if err != nil {
			return modelstore.LiveRelease{}, err
		}
		for _, m := range []modelstore.ModelRevision{snap.Release.Stable, snap.Release.Candidate} {
			if err = s.verify(ctx, m); err != nil {
				return modelstore.LiveRelease{}, err
			}
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return modelstore.LiveRelease{}, workspaces.ErrClosed
	}
	m, fresh, err := s.store.MutateLive(ctx, ws, id, "command", key, intentHash, func(snap modelstore.LiveSnapshot) (modelstore.LiveMutation, error) {
		r := snap.Release
		out := modelstore.LiveMutation{Release: r, Command: command, Reason: "explicit_user_" + command, Decision: decision(snap, s.now())}
		if r.Revision != revision {
			return out, modelstore.ErrRevisionConflict
		}
		if r.EventSequence >= modelstore.LiveEventLimit && command != "pause" && command != "rollback" {
			return out, modelstore.ErrInvalid
		}
		switch command {
		case "start":
			if r.State != "DRAFT" {
				return out, modelstore.ErrInvalid
			}
			out.Release.State = "ACTIVE"
			out.Release.WindowStartedAt = s.now().UTC().Format(time.RFC3339Nano)
		case "resume":
			if r.State != "PAUSED" || snap.Running > 0 {
				return out, modelstore.ErrInvalid
			}
			out.Release.State = "ACTIVE"
			if r.TargetBPS == 10000 {
				out.Release.State = "PROMOTED"
			}
			out.Release.RouteEpoch++
			out.Release.WindowStartedAt = s.now().UTC().Format(time.RFC3339Nano)
		case "pause":
			if r.State != "ACTIVE" && r.State != "PROMOTED" {
				return out, modelstore.ErrInvalid
			}
			out.Release.State = "PAUSED"
		case "rollback":
			if r.State == "DRAFT" || r.State == "ROLLED_BACK" {
				return out, modelstore.ErrInvalid
			}
			out.Release.State = "ROLLED_BACK"
			out.Release.TargetBPS = 0
			out.Release.RouteEpoch++
			out.Release.WindowStartedAt = s.now().UTC().Format(time.RFC3339Nano)
		case "advance":
			if r.State != "ACTIVE" {
				return out, modelstore.ErrInvalid
			}
			if !out.Decision.Allowed {
				return out, modelstore.ErrInsufficientEvidence
			}
			switch r.TargetBPS {
			case 500:
				out.Release.TargetBPS = 2500
			case 2500:
				out.Release.TargetBPS = 5000
			case 5000:
				out.Release.TargetBPS = 10000
				out.Release.State = "PROMOTED"
			default:
				return out, modelstore.ErrInvalid
			}
			out.Release.RouteEpoch++
			out.Release.WindowStartedAt = s.now().UTC().Format(time.RFC3339Nano)
		default:
			return out, modelstore.ErrInvalid
		}
		out.Release.Revision++
		out.Release.UpdatedAt = s.now().UTC().Format(time.RFC3339Nano)
		return out, nil
	})
	if err == nil && fresh && (command == "pause" || command == "rollback") {
		s.cancelRelease(id)
	}
	return m.Release, err
}
func (s *Service) cancelRelease(id string) {
	for _, call := range s.calls {
		if call.release == id {
			call.cancel()
		}
	}
}
func observation(r modelstore.LiveRequest) modelstore.LiveObservation {
	r.Hash = ""
	return modelstore.LiveObservation{ID: r.ID, Arm: r.Arm, Status: r.Status, WallNS: r.WallNS, Hash: modelstore.LiveHash(r), Sequence: r.Sequence, ErrorCode: r.ErrorCode}
}
func boundedOutput(value string) (string, bool) {
	if len(value) <= 65536 {
		return value, false
	}
	raw := []byte(value)[:65536]
	for !utf8.Valid(raw) {
		raw = raw[:len(raw)-1]
	}
	return string(raw), true
}
func (s *Service) Request(parent context.Context, ws, id, key, prompt string) (modelstore.LiveRequest, error) {
	if key == "" || strings.TrimSpace(prompt) == "" || len(prompt) > 16384 || !utf8.ValidString(prompt) {
		return modelstore.LiveRequest{}, modelstore.ErrInvalid
	}
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return modelstore.LiveRequest{}, workspaces.ErrClosed
	}
	ctx, cancel := context.WithTimeout(parent, 2*time.Minute)
	m, fresh, err := s.store.MutateLive(ctx, ws, id, "request", key, modelstore.LiveHash(prompt), func(snap modelstore.LiveSnapshot) (modelstore.LiveMutation, error) {
		r := snap.Release
		out := modelstore.LiveMutation{Release: r}
		if r.State == "DRAFT" {
			return out, modelstore.ErrInvalid
		}
		if snap.Running > 0 {
			return out, workspaces.ErrBusy
		}
		if r.NextSequence >= modelstore.LiveRequestLimit {
			return out, modelstore.ErrInvalid
		}
		sequence := r.NextSequence + 1
		arm := "stable"
		bps := effective(r)
		if bps > 0 && (sequence*int64(bps))%10000 < int64(bps) {
			arm = "candidate"
		}
		model := r.Stable
		if arm == "candidate" {
			model = r.Candidate
		}
		req := modelstore.LiveRequest{ID: "live_req_" + modelstore.LiveHash([]string{ws, id, key})[:32], ReleaseID: id, WorkspaceID: ws, Source: "live_provider", Sequence: sequence, RouteEpoch: r.RouteEpoch, Arm: arm, ModelRevisionID: model.ID, Status: "running", Prompt: prompt, StartedAt: s.now().UTC().Format(time.RFC3339Nano), InputHash: modelstore.LiveHash(prompt)}
		out.Release.NextSequence = sequence
		out.Request = &req
		return out, nil
	})
	if err != nil || !fresh {
		s.mu.Unlock()
		cancel()
		if m.Request != nil {
			return *m.Request, err
		}
		return modelstore.LiveRequest{}, err
	}
	request := *m.Request
	s.calls[request.ID] = activeCall{release: id, cancel: cancel}
	s.work.Add(1)
	s.mu.Unlock()
	defer func() { cancel(); s.mu.Lock(); delete(s.calls, request.ID); s.mu.Unlock(); s.work.Done() }()
	start := time.Now()
	// "evaluation" selects the provider's verified single-call protocol; this
	// request is stored only as live_provider evidence, never as an offline run.
	response, callErr := s.lab.Evaluate(ctx, modellab.InferenceSpec{ModelRevisionID: request.ModelRevisionID, ProfileID: m.Release.Profile.ID, AttemptID: request.ID, Kind: "evaluation", Messages: []providers.Message{{Role: "user", Content: prompt}}})
	request.WallNS = time.Since(start).Nanoseconds()
	request.FinishedAt = s.now().UTC().Format(time.RFC3339Nano)
	request.Status = "succeeded"
	if callErr != nil {
		request.Status = "failed"
		request.ErrorCode = "PROVIDER_UNAVAILABLE"
		var p *providers.EvaluationError
		if errors.As(callErr, &p) {
			request.ErrorCode = p.Code
		}
		if errors.Is(callErr, context.Canceled) || errors.Is(ctx.Err(), context.Canceled) {
			request.Status = "interrupted"
			request.ErrorCode = "REQUEST_CANCELLED"
		}
	}
	if response != nil {
		request.Output, request.OutputTruncated = boundedOutput(response.Output)
		request.OutputTruncated = request.OutputTruncated || response.Truncated
		request.OutputHash = modelstore.LiveHash(response.Output)
		request.IdentityBeforeHash = modelstore.LiveHash(response.IdentityBefore)
		request.IdentityAfterHash = modelstore.LiveHash(response.IdentityAfter)
		request.Tokens = response.EvalCount
		if callErr == nil && (strings.TrimSpace(response.Output) == "" || response.Truncated) {
			request.Status = "failed"
			request.ErrorCode = "INVALID_OUTPUT"
		}
	}
	if response == nil && callErr == nil {
		request.Status = "failed"
		request.ErrorCode = "EMPTY_PROVIDER_RESPONSE"
	}
	// Completion is durable even after HTTP cancellation or a concurrent rollback.
	finishCtx, finishCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer finishCancel()
	s.mu.Lock()
	defer s.mu.Unlock()
	final, _, err := s.store.MutateLive(finishCtx, ws, id, "finish", "", "", func(snap modelstore.LiveSnapshot) (modelstore.LiveMutation, error) {
		out := modelstore.LiveMutation{Release: snap.Release, Request: &request}
		if request.RouteEpoch != snap.Release.RouteEpoch {
			return out, nil
		}
		for i, o := range snap.Observations {
			if o.ID == request.ID {
				snap.Observations[i] = observation(request)
			}
		}
		snap.Running--
		if reason := autoPause(snap); reason != "" {
			out.Release.State = "PAUSED"
			out.Release.Revision++
			out.Release.UpdatedAt = s.now().UTC().Format(time.RFC3339Nano)
			out.Command = "automatic_pause"
			out.Reason = reason
			out.Decision = decision(snap, s.now())
		}
		return out, nil
	})
	if err != nil {
		return request, err
	}
	return *final.Request, nil
}
func (s *Service) Feedback(ctx context.Context, ws, id, requestID, rating, note, key string) (modelstore.LiveRelease, error) {
	if key == "" || (rating != "good" && rating != "bad") || len(note) > 2000 {
		return modelstore.LiveRelease{}, modelstore.ErrInvalid
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return modelstore.LiveRelease{}, workspaces.ErrClosed
	}
	m, fresh, err := s.store.MutateLive(ctx, ws, id, "feedback", key, modelstore.LiveHash([]string{requestID, rating, note}), func(snap modelstore.LiveSnapshot) (modelstore.LiveMutation, error) {
		f := modelstore.LiveFeedback{RequestID: requestID, ReleaseID: id, Rating: rating, Note: note, CreatedAt: s.now().UTC().Format(time.RFC3339Nano)}
		out := modelstore.LiveMutation{Release: snap.Release, Feedback: &f}
		for i, o := range snap.Observations {
			if o.ID == requestID {
				if o.Status != "succeeded" || o.Rating != "" {
					return out, modelstore.ErrInvalid
				}
				snap.Observations[i].Rating = rating
				snap.Observations[i].FeedbackHash = modelstore.LiveHash(f)
			}
		}
		if reason := autoPause(snap); reason != "" {
			out.Release.State = "PAUSED"
			out.Release.Revision++
			out.Release.UpdatedAt = s.now().UTC().Format(time.RFC3339Nano)
			out.Command = "automatic_pause"
			out.Reason = reason
			out.Decision = decision(snap, s.now())
		}
		return out, nil
	})
	if err == nil && fresh && m.Command == "automatic_pause" {
		s.cancelRelease(id)
	}
	return m.Release, err
}
func (s *Service) Close() error {
	s.mu.Lock()
	s.closed = true
	for _, call := range s.calls {
		call.cancel()
	}
	s.mu.Unlock()
	s.httpWork.Wait()
	s.work.Wait()
	return s.store.RecoverLive(context.Background(), s.now())
}
