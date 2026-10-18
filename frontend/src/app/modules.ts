/** Single source of truth for the real Noosphere workspaces. */
export const WORKSPACE_MODULES = [
  { id: 'assistant', number: '01', name: 'AI 对话', english: 'CONVERSATION', to: '/chat', category: '智能终端', description: '连接模型、项目代码与长期记忆，让每一次对话成为可以继续的思考。', capabilities: ['流式对话', '项目上下文', '会话检索'], accent: '#9b754c', service: 'gateway' },
  { id: 'codelens', number: '02', name: 'CodeLens', english: 'CODE INTELLIGENCE', to: '/codelens', category: '代码智能', description: '索引项目结构，检索源码片段，在真实代码上下文中理解与构建。', capabilities: ['项目索引', '语义检索', '结构分析'], accent: '#52746e', service: 'codelens' },
  { id: 'nebula', number: '03', name: 'Nebula', english: 'MEMORY ARCHIVE', to: '/nebula', category: '记忆档案', description: '整理本机的长期记忆与会话记录，让分散的信息建立持久联系。', capabilities: ['混合检索', '记忆浏览', '会话归档'], accent: '#75718d', service: 'nebula' },
  { id: 'devops', number: '04', name: 'DevOps', english: 'SYSTEM TELEMETRY', to: '/devops', category: '系统观测', description: '观察系统指标与运行环境，通过可追溯的工具记录完成诊断。', capabilities: ['资源监测', '诊断工具', '执行记录'], accent: '#9b713e', service: 'devops' },
  { id: 'gateway', number: '05', name: 'Gateway', english: 'MODEL ACCESS', to: '/gateway', category: '模型网关', description: '将不同模型连接到同一个工作空间，管理访问、路由与调用记录。', capabilities: ['模型配置', '调用统计', '访问日志'], accent: '#6c798b', service: 'gateway' },
] as const;
export type WorkspaceModule = (typeof WORKSPACE_MODULES)[number];
export function nextModuleIndex(index: number, direction: number) {
  return ((index + direction) % WORKSPACE_MODULES.length + WORKSPACE_MODULES.length) % WORKSPACE_MODULES.length;
}
export function searchModules(query: string) {
  const term = query.trim().toLocaleLowerCase();
  return WORKSPACE_MODULES.filter((item) => [item.name, item.english, item.category, item.description, ...item.capabilities].join(' ').toLocaleLowerCase().includes(term));
}
