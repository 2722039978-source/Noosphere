import { useQuery, useQueryClient } from '@tanstack/react-query';
import { useCallback } from 'react';
import { create } from 'zustand';
import { persist } from 'zustand/middleware';
import { api, type LocalEngine, type LocalEngineAction, type ModelConfig } from '@/lib/api';

const pending = new Map<string, { model?: string; action: LocalEngineAction; operation: Promise<LocalEngine> }>();
const EMPTY_ENGINES: LocalEngine[] = [];

export const useEnginePreferences = create<{ grants: Record<string, string>; grant: (model: ModelConfig, engine: LocalEngine, enabled: boolean) => void; revoke: (engine: LocalEngine) => void }>()(persist(set => ({
  grants: {},
  grant: (model, engine, enabled) => set(state => {
    const grants = { ...state.grants };
    if (enabled) grants[model.id] = engineIdentity(model, engine);
    else delete grants[model.id];
    return { grants };
  }),
  revoke: engine => set(state => ({ grants: Object.fromEntries(Object.entries(state.grants).filter(([, value]) => !value.startsWith(`${engine.id}\n`))) })),
}), { name: 'noosphere-local-engine-permissions', partialize: ({ grants }) => ({ grants }) }));

export function engineIdentity(model: ModelConfig, engine: LocalEngine) {
  const parts = [engine.id, engine.directory ?? '', model.provider, model.base_url, model.model_name];
  if (engine.id === 'custom_llama') {
    const identity = engine.models.find(item => item.id === model.model_name)?.launch_identity;
    if (!identity) return '';
    parts.push(identity);
  }
  return parts.join('\n');
}

export function useLocalEngines(workspacePath: string, enabled: boolean) {
  const client = useQueryClient();
  const key = ['local-engines', workspacePath];
  const query = useQuery({ queryKey: key, queryFn: () => api.listLocalEngines(workspacePath), enabled, retry: false, staleTime: 5000, refetchInterval: query => query.state.data?.engines?.some(engine => engine.state === 'starting') ? 1500 : 15000 });
  const operate = useCallback(async (engine: LocalEngine, action: LocalEngineAction, model?: string) => {
    const existing = pending.get(engine.id);
    if (existing && action !== 'stop') {
      if (existing.action === action && existing.model === model) return existing.operation;
      throw new Error('本地服务正在切换状态，请稍后重试。');
    }
    const expectedIdentity = engine.id === 'custom_llama' && action !== 'stop'
      ? engine.models.find(item => item.id === model)?.launch_identity : undefined;
    if (engine.id === 'custom_llama' && action !== 'stop' && !expectedIdentity) {
      throw new Error('模型加载配置尚未确认，请重新扫描。');
    }
    const operation = api.controlLocalEngine(engine.id, action, engine.directory, model, expectedIdentity);
    pending.set(engine.id, { action, model, operation });
    try { return await operation; }
    finally {
      if (pending.get(engine.id)?.operation === operation) pending.delete(engine.id);
      await client.invalidateQueries({ queryKey: ['local-engines'] });
    }
  }, [client]);
  return { ...query, engines: query.data?.engines ?? EMPTY_ENGINES, operate };
}
