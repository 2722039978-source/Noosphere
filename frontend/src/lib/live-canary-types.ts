import type { InferenceProfile, ModelRevision } from './model-engineering-types';

export interface LivePolicy { window_seconds: number; minimum_samples_per_arm: number; minimum_feedback_per_arm: number; maximum_error_rate: number; maximum_bad_feedback_rate: number; maximum_latency_ratio: number; auto_pause_consecutive_errors: number }
export type LiveState = 'DRAFT' | 'ACTIVE' | 'PAUSED' | 'PROMOTED' | 'ROLLED_BACK';
export type LiveCommand = 'start' | 'pause' | 'resume' | 'advance' | 'rollback';
export interface LiveRelease { id: string; workspace_id: string; mode: 'live'; title: string; stable: ModelRevision; candidate: ModelRevision; profile: InferenceProfile; policy: LivePolicy; configuration_hash: string; state: LiveState; target_bps: number; revision: number; route_epoch: number; next_sequence: number; window_started_at: string; created_at: string; updated_at: string; event_sequence: number; last_event_hash: string }
export interface LiveRequest { feedback?: { request_id: string; release_id: string; rating: string; note: string; created_at: string; hash: string }; feedback_rating?: string; id: string; release_id: string; workspace_id: string; source: 'live_provider'; sequence: number; route_epoch: number; arm: 'stable' | 'candidate'; model_revision_id: string; status: 'running' | 'succeeded' | 'failed' | 'interrupted'; prompt: string; output: string; output_truncated: boolean; error_code?: string; wall_ns: number; started_at: string; finished_at?: string; input_hash: string; output_hash?: string; identity_before_hash?: string; identity_after_hash?: string; tokens?: number; hash: string }
export interface LiveRule { id: string; status: 'pass' | 'fail' | 'insufficient'; reason: string; observed: number | null; required: number; samples: number }
export interface LiveDecision { allowed: boolean; rules: LiveRule[]; evidence_hash: string; observed_at: string }
export interface LiveArmStats { requests: number; successful: number; errors: number; pending: number; feedback: number; bad_feedback: number; latency_samples: number; mean_latency_ms: number | null; error_rate: number | null; bad_feedback_rate: number | null }
export interface LiveDetail { release: LiveRelease; arms: Record<'stable' | 'candidate', LiveArmStats>; decision: LiveDecision; effective_bps: number; running: number; limitations: string[] }
export interface LiveEvent { release_id: string; sequence: number; command: string; reason: string; from_state: string; to_state: string; route_epoch: number; target_bps: number; configuration_hash: string; decision: LiveDecision; created_at: string; previous_hash: string; hash: string }
type Scope = { workspace_id: string }; type ID = { id: string }; type Key = { idempotency_key: string }; type Page = { cursor?: string; limit?: number };
export type LiveCanaryRequest =
 | ({ operation: 'list_live_canaries' } & Scope & Page)
 | ({ operation: 'create_live_canary'; title: string; stable_revision_id: string; candidate_revision_id: string; profile_id: string; target_bps: number; policy: LivePolicy } & Scope & Key)
 | ({ operation: 'get_live_canary' } & Scope & ID)
 | ({ operation: 'live_canary_command'; command: LiveCommand; expected_revision: number } & Scope & ID & Key)
 | ({ operation: 'live_canary_request'; prompt: string } & Scope & ID & Key)
 | ({ operation: 'list_live_canary_requests' } & Scope & ID & Page)
 | ({ operation: 'get_live_canary_request'; request_id: string } & Scope & ID)
 | ({ operation: 'live_canary_feedback'; request_id: string; rating: 'good' | 'bad'; note: string } & Scope & ID & Key)
 | ({ operation: 'live_canary_events'; after?: number; limit?: number } & Scope & ID);
export interface LiveCanaryResponses {
 list_live_canaries: { releases: LiveRelease[]; next_cursor: string; default_policy: LivePolicy; source: 'live_provider'; request_limit: number };
 create_live_canary: LiveRelease; get_live_canary: LiveDetail; live_canary_command: LiveRelease;
 live_canary_request: LiveRequest; list_live_canary_requests: { requests: LiveRequest[]; next_cursor: string }; get_live_canary_request: LiveRequest;
 live_canary_feedback: LiveRelease; live_canary_events: { events: LiveEvent[]; next_after: number };
}
