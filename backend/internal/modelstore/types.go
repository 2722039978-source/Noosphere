// Package modelstore persists model engineering facts independently of Workspace.
// Callers validate workspace ownership and revalidate external identities/gates.
package modelstore

import (
	"encoding/json"
	"errors"

	"github.com/noosphere/backend/internal/hostinfo"
)

var (
	ErrReadOnly             = errors.New("SCHEMA_TOO_NEW: store is read-only")
	ErrRevisionConflict     = errors.New("REVISION_CONFLICT")
	ErrStaleLease           = errors.New("STALE_LEASE")
	ErrEvaluationFrozen     = errors.New("EVALUATION_FROZEN")
	ErrModelDrift           = errors.New("MODEL_DRIFT")
	ErrArtifactMissing      = errors.New("ARTIFACT_MISSING")
	ErrInsufficientEvidence = errors.New("INSUFFICIENT_EVIDENCE")
	ErrIdempotencyConflict  = errors.New("IDEMPOTENCY_CONFLICT")
	ErrNotFound             = errors.New("NOT_FOUND")
	ErrInvalid              = errors.New("INVALID_RECORD")
)

func Float(v float64) *float64 { return &v }
func Int(v int64) *int64       { return &v }

type ModelRevision struct {
	Capabilities        []string `json:"capabilities,omitempty"`
	ParameterSize       string   `json:"parameter_size,omitempty"` // Provider-reported label, not an exact parameter count.
	SizeBytes           *int64   `json:"size_bytes,omitempty"`     // Reported model file size; never a VRAM measurement.
	FirstDiscoveredAt   string   `json:"first_discovered_at,omitempty"`
	ID                  string   `json:"id"`
	GatewayModelID      string   `json:"gateway_model_id"`
	Provider            string   `json:"provider"`
	ModelName           string   `json:"model_name"`
	IdentityLevel       string   `json:"identity_level"`
	Digest              string   `json:"digest"`
	DeploymentVersion   string   `json:"deployment_version,omitempty"`
	ModelHash           string   `json:"model_hash,omitempty"`
	ConfigHash          string   `json:"config_hash,omitempty"`
	MetadataHash        string   `json:"metadata_hash,omitempty"`
	TemplateHash        string   `json:"template_hash,omitempty"`
	EndpointFingerprint string   `json:"endpoint_fingerprint,omitempty"`
	EngineVersion       string   `json:"engine_version,omitempty"`
	Format              string   `json:"format,omitempty"`
	Quantization        string   `json:"quantization,omitempty"`
	ParameterCount      *int64   `json:"parameter_count,omitempty"`
	UnknownReason       string   `json:"unknown_reason,omitempty"`
}
type Options struct {
	Temperature *float64 `json:"temperature,omitempty"`
	Seed        *int64   `json:"seed,omitempty"`
	TopP        *float64 `json:"top_p,omitempty"`
	TopK        *int64   `json:"top_k,omitempty"`
	NumCtx      *int64   `json:"num_ctx,omitempty"`
	MaxOutput   *int64   `json:"max_output,omitempty"`
	Stop        []string `json:"stop,omitempty"`
	Think       *bool    `json:"think,omitempty"`
	KeepAlive   string   `json:"keep_alive,omitempty"`
	Parallelism *int64   `json:"parallelism,omitempty"`
	DeadlineNS  *int64   `json:"deadline_ns,omitempty"`
	RetryLimit  *int64   `json:"retry_limit,omitempty"`
}
type UnsupportedOption struct {
	Name   string `json:"name"`
	Reason string `json:"reason"`
}
type InferenceProfile struct {
	ID           string              `json:"id"`
	Requested    Options             `json:"requested"`
	Effective    *Options            `json:"effective,omitempty"`
	Reported     *Options            `json:"reported,omitempty"`
	Confirmation string              `json:"confirmation,omitempty"`
	Unsupported  []UnsupportedOption `json:"unsupported,omitempty"`
}
type Case struct {
	ID              string `json:"id"`
	Category        string `json:"category"`
	InputArtifact   string `json:"input_artifact,omitempty"`
	FixtureArtifact string `json:"fixture_artifact,omitempty"`
	Expected        string `json:"expected"`
	FixtureHash     string `json:"fixture_hash,omitempty"`
}
type DatasetRevision struct {
	ID                 string `json:"id"`
	BenchmarkID        string `json:"benchmark_id"`
	Version            string `json:"version"`
	ContentHash        string `json:"content_hash"`
	ScorerVersion      string `json:"scorer_version"`
	RunnerVersion      string `json:"runner_version,omitempty"`
	CodeLensVersion    string `json:"codelens_version,omitempty"`
	PromptTemplateHash string `json:"prompt_template_hash,omitempty"`
	ContextHash        string `json:"context_hash,omitempty"`
	OrderSeed          int64  `json:"order_seed"`
	License            string `json:"license,omitempty"`
	Source             string `json:"source,omitempty"`
	Cases              []Case `json:"cases"`
}
type Metric struct {
	Value         *float64 `json:"value"`
	Unit          string   `json:"unit"`
	Source        string   `json:"source"`
	Quality       string   `json:"quality"`
	MissingReason string   `json:"missing_reason,omitempty"`
}
type EnvironmentSnapshot struct {
	Hardware         *hostinfo.Inventory `json:"hardware,omitempty"`
	ID               string              `json:"id"`
	OS               string              `json:"os"`
	Arch             string              `json:"arch"`
	CPU              string              `json:"cpu,omitempty"`
	Cores            *int64              `json:"cores,omitempty"`
	RAMBytes         *int64              `json:"ram_bytes,omitempty"`
	GPU              string              `json:"gpu,omitempty"`
	Driver           string              `json:"driver,omitempty"`
	VRAM             Metric              `json:"vram"`
	CollectorVersion string              `json:"collector_version"`
	ToolchainVersion string              `json:"toolchain_version,omitempty"`
}
type Manifest struct {
	SchemaVersion       int                 `json:"schema_version"`
	EncoderVersion      string              `json:"encoder_version"`
	WorkspaceID         string              `json:"workspace_id"`
	WorkflowID          string              `json:"workflow_id"`
	Source              string              `json:"source"`
	ParentEvaluationID  string              `json:"parent_evaluation_id,omitempty"`
	Stable              ModelRevision       `json:"stable"`
	Candidate           ModelRevision       `json:"candidate"`
	Profile             InferenceProfile    `json:"profile"`
	Dataset             DatasetRevision     `json:"dataset"`
	Environment         EnvironmentSnapshot `json:"environment"`
	MemoryMode          string              `json:"memory_mode"`
	MemorySnapshotHash  string              `json:"memory_snapshot_hash,omitempty"`
	ToolRegistryVersion string              `json:"tool_registry_version,omitempty"`
	ToolPermissions     []string            `json:"tool_permissions,omitempty"`
}
type Idempotency struct {
	Key         string
	RequestHash string
}
type CreateEvaluationRequest struct {
	ID          string
	Manifest    Manifest
	Idempotency Idempotency
}
type Evaluation struct {
	ID              string   `json:"id"`
	Manifest        Manifest `json:"manifest"`
	ManifestHash    string   `json:"manifest_hash"`
	Status          string   `json:"status"`
	Revision        int64    `json:"revision"`
	LeaseGeneration int64    `json:"lease_generation"`
	RunID           string   `json:"run_id"`
	CancelRequested bool     `json:"cancel_requested"`
	StartedAt       string   `json:"started_at"`
	FinishedAt      string   `json:"finished_at"`
}
type Result struct {
	InferenceCompleted bool     `json:"inference_completed,omitempty"`
	Actual             string   `json:"actual,omitempty"`
	Expected           string   `json:"expected,omitempty"`
	Difference         string   `json:"difference,omitempty"`
	ErrorStage         string   `json:"error_stage,omitempty"`
	ErrorReason        string   `json:"error_reason,omitempty"`
	EvidenceArtifacts  []string `json:"evidence_artifacts,omitempty"`
	Truncated          bool     `json:"truncated"`
	ScorerVersion      string   `json:"scorer_version,omitempty"`
}
type Metrics struct {
	WallNS          Metric `json:"wall_ns"`
	LoadNS          Metric `json:"load_ns"`
	PromptNS        Metric `json:"prompt_ns"`
	DecodeNS        Metric `json:"decode_ns"`
	Tokens          Metric `json:"tokens"`
	CPU             Metric `json:"cpu"`
	RAM             Metric `json:"ram"`
	AvailableVRAM   Metric `json:"available_vram"`
	LoadingProtocol string `json:"loading_protocol,omitempty"`
	WarmVerified    bool   `json:"warm_verified"`
	WarmupArtifact  string `json:"warmup_artifact,omitempty"`
	WarmupCost      Metric `json:"warmup_cost"`
}
type Attempt struct {
	ID               string  `json:"id"`
	EvaluationID     string  `json:"evaluation_id"`
	CaseID           string  `json:"case_id"`
	Arm              string  `json:"arm"`
	AttemptNo        int     `json:"attempt_no"`
	LeaseGeneration  int64   `json:"lease_generation"`
	State            string  `json:"state"`
	Verdict          string  `json:"verdict"`
	ErrorCode        string  `json:"error_code,omitempty"`
	InputArtifact    string  `json:"input_artifact,omitempty"`
	OutputArtifact   string  `json:"output_artifact,omitempty"`
	Result           Result  `json:"result"`
	Metrics          Metrics `json:"metrics"`
	StartedAt        string  `json:"started_at"`
	FinishedAt       string  `json:"finished_at"`
	SupersedesReason string  `json:"supersedes_reason,omitempty"`
}
type AttemptOutcome struct {
	State          string
	Verdict        string
	ErrorCode      string
	OutputArtifact string
	Result         Result
	Metrics        Metrics
}
type SelectedAttempt struct {
	CaseID       string `json:"case_id"`
	Arm          string `json:"arm"`
	ID           string `json:"id"`
	AttemptNo    int    `json:"attempt_no"`
	State        string `json:"state"`
	Verdict      string `json:"verdict"`
	InputSHA256  string `json:"input_sha256,omitempty"`
	OutputSHA256 string `json:"output_sha256,omitempty"`
	ResultHash   string `json:"result_hash"`
	AttemptHash  string `json:"attempt_hash"`
}
type Counts struct {
	Planned        int `json:"planned"`
	Attempted      int `json:"attempted"`
	Scored         int `json:"scored"`
	Pass           int `json:"pass"`
	Fail           int `json:"fail"`
	Infra          int `json:"infra"`
	Cancelled      int `json:"cancelled"`
	Interrupted    int `json:"interrupted"`
	Unknown        int `json:"unknown"`
	PairedComplete int `json:"paired_complete"`
}
type CategoryCounts struct {
	Stable    Counts `json:"stable"`
	Candidate Counts `json:"candidate"`
}
type Report struct {
	// Analysis is an immutable, versioned extension. Omission preserves legacy bodies.
	Analysis             json.RawMessage           `json:"analysis,omitempty"`
	ID                   string                    `json:"id"`
	EvaluationID         string                    `json:"evaluation_id"`
	SchemaVersion        int                       `json:"schema_version"`
	EncoderVersion       string                    `json:"encoder_version"`
	ManifestHash         string                    `json:"manifest_hash"`
	EvaluationRevision   int64                     `json:"evaluation_revision"`
	AttemptSetHash       string                    `json:"attempt_set_hash"`
	Selected             []SelectedAttempt         `json:"selected"`
	Superseded           []Attempt                 `json:"superseded"`
	Counts               Counts                    `json:"counts"`
	Categories           map[string]CategoryCounts `json:"categories"`
	PairMatrix           [4]int                    `json:"pair_matrix"`
	Hash                 string                    `json:"-"`
	PromotionEligible    bool                      `json:"-"`
	IneligibilityReasons []string                  `json:"-"`
}
type ArtifactMetadata struct {
	MediaType      string `json:"media_type"`
	Provenance     string `json:"provenance"`
	RetentionUntil string `json:"retention_until,omitempty"`
}
type Artifact struct {
	ID        string           `json:"id"`
	SHA256    string           `json:"sha256"`
	SizeBytes int64            `json:"size_bytes"`
	Metadata  ArtifactMetadata `json:"metadata"`
}
type ReleasePolicy struct {
	ReleaseRules        []PolicyRule `json:"release_rules"`
	RollbackRules       []PolicyRule `json:"rollback_rules"`
	RollbackRetentionNS int64        `json:"rollback_retention_ns"`
	ID                  string       `json:"id"`
	SchemaVersion       int          `json:"schema_version"`
	EncoderVersion      string       `json:"encoder_version"`
	Mode                string       `json:"mode"`
	Version             string       `json:"version"`
	Hash                string       `json:"-"`
}
type CreateReleaseRequest struct {
	ID, WorkspaceID, WorkflowID, FilterHash, ReportID, PolicyID string
	Idempotency                                                 Idempotency
}
type Release struct {
	Simulation          json.RawMessage `json:"simulation,omitempty"`
	PromotedAt          string          `json:"promoted_at,omitempty"`
	RollbackUntil       string          `json:"rollback_until,omitempty"`
	ServingRevisionID   string          `json:"serving_revision_id,omitempty"`
	ID                  string          `json:"id"`
	WorkspaceID         string          `json:"workspace_id"`
	WorkflowID          string          `json:"workflow_id"`
	FilterHash          string          `json:"filter_hash"`
	Mode                string          `json:"mode"`
	State               string          `json:"state"`
	PausedFrom          string          `json:"paused_from"`
	StableRevisionID    string          `json:"stable_revision_id"`
	CandidateRevisionID string          `json:"candidate_revision_id"`
	PreviousStable      string          `json:"previous_stable"`
	PolicyID            string          `json:"policy_id"`
	ReportID            string          `json:"report_id"`
	Revision            int64           `json:"revision"`
	RouteEpoch          int64           `json:"route_epoch"`
	TargetBPS           int             `json:"target_bps"`
	AdmissionEnabled    bool            `json:"admission_enabled"`
	ClosedAt            string          `json:"closed_at"`
}

// GateDecision is a caller-computed policy result; the store verifies evidence freshness,
// state adjacency and CAS. It does not implement quality/performance policy evaluation.
type GateDecision struct {
	Rules        []RuleResult `json:"rules"`
	Allowed      bool         `json:"allowed"`
	PolicyHash   string       `json:"policy_hash"`
	EvidenceHash string       `json:"evidence_hash"`
	Reasons      []string     `json:"reasons"`
}
type ReleaseTransition struct {
	ReleaseID        string
	ExpectedRevision int64
	Command          string
	Actor            string
	ReasonCode       string
	EvidenceHash     string
	Decision         GateDecision
	Idempotency      Idempotency
}
type ReleaseEvent struct {
	Simulation   json.RawMessage `json:"simulation,omitempty"`
	ID           int64           `json:"id"`
	ReleaseID    string          `json:"release_id"`
	Sequence     int64           `json:"sequence"`
	FromState    string          `json:"from_state"`
	ToState      string          `json:"to_state"`
	Actor        string          `json:"actor"`
	ReasonCode   string          `json:"reason_code"`
	PolicyHash   string          `json:"policy_hash"`
	EvidenceHash string          `json:"evidence_hash"`
	Decision     GateDecision    `json:"decision"`
	PreviousHash string          `json:"previous_hash"`
	EventHash    string          `json:"-"`
	CreatedAt    string          `json:"created_at"`
}
