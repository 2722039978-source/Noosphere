import type { DiscoveredModel, LocalEngine, ModelConfig } from '@/lib/api';

export type ModelTarget = Pick<ModelConfig, 'provider' | 'base_url' | 'model_name'>;

function localUrl(raw: string) {
  try {
    const url = new URL(raw);
    const local = url.hostname === 'localhost' || url.hostname === '[::1]' || /^127(?:\.\d{1,3}){3}$/.test(url.hostname);
    if (local && ['http:', 'https:'].includes(url.protocol) && !url.username && !url.password && !url.search && !url.hash) return url;
  } catch { /* The configuration can still be edited from model management. */ }
}

export function isLocalModel(model: ModelTarget) {
  const url = localUrl(model.base_url);
  return Boolean(url && model.model_name.trim() && !/\/(?:chat\/completions|generateContent|messages)\/?$/.test(url.pathname));
}

function loopbackEndpoint(raw: string, provider: string) {
  const url = localUrl(raw);
  if (!url) return '';
  const host = url.protocol === 'http:' && url.hostname === 'localhost' ? '127.0.0.1' : url.hostname;
  let path = url.pathname.replace(/\/+$/, '');
  if (provider === 'ollama' && path === '/v1') path = '';
  return url.protocol + '//' + host + (url.port ? ':' + url.port : '') + path;
}

export function modelNameKey(provider: string, name: string) {
  const tag = name.slice(name.lastIndexOf('/') + 1);
  return provider === 'ollama' && tag && !tag.includes(':') ? name + ':latest' : name;
}

export function modelConnectionKey(model: ModelTarget) {
  return JSON.stringify([model.provider, loopbackEndpoint(model.base_url, model.provider) || model.base_url, modelNameKey(model.provider, model.model_name)]);
}

export function localEngineForModel(engines: LocalEngine[], model?: Pick<ModelConfig, 'base_url' | 'provider'>) {
  if (!model) return undefined;
  const endpoint = loopbackEndpoint(model.base_url, model.provider);
  return endpoint ? engines.find(engine => engine.provider === model.provider && loopbackEndpoint(engine.base_url, engine.provider) === endpoint) : undefined;
}

export interface LocalModelChoice {
  key: string;
  name: string;
  target: ModelTarget;
  config?: ModelConfig;
  engine?: LocalEngine;
  model?: DiscoveredModel;
}

export function localEngineName(engine: LocalEngine) {
  return engine.name.replace(/（llama\.cpp）$/, '');
}

export function localModelChoices(configs: ModelConfig[], engines: LocalEngine[], preferredId?: string) {
  const choices = new Map<string, LocalModelChoice>();
  const rank = (model: ModelConfig) => (model.enabled === false ? 0 : 4) + (model.id === preferredId ? 2 : 0) + (model.is_default ? 1 : 0);
  for (const config of configs) {
    if (!isLocalModel(config)) continue;
    const key = modelConnectionKey(config);
    const prior = choices.get(key)?.config;
    if (prior && rank(prior) >= rank(config)) continue;
    const engine = localEngineForModel(engines, config);
    const name = (config.name || config.model_name).replace(/（llama\.cpp）$/, '');
    choices.set(key, { key, name, target: config, config, engine });
  }
  for (const engine of engines) {
    for (const model of engine.models) {
      const target = { provider: engine.provider, base_url: engine.base_url, model_name: model.id };
      const key = modelConnectionKey(target);
      const config = choices.get(key)?.config;
      const name = (config?.name || model.name || model.id).replace(/（llama\.cpp）$/, '');
      choices.set(key, { key, name, target, config, engine, model });
    }
  }
  const counts = new Map<string, number>();
  for (const { name } of choices.values()) counts.set(name, (counts.get(name) ?? 0) + 1);
  return [...choices.values()].map(choice => (counts.get(choice.name) ?? 0) > 1 ? {
    ...choice, name: [choice.name, choice.target.model_name, new URL(choice.target.base_url).host].join(' · '),
  } : choice);
}

export function portableConflict(engines: LocalEngine[], engine?: LocalEngine) {
  const otherId = engine?.id === 'bonsai' ? 'deepseek_local' : engine?.id === 'deepseek_local' ? 'bonsai' : '';
  return engines.find(item => item.id === otherId && (item.state === 'online' || item.state === 'starting' || item.code === 'PORT_IN_USE'));
}

export function needsModelSetup(choice: LocalModelChoice) {
  return !choice.config || Boolean(choice.engine && (choice.engine.state !== 'online' || !choice.model));
}

export function modelChoiceStatus(choice: LocalModelChoice) {
  if (choice.config?.enabled === false) return '已停用';
  switch (choice.engine?.state) {
    case 'starting': return '启动中';
    case 'stopped': return '未启动';
    case 'missing': return '未找到引擎';
    case 'unavailable': return '暂不可用';
    case 'online': return choice.model?.loaded ? '已加载' : choice.model ? '可加载' : '待验证';
    default: return '已配置';
  }
}
