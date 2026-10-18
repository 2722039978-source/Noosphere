// Route chunks start loading while the selected archive travels into the workspace.
export const workspaceLoaders = {
  '/chat': () => import('@/features/chat/ChatPage'),
  '/gateway': () => import('@/features/gateway/GatewayPage'),
  '/codelens': () => import('@/features/codelens/CodeLensPage'),
  '/nebula': () => import('@/features/nebula/NebulaPage'),
  '/devops': () => import('@/features/devops/DevOpsPage'),
  '/settings': () => import('@/features/settings/SettingsPage'),
};
export function preloadWorkspace(path: string) {
  const load = workspaceLoaders[path as keyof typeof workspaceLoaders];
  return load?.().catch(() => undefined);
}
