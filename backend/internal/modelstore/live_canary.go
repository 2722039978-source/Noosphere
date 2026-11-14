package modelstore

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strconv"
	"time"
)

const LiveRequestLimit = 1000
const LiveReleaseLimit = 32
const LiveEventLimit = 128

type LivePolicy struct {
	WindowSeconds          int64   `json:"window_seconds"`
	MinimumSamples         int     `json:"minimum_samples_per_arm"`
	MinimumFeedback        int     `json:"minimum_feedback_per_arm"`
	MaximumErrorRate       float64 `json:"maximum_error_rate"`
	MaximumBadFeedbackRate float64 `json:"maximum_bad_feedback_rate"`
	MaximumLatencyRatio    float64 `json:"maximum_latency_ratio"`
	ConsecutiveErrors      int     `json:"auto_pause_consecutive_errors"`
}
type LiveRelease struct {
	ID                string           `json:"id"`
	WorkspaceID       string           `json:"workspace_id"`
	Mode              string           `json:"mode"`
	Title             string           `json:"title"`
	Stable            ModelRevision    `json:"stable"`
	Candidate         ModelRevision    `json:"candidate"`
	Profile           InferenceProfile `json:"profile"`
	Policy            LivePolicy       `json:"policy"`
	ConfigurationHash string           `json:"configuration_hash"`
	State             string           `json:"state"`
	TargetBPS         int              `json:"target_bps"`
	Revision          int64            `json:"revision"`
	RouteEpoch        int64            `json:"route_epoch"`
	NextSequence      int64            `json:"next_sequence"`
	WindowStartedAt   string           `json:"window_started_at"`
	CreatedAt         string           `json:"created_at"`
	UpdatedAt         string           `json:"updated_at"`
	EventSequence     int64            `json:"event_sequence"`
	LastEventHash     string           `json:"last_event_hash"`
}
type LiveRequest struct {
	// Read-time feedback projection; not part of the immutable request body hash.
	FeedbackRating     string        `json:"feedback_rating,omitempty"`
	Feedback           *LiveFeedback `json:"feedback,omitempty"`
	ID                 string        `json:"id"`
	ReleaseID          string        `json:"release_id"`
	WorkspaceID        string        `json:"workspace_id"`
	Source             string        `json:"source"`
	Sequence           int64         `json:"sequence"`
	RouteEpoch         int64         `json:"route_epoch"`
	Arm                string        `json:"arm"`
	ModelRevisionID    string        `json:"model_revision_id"`
	Status             string        `json:"status"`
	Prompt             string        `json:"prompt"`
	Output             string        `json:"output"`
	OutputTruncated    bool          `json:"output_truncated"`
	ErrorCode          string        `json:"error_code,omitempty"`
	WallNS             int64         `json:"wall_ns"`
	StartedAt          string        `json:"started_at"`
	FinishedAt         string        `json:"finished_at,omitempty"`
	InputHash          string        `json:"input_hash"`
	OutputHash         string        `json:"output_hash,omitempty"`
	IdentityBeforeHash string        `json:"identity_before_hash,omitempty"`
	IdentityAfterHash  string        `json:"identity_after_hash,omitempty"`
	Tokens             *int64        `json:"tokens,omitempty"`
	Hash               string        `json:"hash"`
}
type LiveFeedback struct {
	RequestID string `json:"request_id"`
	ReleaseID string `json:"release_id"`
	Rating    string `json:"rating"`
	Note      string `json:"note"`
	CreatedAt string `json:"created_at"`
	Hash      string `json:"hash"`
}
type LiveObservation struct {
	ID           string `json:"id"`
	Arm          string `json:"arm"`
	Status       string `json:"status"`
	WallNS       int64  `json:"wall_ns"`
	Hash         string `json:"hash"`
	Sequence     int64  `json:"sequence"`
	ErrorCode    string `json:"error_code,omitempty"`
	Rating       string `json:"rating,omitempty"`
	FeedbackHash string `json:"feedback_hash,omitempty"`
}
type LiveRule struct {
	ID       string   `json:"id"`
	Status   string   `json:"status"`
	Reason   string   `json:"reason"`
	Observed *float64 `json:"observed"`
	Required float64  `json:"required"`
	Samples  int      `json:"samples"`
}
type LiveDecision struct {
	Allowed      bool              `json:"allowed"`
	Rules        []LiveRule        `json:"rules"`
	EvidenceHash string            `json:"evidence_hash"`
	ObservedAt   string            `json:"observed_at"`
	RouteEpoch   int64             `json:"route_epoch"`
	Observations []LiveObservation `json:"observations"`
}
type LiveEvent struct {
	ReleaseID         string       `json:"release_id"`
	Sequence          int64        `json:"sequence"`
	Command           string       `json:"command"`
	Reason            string       `json:"reason"`
	FromState         string       `json:"from_state"`
	ToState           string       `json:"to_state"`
	RouteEpoch        int64        `json:"route_epoch"`
	TargetBPS         int          `json:"target_bps"`
	ConfigurationHash string       `json:"configuration_hash"`
	Decision          LiveDecision `json:"decision"`
	CreatedAt         string       `json:"created_at"`
	PreviousHash      string       `json:"previous_hash"`
	Hash              string       `json:"hash"`
}
type LiveSnapshot struct {
	Release      LiveRelease       `json:"release"`
	Observations []LiveObservation `json:"observations"`
	Running      int               `json:"running"`
}
type LiveMutation struct {
	Release  LiveRelease   `json:"release"`
	Request  *LiveRequest  `json:"request,omitempty"`
	Feedback *LiveFeedback `json:"feedback,omitempty"`
	Command  string        `json:"command,omitempty"`
	Reason   string        `json:"reason,omitempty"`
	Decision LiveDecision  `json:"decision"`
}

func LiveHash(v any) string { raw, _ := json.Marshal(v); return digest(raw) }
func liveBody(v any) (string, string, error) {
	raw, err := json.Marshal(v)
	return string(raw), digest(raw), err
}
func readLiveRelease(ctx context.Context, q queryer, ws, id string) (LiveRelease, error) {
	var out LiveRelease
	var body, hash string
	err := q.QueryRowContext(ctx, "SELECT body_json,body_hash FROM live_canary_releases WHERE workspace_id=? AND id=?", ws, id).Scan(&body, &hash)
	if err == sql.ErrNoRows {
		return out, ErrNotFound
	}
	if err != nil {
		return out, err
	}
	if digest([]byte(body)) != hash {
		return out, ErrInsufficientEvidence
	}
	err = json.Unmarshal([]byte(body), &out)
	return out, err
}
func liveSnapshot(ctx context.Context, q queryer, ws, id string) (LiveSnapshot, error) {
	out := LiveSnapshot{Observations: []LiveObservation{}}
	var err error
	out.Release, err = readLiveRelease(ctx, q, ws, id)
	if err != nil {
		return out, err
	}
	err = q.QueryRowContext(ctx, "SELECT count(*) FROM live_canary_requests WHERE release_id=? AND status='running'", id).Scan(&out.Running)
	if err != nil {
		return out, err
	}
	rows, err := q.QueryContext(ctx, `SELECT r.id,r.arm,r.status,r.wall_ns,r.body_hash,r.sequence,coalesce(json_extract(r.body_json,'$.error_code'),''),coalesce(f.rating,''),coalesce(f.body_hash,'') FROM live_canary_requests r LEFT JOIN live_canary_feedback f ON f.request_id=r.id WHERE r.release_id=? AND r.route_epoch=? ORDER BY r.sequence`, id, out.Release.RouteEpoch)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var o LiveObservation
		if err = rows.Scan(&o.ID, &o.Arm, &o.Status, &o.WallNS, &o.Hash, &o.Sequence, &o.ErrorCode, &o.Rating, &o.FeedbackHash); err != nil {
			return out, err
		}
		out.Observations = append(out.Observations, o)
	}
	return out, rows.Err()
}
func (s *Store) LiveSnapshot(ctx context.Context, ws, id string) (LiveSnapshot, error) {
	// A read transaction keeps release epoch, request and feedback observations coherent.
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return LiveSnapshot{}, err
	}
	defer tx.Rollback()
	return liveSnapshot(ctx, tx, ws, id)
}
func saveLiveRelease(ctx context.Context, tx *sql.Tx, r LiveRelease) error {
	body, hash, err := liveBody(r)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, "UPDATE live_canary_releases SET body_json=?,body_hash=? WHERE id=? AND workspace_id=?", body, hash, r.ID, r.WorkspaceID)
	return err
}
func appendLiveEvent(ctx context.Context, tx *sql.Tx, old LiveRelease, m *LiveMutation) error {
	if m.Command == "" {
		return nil
	}
	r := &m.Release
	event := LiveEvent{ReleaseID: r.ID, Sequence: old.EventSequence + 1, Command: m.Command, Reason: m.Reason, FromState: old.State, ToState: r.State, RouteEpoch: r.RouteEpoch, TargetBPS: r.TargetBPS, ConfigurationHash: r.ConfigurationHash, Decision: m.Decision, CreatedAt: r.UpdatedAt, PreviousHash: old.LastEventHash}
	event.Hash = LiveHash(event)
	body, _, err := liveBody(event)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, "INSERT INTO live_canary_events VALUES(?,?,?,?)", r.ID, event.Sequence, body, event.Hash)
	if err != nil {
		return err
	}
	r.LastEventHash = event.Hash
	r.EventSequence = event.Sequence
	return nil
}
func liveReceipt(ctx context.Context, tx *sql.Tx, ws, id, op, key, hash string) (LiveMutation, bool, error) {
	var out LiveMutation
	var saved, body string
	err := tx.QueryRowContext(ctx, "SELECT request_hash,response_json FROM live_canary_receipts WHERE workspace_id=? AND release_id=? AND operation=? AND key=?", ws, id, op, key).Scan(&saved, &body)
	if err == sql.ErrNoRows {
		return out, false, nil
	}
	if err != nil {
		return out, false, err
	}
	if saved != hash {
		return out, true, ErrIdempotencyConflict
	}
	err = json.Unmarshal([]byte(body), &out)
	return out, true, err
}

// LiveIntent reads a completed intent before external identity checks. A replay
// must not depend on provider availability or cancel a subsequently resumed call.
func (s *Store) LiveIntent(ctx context.Context, ws, id, op, key, hash string) (LiveMutation, bool, error) {
	if !validID(key) {
		return LiveMutation{}, false, ErrInvalid
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return LiveMutation{}, false, err
	}
	defer tx.Rollback()
	if _, err := readLiveRelease(ctx, tx, ws, id); err != nil {
		return LiveMutation{}, false, err
	}
	return liveReceipt(ctx, tx, ws, id, op, key, hash)
}
func putLiveReceipt(ctx context.Context, tx *sql.Tx, ws, id, op, key, hash string, m LiveMutation) error {
	body, _, err := liveBody(m)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, "INSERT INTO live_canary_receipts VALUES(?,?,?,?,?,?)", ws, id, op, key, hash, body)
	return err
}
func (s *Store) CreateLive(ctx context.Context, r LiveRelease, key string) (LiveRelease, error) {
	var out LiveRelease
	if !validID(r.ID) || !validID(r.WorkspaceID) || !validID(key) || r.Mode != "live" || r.State != "DRAFT" || validateModelRevision(r.Stable) != nil || validateModelRevision(r.Candidate) != nil || validateInferenceProfile(r.Profile) != nil {
		return out, ErrInvalid
	}
	requestHash := r.ConfigurationHash
	err := s.write(ctx, func(tx *sql.Tx) error {
		prior, found, err := liveReceipt(ctx, tx, r.WorkspaceID, r.ID, "create", key, requestHash)
		if err != nil {
			return err
		}
		if found {
			out = prior.Release
			return nil
		}
		var n int
		if err = tx.QueryRowContext(ctx, "SELECT count(*) FROM live_canary_releases WHERE workspace_id=?", r.WorkspaceID).Scan(&n); err != nil {
			return err
		}
		if n >= LiveReleaseLimit {
			return fmt.Errorf("%w: live release retention limit", ErrInvalid)
		}
		body, hash, err := liveBody(r)
		if err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, "INSERT INTO live_canary_releases VALUES(?,?,?,?,?)", r.ID, r.WorkspaceID, body, hash, r.CreatedAt); err != nil {
			return err
		}
		m := LiveMutation{Release: r, Command: "create", Reason: "explicit_workspace_live_draft"}
		if err = appendLiveEvent(ctx, tx, LiveRelease{}, &m); err != nil {
			return err
		}
		if err = saveLiveRelease(ctx, tx, m.Release); err != nil {
			return err
		}
		out = m.Release
		return putLiveReceipt(ctx, tx, r.WorkspaceID, r.ID, "create", key, requestHash, m)
	})
	return out, err
}

// MutateLive serializes route admission, gates, feedback and state changes in one transaction.
// A receipt owns the caller's intent. Replaying admission never dispatches another provider call.
func (s *Store) MutateLive(ctx context.Context, ws, id, op, key, requestHash string, fn func(LiveSnapshot) (LiveMutation, error)) (out LiveMutation, fresh bool, err error) {
	if key != "" && !validID(key) {
		return out, false, ErrInvalid
	}
	err = s.write(ctx, func(tx *sql.Tx) error {
		if key != "" {
			prior, found, e := liveReceipt(ctx, tx, ws, id, op, key, requestHash)
			if e != nil {
				return e
			}
			if found {
				out = prior
				if out.Request != nil {
					r, e := readLiveRequest(ctx, tx, ws, id, out.Request.ID)
					if e != nil {
						return e
					}
					out.Request = &r
				}
				return nil
			}
		}
		snap, e := liveSnapshot(ctx, tx, ws, id)
		if e != nil {
			return e
		}
		m, e := fn(snap)
		if e != nil {
			return e
		}
		if m.Release.ID != id || m.Release.WorkspaceID != ws || m.Release.ConfigurationHash != snap.Release.ConfigurationHash {
			return ErrInvalid
		}
		if m.Request != nil {
			r := m.Request
			if r.ReleaseID != id || r.WorkspaceID != ws {
				return ErrInvalid
			}
			r.Hash = ""
			r.FeedbackRating = ""
			r.Feedback = nil
			body, hash, e := liveBody(r)
			if e != nil {
				return e
			}
			r.Hash = hash
			if r.Status == "running" {
				if snap.Running > 0 || snap.Release.NextSequence >= LiveRequestLimit {
					return ErrInvalid
				}
				_, e = tx.ExecContext(ctx, "INSERT INTO live_canary_requests VALUES(?,?,?,?,?,?,?,?,?,?)", r.ID, id, ws, r.Sequence, r.RouteEpoch, r.Arm, r.Status, r.WallNS, body, hash)
			} else {
				result, x := tx.ExecContext(ctx, "UPDATE live_canary_requests SET status=?,wall_ns=?,body_json=?,body_hash=? WHERE id=? AND release_id=? AND workspace_id=? AND status='running'", r.Status, r.WallNS, body, hash, r.ID, id, ws)
				e = x
				if e == nil {
					n, _ := result.RowsAffected()
					if n != 1 {
						return ErrStaleLease
					}
				}
			}
			if e != nil {
				return e
			}
		}
		if m.Feedback != nil {
			f := m.Feedback
			r, e := readLiveRequest(ctx, tx, ws, id, f.RequestID)
			if e != nil {
				return e
			}
			if r.Status != "succeeded" || f.ReleaseID != id || (f.Rating != "good" && f.Rating != "bad") {
				return ErrInvalid
			}
			f.Hash = ""
			body, hash, e := liveBody(f)
			if e != nil {
				return e
			}
			f.Hash = hash
			_, e = tx.ExecContext(ctx, "INSERT INTO live_canary_feedback VALUES(?,?,?,?,?)", f.RequestID, id, f.Rating, body, hash)
			if e != nil {
				return ErrInvalid
			}
		}
		if e = appendLiveEvent(ctx, tx, snap.Release, &m); e != nil {
			return e
		}
		if e = saveLiveRelease(ctx, tx, m.Release); e != nil {
			return e
		}
		if key != "" {
			if e = putLiveReceipt(ctx, tx, ws, id, op, key, requestHash, m); e != nil {
				return e
			}
		}
		out = m
		fresh = true
		return nil
	})
	return
}
func readLiveRequest(ctx context.Context, q queryer, ws, release, id string) (LiveRequest, error) {
	var out LiveRequest
	var body, hash, feedback, feedbackHash string
	err := q.QueryRowContext(ctx, "SELECT r.body_json,r.body_hash,coalesce(f.body_json,''),coalesce(f.body_hash,'') FROM live_canary_requests r LEFT JOIN live_canary_feedback f ON f.request_id=r.id WHERE r.workspace_id=? AND r.release_id=? AND r.id=?", ws, release, id).Scan(&body, &hash, &feedback, &feedbackHash)
	if err == sql.ErrNoRows {
		return out, ErrNotFound
	}
	if err != nil {
		return out, err
	}
	if digest([]byte(body)) != hash {
		return out, ErrInsufficientEvidence
	}
	err = json.Unmarshal([]byte(body), &out)
	out.Hash = hash
	if err == nil {
		err = attachLiveFeedback(&out, feedback, feedbackHash)
	}
	return out, err
}
func attachLiveFeedback(r *LiveRequest, body, hash string) error {
	if body == "" && hash == "" {
		return nil
	}
	if digest([]byte(body)) != hash {
		return ErrInsufficientEvidence
	}
	var f LiveFeedback
	if err := json.Unmarshal([]byte(body), &f); err != nil {
		return err
	}
	if f.RequestID != r.ID || f.ReleaseID != r.ReleaseID || r.Status != "succeeded" {
		return ErrInsufficientEvidence
	}
	f.Hash = hash
	r.FeedbackRating = f.Rating
	r.Feedback = &f
	return nil
}
func (s *Store) GetLiveRequest(ctx context.Context, ws, release, id string) (LiveRequest, error) {
	return readLiveRequest(ctx, s.db, ws, release, id)
}
func (s *Store) ListLive(ctx context.Context, ws, cursor string, limit int) ([]LiveRelease, string, error) {
	out := []LiveRelease{}
	rows, err := s.db.QueryContext(ctx, "SELECT body_json,body_hash FROM live_canary_releases WHERE workspace_id=? AND (?='' OR id<?) ORDER BY id DESC LIMIT ?", ws, cursor, cursor, limit+1)
	if err != nil {
		return nil, "", err
	}
	defer rows.Close()
	for rows.Next() {
		var body, hash string
		var r LiveRelease
		if err = rows.Scan(&body, &hash); err != nil {
			return nil, "", err
		}
		if digest([]byte(body)) != hash {
			return nil, "", ErrInsufficientEvidence
		}
		if err = json.Unmarshal([]byte(body), &r); err != nil {
			return nil, "", err
		}
		out = append(out, r)
	}
	if err = rows.Err(); err != nil {
		return nil, "", err
	}
	next := ""
	if len(out) > limit {
		out = out[:limit]
		next = out[len(out)-1].ID
	}
	return out, next, nil
}
func (s *Store) ListLiveRequests(ctx context.Context, ws, id, cursor string, limit int) ([]LiveRequest, string, error) {
	if _, err := readLiveRelease(ctx, s.db, ws, id); err != nil {
		return nil, "", err
	}
	before := int64(LiveRequestLimit + 1)
	if cursor != "" {
		var err error
		before, err = strconv.ParseInt(cursor, 10, 64)
		if err != nil || before < 1 {
			return nil, "", ErrInvalid
		}
	}
	rows, err := s.db.QueryContext(ctx, "SELECT r.body_json,r.body_hash,coalesce(f.body_json,''),coalesce(f.body_hash,'') FROM live_canary_requests r LEFT JOIN live_canary_feedback f ON f.request_id=r.id WHERE r.workspace_id=? AND r.release_id=? AND r.sequence<? ORDER BY r.sequence DESC LIMIT ?", ws, id, before, limit+1)
	if err != nil {
		return nil, "", err
	}
	defer rows.Close()
	out := []LiveRequest{}
	for rows.Next() {
		var body, hash, feedback, feedbackHash string
		var r LiveRequest
		if err = rows.Scan(&body, &hash, &feedback, &feedbackHash); err != nil {
			return nil, "", err
		}
		if digest([]byte(body)) != hash {
			return nil, "", ErrInsufficientEvidence
		}
		if err = json.Unmarshal([]byte(body), &r); err != nil {
			return nil, "", err
		}
		r.Hash = hash
		if err = attachLiveFeedback(&r, feedback, feedbackHash); err != nil {
			return nil, "", err
		}
		out = append(out, r)
	}
	if err = rows.Err(); err != nil {
		return nil, "", err
	}
	next := ""
	if len(out) > limit {
		out = out[:limit]
		next = strconv.FormatInt(out[len(out)-1].Sequence, 10)
	}
	return out, next, nil
}
func (s *Store) LiveEvents(ctx context.Context, ws, id string, after int64, limit int) ([]LiveEvent, error) {
	if _, err := readLiveRelease(ctx, s.db, ws, id); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, "SELECT body_json,event_hash FROM live_canary_events WHERE release_id=? AND sequence>? ORDER BY sequence LIMIT ?", id, after, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []LiveEvent{}
	for rows.Next() {
		var body, hash string
		var v LiveEvent
		if err = rows.Scan(&body, &hash); err != nil {
			return nil, err
		}
		if err = json.Unmarshal([]byte(body), &v); err != nil {
			return nil, err
		}
		v.Hash = ""
		if LiveHash(v) != hash {
			return nil, ErrInsufficientEvidence
		}
		if v.Decision.EvidenceHash != "" && LiveHash(v.Decision.Observations) != v.Decision.EvidenceHash {
			return nil, ErrInsufficientEvidence
		}
		v.Hash = hash
		out = append(out, v)
	}
	return out, rows.Err()
}
func (s *Store) RecoverLive(ctx context.Context, stamp time.Time) error {
	return s.write(ctx, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, "SELECT body_json FROM live_canary_releases")
		if err != nil {
			return err
		}
		var releases []LiveRelease
		for rows.Next() {
			var body string
			var r LiveRelease
			if err = rows.Scan(&body); err != nil {
				rows.Close()
				return err
			}
			if err = json.Unmarshal([]byte(body), &r); err != nil {
				rows.Close()
				return err
			}
			releases = append(releases, r)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
		for _, r := range releases {
			requests, e := tx.QueryContext(ctx, "SELECT body_json FROM live_canary_requests WHERE release_id=? AND status='running'", r.ID)
			if e != nil {
				return e
			}
			var pending []LiveRequest
			for requests.Next() {
				var body string
				var req LiveRequest
				if e = requests.Scan(&body); e != nil {
					requests.Close()
					return e
				}
				if e = json.Unmarshal([]byte(body), &req); e != nil {
					requests.Close()
					return e
				}
				pending = append(pending, req)
			}
			e = requests.Err()
			requests.Close()
			if e != nil {
				return e
			}
			for _, req := range pending {
				req.Status = "interrupted"
				req.ErrorCode = "PROCESS_RESTART"
				req.FinishedAt = stamp.UTC().Format(time.RFC3339Nano)
				req.Hash = ""
				body, hash, e := liveBody(req)
				if e != nil {
					return e
				}
				if _, e = tx.ExecContext(ctx, "UPDATE live_canary_requests SET status='interrupted',body_json=?,body_hash=? WHERE id=?", body, hash, req.ID); e != nil {
					return e
				}
			}
			if r.State == "ACTIVE" || r.State == "PROMOTED" || len(pending) > 0 {
				m := LiveMutation{Release: r, Command: "restart_pause", Reason: "restart_requires_explicit_resume"}
				if r.State != "ROLLED_BACK" {
					m.Release.State = "PAUSED"
				}
				m.Release.Revision++
				m.Release.UpdatedAt = stamp.UTC().Format(time.RFC3339Nano)
				if e = appendLiveEvent(ctx, tx, r, &m); e != nil {
					return e
				}
				if e = saveLiveRelease(ctx, tx, m.Release); e != nil {
					return e
				}
			}
		}
		return nil
	})
}
