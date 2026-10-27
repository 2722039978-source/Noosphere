/**
 * 类型安全的 Tauri IPC 客户端
 * 类型与 src-tauri/src/commands/types.rs 一一对齐 (Rust serde 直接返回结构化对象)
 */
import { invoke as desktopInvoke, type Channel } from "@tauri-apps/api/core";

function invoke<T>(command: string, args?: Record<string, unknown>): Promise<T> {
  if (typeof window === 'undefined' || !('__TAURI_INTERNALS__' in window)) return Promise.reject(new Error('当前为浏览器预览，请在 Noosphere 桌面应用中连接本地服务。'));
  return desktopInvoke<T>(command, args);
}

// ============ 错误 ============

/** Rust ApiError: { message } */
export interface ApiError {
  message: string;
}

export function errMessage(e: unknown): string {
  if (typeof e === "string") return e;
  if (e && typeof e === "object" && "message" in e) return String((e as ApiError).message);
  return String(e);
}

// ============ 类型定义 (对齐 types.rs) ============

export interface ModuleState {
  name: string;
  state?: string | null;
  error?: string | null;
}

export interface HealthResponse {
  status: string;
  version: string;
  modules: Record<string, ModuleState>;
  time: string;
}

export interface BackendStatus {
  running: boolean;
  restarts: number;
  error?: string | null;
  data_dir?: string | null;
}

export interface ModelConfig {
  id: string;
  name: string;
  provider: string;
  base_url: string;
  model_name: string;
  vendor?: string;
  model_family?: string;
  deployment?: 'local'|'api'|'';
  context_length?: number;
  metadata?: { vendor?:string;family?:string;architecture?:string;parameter_size?:string;quantization?:string;context_length?:number;source?:string };
  enabled?: boolean | null;
  is_default?: boolean | null;
  api_key?: string | null;
}

export interface ModelsResponse {
  models: ModelConfig[];
  count?: number | null;
}

export interface ModelSelection {
  scope: 'session' | 'workspace' | 'global';
  workspace_id: string;
  session_id: string;
  model_id: string;
}
export interface ModelSelectionResult {
  workspace_id: string;
  session_id: string;
  model_id: string;
  bound: boolean;
}

export interface ChatMessageParam {
  role: string;
  content: string;
}

export interface ChatResponse {
  id?: string | null;
  model?: string | null;
  provider?: string | null;
  content: string;
  tokens_used?: number | null;
  latency_ms?: number | null;
  timestamp?: string | null;
}

/** 流式 Channel 消息 (Rust StreamChunk) */
export interface StreamChunk {
  request_id?: string;
  token?: string;
  content?: string;
  count?: number;
  error?: string;
  done?: boolean;
  status?: "completed" | "cancelled" | "failed";
}

export interface SessionInfo {
  id: string;
  name: string;
  created_at: string;
  memory_count?: number | null;
}

export interface SessionsResponse {
  sessions: SessionInfo[];
  count?: number | null;
}

export interface CreateSessionResponse {
  session_id: string;
  name?: string | null;
}

export interface MemoryRecord {
  id: string;
  session_id: string;
  content: string;
  type?: string | null;
  tags: string[];
  metadata: Record<string, string>;
  importance?: number | null;
  created_at: string;
}

export interface SearchHit {
  key: string;
  score: number;
  record?: MemoryRecord | null;
}

export interface SearchResponse {
  results: SearchHit[];
  count?: number | null;
  query?: string | null;
}

export interface SystemMetrics {
  cpu: { usage_percent: number; cores: number };
  memory: { total_gb: number; used_gb: number; usage_percent: number };
  disk: { total_gb: number; free_gb: number; usage_percent: number };
  process_count: number;
  goroutines?: number | null;
  timestamp: string;
}

export interface Workspace {
  id: string;
  name: string;
  project_path: string;
  project_id: string;
  default_model_id: string;
  created_at: string;
  updated_at: string;
}
export interface KnowledgeSource {
  kind: 'note' | 'chat' | 'file'; file?: string; start_line?: number; end_line?: number;
  source_hash?: string; project_path?: string; session_id?: string; message_id?: string;
}
export interface KnowledgeMemory {
  id: string; workspace_id: string; content: string;
  category: 'convention' | 'decision' | 'preference' | 'solution';
  status: 'proposed' | 'confirmed' | 'rejected' | 'superseded'; revision: number;
  source: KnowledgeSource; created_at: string; updated_at: string;
  valid_until?: string; supersedes?: string; supersedes_revision?: number; superseded_by?: string; inactive_reason?: string;
}
export interface ContextCitation {
  id: string; kind: 'code' | 'memory'; title: string; excerpt: string; workspace_id: string;
  project_id?: string; file?: string; start_line?: number; end_line?: number;
  source_hash?: string; symbol?: string; memory_id?: string; revision?: number;
}
export interface PreparedContext {
  workspace_id: string; project_id: string; context: string;
  citations: ContextCitation[]; warnings: string[]; mode: 'lexical' | 'semantic';
}
export interface WorkspaceRun {
  id: string;
  workspace_id: string;
  session_id: string;
  kind: 'chat' | 'index' | 'diagnosis' | 'agent' | 'evaluation' | 'release_simulation' | (string & {});
  reference_id?: string;
  reference_generation?: number;
  title: string;
  status: 'queued' | 'running' | 'completed' | 'failed' | 'cancelled' | 'interrupted';
  phase: string;
  error: string;
  model_id: string;
  created_at: string;
  updated_at: string;
  result: unknown;
}
export interface AgentAction {
 id:string;workspace_id:string;run_id:string;project_path:string;kind:'file'|'test'|'service';status:string;reason:string;path?:string;before?:string;after?:string;existed:boolean;before_hash?:string;after_hash?:string;runner?:string;command?:string;service?:string;target?:string;previous?:string;output?:string;error?:string;
}
export interface RunEvent {
  seq: number;
  run_id: string;
  phase: string;
  message: string;
  data: unknown;
  created_at: string;
}

// ============ IPC 封装 (零 JSON.parse) ============

export const api = {
  listAgentActions: (workspaceId:string,runId:string) => invoke<{actions:AgentAction[]}>('list_agent_actions',{workspaceId,runId}),
  decideAgentAction: (workspaceId:string,id:string,decision:string) => invoke<AgentAction>('decide_agent_action',{workspaceId,id,decision}),
  listKnowledge: (workspaceId: string) => invoke<{ memories: KnowledgeMemory[] }>('list_knowledge', { workspaceId }),
  saveKnowledge: (workspaceId: string, memory: Partial<KnowledgeMemory> & Pick<KnowledgeMemory, 'content' | 'category' | 'source'>) => invoke<KnowledgeMemory>('save_knowledge', { workspaceId, memory }),
  prepareContext: (workspaceId: string, query: string) => invoke<PreparedContext>('prepare_context', { workspaceId, query }),
  listWorkspaces: () => invoke<{ workspaces: Workspace[] }>('list_workspaces'),
  saveWorkspace: (workspace: Partial<Workspace> & Pick<Workspace, 'name' | 'project_path' | 'default_model_id'>) => invoke<Workspace>('save_workspace', { workspace }),
  listWorkspaceRuns: (workspaceId: string) => invoke<{ runs: WorkspaceRun[] }>('list_workspace_runs', { workspaceId }),
  submitWorkspaceRun: (workspaceId: string, kind: WorkspaceRun['kind'], title: string, input: unknown) => invoke<WorkspaceRun>('submit_workspace_run', { workspaceId, kind, title, input }),
  getWorkspaceRun: (id: string) => invoke<WorkspaceRun>('get_workspace_run', { id }),
  getRunEvents: (id: string, after = 0) => invoke<{ events: RunEvent[] }>('get_run_events', { id, after }),
  cancelWorkspaceRun: (id: string) => invoke<WorkspaceRun>('cancel_workspace_run', { id }),
  // services
  checkHealth: () => invoke<HealthResponse>("check_health"),
  getBackendStatus: () => invoke<BackendStatus>("get_backend_status"),
  startBackend: () => invoke<{ status: string }>("start_backend"),

  // gateway
  getModels: () => invoke<ModelsResponse>("get_models"),
  selectModel: (selection: ModelSelection) => invoke<ModelSelectionResult>('select_model', { selection }),
  getModelSelection: (workspaceId: string, sessionId: string) => invoke<ModelSelectionResult>('get_model_selection', { workspaceId, sessionId }),
  testModel: (id: string) => invoke<{ status: string; latency_ms?: number; error?: string }>("test_model", { id }),
  addModel: (model: ModelConfig) => invoke("add_model", { model }),
  deleteModel: (id: string) => invoke("delete_model", { id }),
  chat: (
    messages: ChatMessageParam[],
    modelId?: string,
    project?: string,
    systemPrompt?: string
  ) =>
    invoke<ChatResponse>("gateway_chat", {
      messages,
      modelId: modelId ?? null,
      project: project ?? null,
      systemPrompt: systemPrompt ?? null,
    }),
  gatewayStats: () => invoke("gateway_stats"),

  // nebula
  listSessions: () => invoke<SessionsResponse>("list_sessions"),
  createSession: (name: string) => invoke<CreateSessionResponse>("create_session", { name }),
  storeMemory: (
    sessionId: string,
    content: string,
    memoryType?: string,
    tags?: string[],
    metadata?: Record<string, string>
  ) =>
    invoke("store_memory", {
      sessionId,
      content,
      memoryType: memoryType ?? null,
      tags: tags ?? null,
      metadata: metadata ?? null,
    }),
  searchMemories: (sessionId: string, query: string, topK?: number) =>
    invoke<SearchResponse>("search_memories", {
      sessionId,
      query,
      topK: topK ?? null,
    }),
  nebulaStats: () => invoke("nebula_stats"),

  // devops
  devopsStatus: () => invoke("get_status"),
  metrics: () => invoke<SystemMetrics>("get_metrics"),
  diagnose: (issue: string) => invoke("diagnose", { issue }),
  executeTool: (name: string, args?: Record<string, unknown>) =>
    invoke("execute_tool", { name, args: args ?? null }),
  listTools: () => invoke("list_tools"),
  devopsLoop: (requestId: string, issue: string) => invoke<{answer:string; trace: {action:string;thought:string;result?:{success:boolean;output:string;error?:string}}[]}>("devops_loop", {requestId, issue}),

  // codelens
  codelensIndex: (projectPath: string, exclude?: string[]) =>
    invoke("codelens_index", { projectPath, exclude: exclude ?? null }),
  codelensSearch: (projectPath: string, query: string) =>
    invoke("codelens_search", { projectPath, query }),
  codelensAnalyze: (projectPath: string, file: string) =>
    invoke("codelens_analyze", { projectPath, file }),
  codelensHealth: () => invoke("codelens_health"),

  // 流式对话: Channel 直接传递结构化 StreamChunk
  chatStream: (
    requestId: string,
    messages: ChatMessageParam[],
    onEvent: Channel<StreamChunk>,
    modelId?: string,
    workspaceId?: string,
    sessionId?: string
  ) => invoke("gateway_chat_stream", { requestId, messages, onEvent, modelId: modelId ?? null, workspaceId: workspaceId ?? null, sessionId: sessionId ?? null }),
  cancelChat: (requestId: string) => invoke("gateway_chat_cancel", { requestId }),
};

export type { Channel };
