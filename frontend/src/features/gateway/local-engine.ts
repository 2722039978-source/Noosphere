import { useQuery, useQueryClient } from '@tanstack/react-query';
import { useCallback } from 'react';
import { create } from 'zustand';
import { persist } from 'zustand/middleware';
import { api, type LocalEngine, type LocalEngineAction, type ModelConfig } from '@/lib/api';

const pending = new Map<string, Promise<LocalEngine>>();
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
  return [engine.id, engine.directory ?? '', model.provider, model.base_url, model.model_name].join('\n');
}

export function useLocalEngines(workspacePath: string, enabled: boolean) {
  const client = useQueryClient();
  const key = ['local-engines', workspacePath];
  const query = useQuery({ queryKey: key, queryFn: () => api.listLocalEngines(workspacePath), enabled, retry: false, staleTime: 4000, refetchInterval: 7000 });
  const operate = useCallback(async (engine: LocalEngine, action: LocalEngineAction, model?: string) => {
    const existing = pending.get(engine.id);
    if (existing) {
      if (action === 'start') return existing;
      throw new Error('本地服务正在切换状态，请稍后重试。');
    }
    const operation = api.controlLocalEngine(engine.id, action, engine.directory, model);
    pending.set(engine.id, operation);
    try { return await operation; }
    finally {
      pending.delete(engine.id);
      void client.invalidateQueries({ queryKey: ['local-engines'] });
    }
  }, [client]);
  return { ...query, engines: query.data?.engines ?? EMPTY_ENGINES, operate };
}
