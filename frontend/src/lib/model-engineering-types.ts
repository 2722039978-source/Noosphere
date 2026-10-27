import type { HardwareInventory } from './hardware-types';
/** T8's fixed native IPC contract. Dates and hashes are server supplied strings. */
export type EvidenceSource = 'offline_real' | 'offline_fixture' | 'simulation_replay' | 'simulation_injected' | 'shadow_live' | 'canary_live';
export type RuleStatus = 'pass' | 'fail' | 'insufficient' | 'not_applicable';
export type RunStatus = 'queued' | 'running' | 'completed' | 'failed' | 'cancelled' | 'interrupted';
export type EvaluationStatus = 'queued' | 'running' | 'completed' | 'incomplete' | 'failed' | 'cancelled' | 'interrupted';
export type ReleaseState = 'DRAFT' | 'OFFLINE_EVAL' | 'SHADOW' | 'CANARY_5' | 'CANARY_25' | 'CANARY_50' | 'PROMOTED' | 'PAUSED' | 'REJECTED' | 'ROLLED_BACK';
export type ReleaseCommand = 'start_offline' | 'advance' | 'pause' | 'resume' | 'reject' | 'rollback';
export type RuleValue = number | boolean | string | null;

export interface ModelIdentity {
  name: string; digest: string; engine_version: string; metadata_hash: string; config_hash: string;
  template_hash: string; format: string; family: string; parameter_size: string; quantization: string;
  size_bytes: number; capabilities: string[]; local_verified: boolean; ineligibility_reason?: string; limitations?: string[];
}
export interface ModelDiscovery { engine_version: string; models: ModelIdentity[] }
export interface ModelRevision {
  id: string; gateway_model_id: string; provider: string; model_name: string; identity_level: string;
  digest: string; deployment_version?: string; model_hash?: string; config_hash?: string; metadata_hash?: string;
  template_hash?: string; endpoint_fingerprint?: string; engine_version?: string; format?: string;
  quantization?: string; parameter_count?: number; parameter_size?: string; size_bytes?: number;
  first_discovered_at?: string; capabilities?: string[]; unknown_reason?: string;
}
export interface Options {
  temperature: number; seed: number; num_ctx: number; max_output: number;
  top_p?: number; top_k?: number; stop?: string[]; think?: boolean; keep_alive?: string;
  parallelism?: number; deadline_ns?: number; retry_limit?: number;
}
export interface InferenceProfile { id: string; requested: Options; effective?: Partial<Options>; reported?: Partial<Options>; confirmation?: string; unsupported?: { name: string; reason: string }[] }
export interface Case { id: string; category: string; input_artifact?: string; fixture_artifact?: string; expected: string; fixture_hash?: string }
export interface DatasetRevision {
  id: string; benchmark_id: string; version: string; content_hash: string; scorer_version: string;
  runner_version?: string; codelens_version?: string; prompt_template_hash?: string; context_hash?: string;
  order_seed: number; license?: string; source?: string; cases: Case[];
}
export interface Metric { value: number | null; unit: string; source: string; quality: string; missing_reason?: string }
export interface EnvironmentSnapshot {
  hardware?: HardwareInventory;
  id: string; os: string; arch: string; cpu?: string; cores?: number; ram_bytes?: number; gpu?: string;
  driver?: string; vram: Metric; collector_version: string; toolchain_version?: string;
}
export interface Manifest {
  schema_version: number; encoder_version: string; workspace_id: string; workflow_id: string;
  source: 'offline_real' | 'offline_fixture'; parent_evaluation_id?: string;
  stable: ModelRevision; candidate: ModelRevision; profile: InferenceProfile; dataset: DatasetRevision;
  environment: EnvironmentSnapshot; memory_mode: string; memory_snapshot_hash?: string;
  tool_registry_version?: string; tool_permissions?: string[];
}
export interface Evaluation {
  id: string; manifest: Manifest; manifest_hash: string; status: EvaluationStatus; revision: number;
  lease_generation: number; run_id: string; cancel_requested: boolean; started_at: string; finished_at: string;
}
export interface Counts {
  planned: number; attempted: number; scored: number; pass: number; fail: number; infra: number;
  cancelled: number; interrupted: number; unknown: number; paired_complete: number;
}
export interface AttemptSummary {
  id: string; case_id: string; arm: 'stable' | 'candidate'; attempt_no: number;
  state: string; verdict: string; error_code?: string; supersedes_reason?: string;
}
export interface AttemptResult {
  inference_completed?: boolean; actual?: string; expected?: string; difference?: string;
  error_stage?: string; error_reason?: string; evidence_artifacts?: string[]; truncated: boolean; scorer_version?: string;
}
export interface AttemptMetrics {
  wall_ns: Metric; load_ns: Metric; prompt_ns: Metric; decode_ns: Metric; tokens: Metric;
  cpu: Metric; ram: Metric; available_vram: Metric; loading_protocol?: string;
  warm_verified: boolean; warmup_artifact?: string; warmup_cost: Metric;
}
export interface Attempt extends AttemptSummary {
  evaluation_id: string; lease_generation: number; input_artifact?: string; output_artifact?: string;
  result: AttemptResult; metrics: AttemptMetrics; started_at: string; finished_at: string;
}
export interface SelectedAttempt {
  case_id: string; arm: 'stable' | 'candidate'; id: string; attempt_no: number; state: string;
  verdict: string; input_sha256?: string; output_sha256?: string; result_hash: string; attempt_hash: string;
}
export interface Report {
  analysis?: unknown; id: string; evaluation_id: string; schema_version: number; encoder_version: string;
  manifest_hash: string; evaluation_revision: number; attempt_set_hash: string; selected: SelectedAttempt[];
  superseded: Attempt[]; counts: Counts; categories: Record<string, { stable: Counts; candidate: Counts }>;
  pair_matrix: [number, number, number, number];
}
export interface RuleResult {
  id: string; observed: RuleValue; required: RuleValue; unit: string; evidence_ids: string[];
  status: RuleStatus; reason: string; sample_count: number; observation_window_ns: number;
}
export interface GateDecision { rules: RuleResult[]; allowed: boolean; policy_hash: string; evidence_hash: string; reasons: string[] }
export interface PolicyRule {
  id: string; domain: string; stage: string; operator: string; required: Exclude<RuleValue, null>;
  unit: string; minimum_samples: number; observation_window_ns: number;
}
export interface ReleasePolicy {
  id: string; schema_version: number; encoder_version: string; mode: 'simulation'; version: string;
  policy_hash: string; release_rules: PolicyRule[]; rollback_rules: PolicyRule[]; rollback_retention_ns: number;
}
export interface SimulationEvidence {
  source: 'simulation_replay' | 'simulation_injected'; offline_source: 'offline_real' | 'offline_fixture';
  [key: string]: unknown;
}
export interface Release {
  id: string; workspace_id: string; workflow_id: string; filter_hash: string; mode: 'simulation';
  state: ReleaseState; paused_from: string; stable_revision_id: string; candidate_revision_id: string;
  previous_stable: string; policy_id: string; report_id: string; revision: number; route_epoch: number;
  target_bps: number; admission_enabled: boolean; closed_at: string;
  simulation?: SimulationEvidence; promoted_at?: string; rollback_until?: string; serving_revision_id?: string;
}
export interface ReleaseEvent {
  id: number; release_id: string; sequence: number; from_state: string; to_state: string; actor: string;
  reason_code: string; policy_hash: string; evidence_hash: string; decision: GateDecision;
  previous_hash: string; event_hash: string; created_at: string; simulation?: SimulationEvidence;
}
export interface DecisionPreview { release_id: string; revision: number; next_state: string; decision: GateDecision; evidence_hash: string }
export interface ArtifactContent {
  id: string; content?: string; missing: boolean; truncated: boolean;
  /** True when the presentation view omits private raw protocol fields; stored evidence stays unchanged. */
  redacted?: boolean;
}
export interface EvaluationReference { evaluation_id: string; run_id: string; status: EvaluationStatus; revision: number }
export interface SimulationReference { release_id: string; run_id: string; status: RunStatus }
export interface ModelEngineeringError {
  message: string; status?: number; code?: string;
  details?: { current_revision?: number; gates?: RuleResult[]; [key: string]: unknown };
}

type Page = { cursor?: string; limit?: number };
type Scope = { workspace_id: string };
type Key = { idempotency_key: string };
export type ModelEngineeringRequest =
  | { operation: 'discover'; gateway_model_id: string }
  | { operation: 'register_model'; gateway_model_id: string; discovered_identity_hash: string }
  | ({ operation: 'list_models' | 'list_profiles' } & Page)
  | { operation: 'list_datasets' | 'list_policies' | 'environment' }
  | { operation: 'create_profile'; requested: Options }
  | ({ operation: 'create_evaluation'; stable_revision_id: string; candidate_revision_id: string; profile_id: string; dataset_id?: string; workflow_id?: string } & Scope & Key)
  | ({ operation: 'list_evaluations' | 'list_releases' } & Scope & Page)
  | ({ operation: 'get_evaluation'; id: string } & Scope & Page)
  | ({ operation: 'evaluation_command'; id: string; command: 'cancel' | 'resume'; expected_revision: number } & Scope & Key)
  | ({ operation: 'evaluation_command'; id: string; command: 'retry_case'; expected_revision: number; case_id: string; arm: 'stable' | 'candidate' } & Scope & Key)
  | ({ operation: 'get_report' | 'get_release'; id: string } & Scope)
  | ({ operation: 'get_case'; evaluation_id: string; case_id: string } & Scope & Page)
  | ({ operation: 'create_release'; mode: 'simulation'; workflow_id: string; report_id: string; policy_id?: 'simulation-demo-v1'; task_type_filter?: 'codetask_go' } & Scope & Key)
  | ({ operation: 'preview_release'; id: string; command: ReleaseCommand } & Scope)
  | ({ operation: 'release_command'; id: string; command: ReleaseCommand; expected_revision: number } & Scope & Key)
  | ({ operation: 'simulate_release'; id: string; scenario: 'replay_stage' | 'inject_oom' | 'inject_failure'; expected_revision: number; seed?: number; virtual_seconds?: number } & Scope & Key)
  | ({ operation: 'release_events'; id: string; after?: number; limit?: number } & Scope);

export interface ModelEngineeringResponses {
  discover: ModelDiscovery; register_model: ModelRevision; list_models: { models: ModelRevision[]; next_cursor: string };
  list_datasets: { datasets: DatasetRevision[] }; create_profile: InferenceProfile;
  list_profiles: { profiles: InferenceProfile[]; next_cursor: string };
  list_policies: { policies: ReleasePolicy[] }; environment: { environment: EnvironmentSnapshot; preflight: { available: boolean; code?: string; budget?: { max_context: number; max_output: number; inference_timeout_ns: number; cleanup_timeout_ns: number } } };
  create_evaluation: EvaluationReference; list_evaluations: { evaluations: Evaluation[]; next_cursor: string };
  get_evaluation: { evaluation: Evaluation; attempts: AttemptSummary[]; progress: Counts; latest_report_id: string; next_cursor: string };
  evaluation_command: EvaluationReference;
  get_report: { report: Report; report_hash: string; promotion_eligibility: { eligible: boolean; reasons: string[] } };
  get_case: { case: Case; attempts: Attempt[]; artifacts: ArtifactContent[]; next_cursor: string };
  create_release: Release; list_releases: { releases: Release[]; next_cursor: string }; get_release: Release;
  preview_release: DecisionPreview; release_command: Release; simulate_release: SimulationReference;
  release_events: { events: ReleaseEvent[]; next_after: number };
}
