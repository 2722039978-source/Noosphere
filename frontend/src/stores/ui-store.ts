import { create } from "zustand";
import { persist } from "zustand/middleware";

export type RouteId =
  | "assistant"
  | "home"
  | "gateway"
  | "nebula"
  | "codelens"
  | "devops"
  | "settings";

interface UiState {
  /** 当前激活路由 (与 URL 同步由路由层负责) */
  route: RouteId;
  /** 项目路径 (CodeLens 索引 / RAG 上下文共享) */
  projectPath: string;
  projectId: string;
  setProjectIndex: (path: string, id: string) => void;
  setRoute: (r: RouteId) => void;
  setProjectPath: (p: string) => void;
}

export const useUiStore = create<UiState>()(persist((set) => ({
  route: "assistant",
  // 由用户选择项目；不把开发者机器上的绝对路径固化进发布包。
  projectPath: "",
  projectId: "",
  setProjectIndex: (path, id) => set((state) => state.projectPath === path ? { projectId: id } : {}),
  setRoute: (route) => set({ route }),
  setProjectPath: (projectPath) => set((state) => ({ projectPath, projectId: state.projectPath === projectPath ? state.projectId : '' })),
}), { name: 'noosphere-project-v1', partialize: ({ projectPath, projectId }) => ({ projectPath, projectId }) }));
