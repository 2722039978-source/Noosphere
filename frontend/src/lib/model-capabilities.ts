import type { Capability, ModelConfig } from './api';

export type CapabilityKey = 'chat' | 'agent_tools' | 'native_evaluation';
const names = { chat: '对话', agent_tools: 'Agent 工具', native_evaluation: '原生测评' };
/** Never derive protocol support from vendor labels, endpoint names or branding. */
export function capabilityFor(model: Pick<ModelConfig, 'capabilities'> | undefined, key: CapabilityKey): Capability | null {
  const caps = model?.capabilities;
  if (!caps || caps.schema_version !== 1 || caps.source !== 'gateway-adapter-policy-v1') return null;
  const item = caps[key];
  if (!item || typeof item.supported !== 'boolean' || typeof item.available !== 'boolean' || !item.code || !item.reason || !['not_checked', 'requires_discovery', 'not_applicable'].includes(item.verification) || item.available && !item.supported) return null;
  return item;
}
export function capabilityLabel(key: CapabilityKey, item: Capability | null): string {
  if (!item) return `${names[key]}能力未知`;
  if (!item.supported) return `不支持${names[key]}`;
  if (!item.available) return `${names[key]}暂不可用`;
  if (key === 'native_evaluation' && item.verification === 'requires_discovery') return '原生测评待核验';
  return `支持${names[key]}`;
}
export function agentCapability(model?: Pick<ModelConfig, 'capabilities'>) {
  const item = capabilityFor(model, 'agent_tools');
  // Older backends keep their existing server-checked path. The UI labels this
  // unknown; absence of a declaration never becomes a positive support claim.
  return { known: !!item, blocked: !!item && !item.available, reason: item?.reason || '尚未取得 Agent 能力说明，执行前仍由后端校验。可刷新连接信息。' };
}
export function selectedChatModel(models: ModelConfig[], modelID: string, workspaceDefault = '') {
  return modelID ? models.find(model => model.id === modelID)
    : workspaceDefault ? models.find(model => model.id === workspaceDefault)
    : models.find(model => model.is_default && model.enabled !== false) ?? models.find(model => model.enabled !== false);
}
/** Shared by the composer, welcome prompts and history regeneration, before history is changed. */
export function chatCapabilityBlocker(model: ModelConfig | undefined, agentMode: boolean): string {
  if (!model) return '所选模型连接不可用，请重新选择模型。';
  if (model.enabled === false) return '所选模型连接已停用。';
  const cap = capabilityFor(model, agentMode ? 'agent_tools' : 'chat');
  return cap && !cap.available ? cap.reason : '';
}
