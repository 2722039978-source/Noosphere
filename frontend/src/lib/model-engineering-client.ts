import { invoke } from '@tauri-apps/api/core';
import { api } from './api';
import { waitForWorkspaceRun } from './task-client';
import { MODEL_IDENTITY_UPDATED } from './model-identity-events';
import type { ModelEngineeringError, ModelEngineeringRequest, ModelEngineeringResponses } from './model-engineering-types';

/** The idempotency key belongs to the caller's intent; an explicit retry reuses this request unchanged. */
export async function modelEngineering<R extends ModelEngineeringRequest>(request: R): Promise<ModelEngineeringResponses[R['operation']]> {
  const result = await invoke<ModelEngineeringResponses[R['operation']]>('model_engineering', { request });
  if (request.operation === 'discover' || request.operation === 'register_model') {
    window.dispatchEvent(new Event(MODEL_IDENTITY_UPDATED));
  }
  return result;
}

/** Keep HTTP code, status, current revision, and gate decisions visible after IPC rejection. */
export function asModelEngineeringError(error: unknown): ModelEngineeringError {
  if (typeof error === 'object' && error !== null && 'message' in error && typeof error.message === 'string') {
    return error as ModelEngineeringError;
  }
  return { message: String(error) };
}

/** Observing a run never owns or cancels the durable backend task. */
export function waitForModelEngineeringRun(runId: string, options: Parameters<typeof waitForWorkspaceRun>[2] = {}) {
  return waitForWorkspaceRun(runId, api.getWorkspaceRun, options);
}
