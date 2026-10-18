import { create } from 'zustand';
import { persist } from 'zustand/middleware';
import { api, type Workspace } from '@/lib/api';
import { useUiStore } from './ui-store';

export const LOCAL_WORKSPACE: Workspace = {
  id: 'local', name: '本地工作区', project_path: '', project_id: '', default_model_id: '', created_at: '', updated_at: '',
};

export type WorkspaceInput = Partial<Workspace> & Pick<Workspace, 'name' | 'project_path' | 'default_model_id'>;

interface WorkspaceState {
  activeId: string;
  workspaces: Workspace[];
  hydrated: boolean;
  refresh: () => Promise<void>;
  select: (id: string) => void;
  save: (input: WorkspaceInput) => Promise<Workspace>;
}

function syncProject(workspace: Workspace) {
  // Both values come from the same backend record; never carry the old project's ID.
  useUiStore.setState({ projectPath: workspace.project_path, projectId: workspace.project_id });
}

let refreshInFlight: Promise<void> | null = null;

export const useWorkspaceStore = create<WorkspaceState>()(persist((set, get) => ({
  activeId: 'local',
  workspaces: [LOCAL_WORKSPACE],
  hydrated: false,
  refresh: () => {
    if (refreshInFlight) return refreshInFlight;
    refreshInFlight = (async () => {
      const response = await api.listWorkspaces();
      if (!Array.isArray(response.workspaces) || !response.workspaces.length) throw new Error('无法读取工作区列表，请重试。');
      const state = get();
      const active = response.workspaces.find(workspace => workspace.id === state.activeId) ?? response.workspaces.find(workspace => workspace.id === 'local') ?? response.workspaces[0];
      const oldActive = state.workspaces.find(workspace => workspace.id === state.activeId);
      const draftPath = useUiStore.getState().projectPath;
      set({ workspaces: response.workspaces, activeId: active.id, hydrated: true });
      // Preserve an unsaved directory input while refreshing job status. It is not an indexed context.
      const legacyDraft = !state.hydrated && active.id === 'local' && !active.project_path && !!draftPath;
      if (legacyDraft || (state.hydrated && oldActive?.id === active.id && draftPath !== oldActive.project_path)) {
        useUiStore.setState({ projectId: '' });
      } else syncProject(active);
    })().finally(() => { refreshInFlight = null; });
    return refreshInFlight;
  },
  select: id => {
    const workspace = get().workspaces.find(item => item.id === id);
    if (!workspace) return;
    set({ activeId: id });
    syncProject(workspace);
  },
  save: async input => {
    const workspace = await api.saveWorkspace(input);
    set(state => ({ workspaces: state.workspaces.some(item => item.id === workspace.id)
      ? state.workspaces.map(item => item.id === workspace.id ? workspace : item)
      : [...state.workspaces, workspace] }));
    if (get().activeId === workspace.id) syncProject(workspace);
    return workspace;
  },
}), {
  name: 'noosphere-workspace-v1',
  partialize: ({ activeId }) => ({ activeId }),
}));
