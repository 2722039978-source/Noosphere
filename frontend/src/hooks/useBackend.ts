import {
  createContext,
  createElement,
  useCallback,
  useContext,
  useEffect,
  useRef,
  useState,
  type ReactNode,
} from "react";
import {
  api,
  errMessage,
  type BackendStatus,
  type ModelConfig,
  type SearchResponse,
  type SessionInfo,
  type SystemMetrics,
} from "@/lib/api";
import { useWorkspaceStore } from '@/stores/workspace-store';
import { useUiStore } from '@/stores/ui-store';
import { waitForWorkspaceRun } from '@/lib/task-client';
import { MODEL_IDENTITY_UPDATED } from '@/lib/model-identity-events';

// re-export 兼容旧模块的类型导入
export type { ModuleState } from "@/lib/api";

export type { ModelConfig, SessionInfo, SystemMetrics, BackendStatus };

/** @deprecated 类型请从 "@/lib/api" 导入, 此处仅为兼容旧模块保留 */
export interface ModuleStateCompat {
  name: string;
  state: string;
  error?: string;
}

function useBackendController() {
  const [health, setHealth] = useState<string>("checking");
  const [modules, setModules] = useState<Record<string, ModuleStateCompat>>({});
  const [models, setModels] = useState<ModelConfig[]>([]);
  const [modelsReady, setModelsReady] = useState(false);
  const [modelsError, setModelsError] = useState<string | null>(null);
  const [sessions, setSessions] = useState<SessionInfo[]>([]);
  const [metrics, setMetrics] = useState<SystemMetrics | null>(null);
  const [backendStatus, setBackendStatus] = useState<BackendStatus>({ running: false, restarts: 0 });
  const [retryCount, setRetryCount] = useState(0);
  const [connectionError, setConnectionError] = useState<string | null>(null);
  const [startError, setStartError] = useState<string | null>(null);
  const [startingBackend, setStartingBackend] = useState(false);
  const startInFlight = useRef(false);
  const healthInFlight = useRef<Promise<void> | null>(null);
  const statusInFlight = useRef<Promise<void> | null>(null);
  const modelLoadGeneration = useRef(0);

  const checkHealth = useCallback(() => {
    if (healthInFlight.current) return healthInFlight.current;
    const request = (async () => {
      try {
        const d = await api.checkHealth();
        setHealth(d.status === "ok" ? "online" : "error");
        setConnectionError(d.status === "ok" ? null : `后端健康检查返回异常状态：${d.status}`);
        if (d.status === "ok") setStartError(null);
      // 兼容: ModuleState.state 在 Go 端可能为 null, 归一为字符串
        const m: Record<string, ModuleStateCompat> = {};
        for (const [k, v] of Object.entries(d.modules || {})) {
          m[k] = { name: v.name, state: v.state ?? "unknown", error: v.error ?? undefined };
        }
        setModules(m);
        setRetryCount(0);
      } catch (error) {
        setHealth("offline");
        setConnectionError(errMessage(error));
        setRetryCount((c) => c + 1);
      }
    })();
    healthInFlight.current = request;
    void request.finally(() => { healthInFlight.current = null; });
    return request;
  }, []);

  const checkBackendStatus = useCallback(() => {
    if (statusInFlight.current) return statusInFlight.current;
    const request = (async () => {
      try {
        setBackendStatus(await api.getBackendStatus());
      } catch {
        /* 保留最近的原生启动错误与路径，连接失败由健康检查报告。 */
      }
    })();
    statusInFlight.current = request;
    void request.finally(() => { statusInFlight.current = null; });
    return request;
  }, []);

  const restartBackend = useCallback(async () => {
    // 底栏和设置共用一个任务，避免连续点击创建重复的启动请求。
    if (startInFlight.current) return;
    startInFlight.current = true;
    setStartingBackend(true);
    setStartError(null);
    setHealth("checking");
    try {
      await api.startBackend();
    } catch (error) {
      setStartError(errMessage(error));
    } finally {
      // 先等之前的轮询结束，再读取启动后的状态，避免复用旧请求结果。
      await Promise.allSettled([healthInFlight.current, statusInFlight.current]);
      await Promise.allSettled([checkBackendStatus(), checkHealth()]);
      startInFlight.current = false;
      setStartingBackend(false);
    }
  }, [checkBackendStatus, checkHealth]);

  const loadModels = useCallback(async () => {
    const generation=++modelLoadGeneration.current;
    try {
      const d = await api.getModels();
      if(generation!==modelLoadGeneration.current)return;
      setModels(d.models || []);
      setModelsReady(true);
      setModelsError(null);
    } catch (error) {
      if(generation===modelLoadGeneration.current)setModelsError(errMessage(error));
    }
  }, []);

  useEffect(() => {
    const refreshIdentity = () => {void loadModels();};
    window.addEventListener(MODEL_IDENTITY_UPDATED,refreshIdentity);
    return () => window.removeEventListener(MODEL_IDENTITY_UPDATED,refreshIdentity);
  }, [loadModels]);

  const addModel = useCallback(
    async (model: Record<string, unknown>) => {
      await api.addModel(model as unknown as ModelConfig);
      await loadModels();
    },
    [loadModels]
  );

  const deleteModel = useCallback(
    async (id: string) => {
      await api.deleteModel(id);
      await loadModels();
    },
    [loadModels]
  );

  const loadSessions = useCallback(async () => {
    try {
      const d = await api.listSessions();
      setSessions(d.sessions || []);
    } catch {
      /* 会话加载失败静默 */
    }
  }, []);

  const createSession = useCallback(
    async (name: string) => {
      const d = await api.createSession(name);
      await loadSessions();
      return d.session_id;
    },
    [loadSessions]
  );

  const storeMemory = useCallback(
    async (
      sessionId: string,
      content: string,
      type = "episodic",
      tags?: string[],
      metadata?: Record<string, string>
    ) => api.storeMemory(sessionId, content, type, tags, metadata),
    []
  );

  const searchMemories = useCallback(
    async (sessionId: string, query: string, topK = 10): Promise<SearchResponse> =>
      api.searchMemories(sessionId, query, topK),
    []
  );

  const loadMetrics = useCallback(async () => {
    try {
      setMetrics(await api.metrics());
    } catch {
      /* 指标获取失败保持旧值 */
    }
  }, []);

  const diagnose = useCallback(async (issue: string) => api.diagnose(issue) as Promise<any>, []);

  const executeTool = useCallback(
    async (name: string, args?: Record<string, unknown>) =>
      api.executeTool(name, args) as Promise<any>,
    []
  );

  const loadGatewayStats = useCallback(async () => {
    try {
      return (await api.gatewayStats()) as any;
    } catch {
      return null;
    }
  }, []);

  const loadNebulaStats = useCallback(async () => {
    try {
      return (await api.nebulaStats()) as any;
    } catch {
      return null;
    }
  }, []);

  const listTools = useCallback(async () => {
    try {
      return (await api.listTools()) as any;
    } catch {
      return null;
    }
  }, []);

  const getDevOpsStatus = useCallback(async () => {
    try {
      return (await api.devopsStatus()) as any;
    } catch {
      return null;
    }
  }, []);

  const chat = useCallback(
    async (messages: { role: string; content: string }[], modelId?: string) =>
      api.chat(messages, modelId),
    []
  );

  // codelens 三方法: 后端为动态 JSON (Python 服务), 旧页面动态消费, P5 重写时收紧
  const indexProject = useCallback(
    async (projectPath: string, exclude?: string[]) => {
      const workspaceId = useWorkspaceStore.getState().activeId;
      if (!useWorkspaceStore.getState().hydrated) await useWorkspaceStore.getState().refresh();
      let workspace = useWorkspaceStore.getState().workspaces.find(item => item.id === workspaceId);
      if (!workspace) throw new Error('工作区不存在，请重新选择工作区。');
      if (!projectPath.trim()) throw new Error('请先填写项目目录。');
      if (workspace.project_path !== projectPath.trim()) {
        workspace = await useWorkspaceStore.getState().save({ ...workspace, project_path: projectPath.trim() });
      }
      const root = workspace.project_path;
      const submitted = await api.submitWorkspaceRun(workspaceId, 'index', `索引 · ${workspace.name}`, { exclude });
      const completed = await waitForWorkspaceRun(submitted.id, api.getWorkspaceRun);
      // Backend owns the binding; a late completion cannot overwrite another workspace or draft root.
      if (useWorkspaceStore.getState().activeId === workspaceId && useUiStore.getState().projectPath === root) {
        await useWorkspaceStore.getState().refresh();
      }
      return completed.result as any;
    },
    []
  );

  const searchCode = useCallback(
    async (projectPath: string, query: string) =>
      api.codelensSearch(projectPath, query) as Promise<any>,
    []
  );

  const analyzeFile = useCallback(
    async (projectPath: string, file: string) =>
      api.codelensAnalyze(projectPath, file) as Promise<any>,
    []
  );

  useEffect(() => {
    void Promise.allSettled([checkHealth(), checkBackendStatus()]);
  }, [checkBackendStatus, checkHealth]);

  useEffect(() => {
    // 首次连接和掉线恢复都补齐缓存；健康轮询成功不会重复加载资源。
    if (health === "online") {
      void Promise.allSettled([loadModels(), loadSessions(), loadMetrics()]);
      void useWorkspaceStore.getState().refresh().catch(() => { /* The workspace editor presents actionable read errors. */ });
    }
  }, [health, loadMetrics, loadModels, loadSessions]);

  useEffect(() => {
    const t = setInterval(() => {
      void checkHealth();
      void checkBackendStatus();
    }, health === "online" ? 10000 : 3000);
    return () => clearInterval(t);
  }, [checkBackendStatus, checkHealth, health]);

  const backendError = health === "online" ? null :
    startError || backendStatus.error?.trim() || connectionError;

  return {
    health, modules, models, modelsReady, modelsError, sessions, metrics, backendStatus, retryCount,
    backendError, startingBackend, restartBackend,
    checkHealth, loadModels, addModel, deleteModel,
    loadSessions, createSession, storeMemory, searchMemories,
    loadMetrics, diagnose, executeTool,
    loadGatewayStats, loadNebulaStats, listTools, getDevOpsStatus,
    chat, indexProject, searchCode, analyzeFile,
  };
}

export type BackendApi = ReturnType<typeof useBackendController>;

const BackendContext = createContext<BackendApi | null>(null);

/** 全应用唯一后端状态源，集中健康轮询与资源缓存。 */
export function BackendProvider({ children }: { children: ReactNode }) {
  const backend = useBackendController();
  return createElement(BackendContext.Provider, { value: backend }, children);
}

export function useBackend(): BackendApi {
  const backend = useContext(BackendContext);
  if (!backend) {
    throw new Error("useBackend must be used within BackendProvider");
  }
  return backend;
}
