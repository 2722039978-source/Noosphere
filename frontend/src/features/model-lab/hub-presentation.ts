import type { ModelConfig } from '@/lib/api';
import type { ModelRevision } from '@/lib/model-engineering-types';
import { resolveModelIdentity } from './model-identity';

export type DeploymentFilter = 'all' | 'local' | 'api';
export function primaryHubModel(models: ModelConfig[], workspaceDefault = '') {
  const enabled = models.filter(model => model.enabled !== false);
  return enabled.find(model => model.id === workspaceDefault) ?? enabled.find(model => model.is_default) ?? enabled[0];
}
export function modelDeployment(model: ModelConfig): 'local' | 'api' | 'unknown' {
  return resolveModelIdentity(model).deployment;
}
export function filterHubModels(models: ModelConfig[], deployment: DeploymentFilter, query: string, sort: 'name' | 'provider') {
  const term = query.trim().toLocaleLowerCase();
  return models.filter(model => (deployment === 'all' || modelDeployment(model) === deployment) && [model.name, model.model_name, model.provider, model.id].join(' ').toLocaleLowerCase().includes(term))
    .sort((a, b) => (sort === 'provider' ? a.provider.localeCompare(b.provider) : 0) || (a.name || a.model_name).localeCompare(b.name || b.model_name));
}
export function matchingRevision(model: ModelConfig, revisions: ModelRevision[]) {
  return revisions.find(revision => revision.gateway_model_id === model.id && revision.provider === model.provider && revision.model_name === model.model_name);
}
export function modelChatDestination(model: ModelConfig) { return `/chat?model=${encodeURIComponent(model.id)}`; }
