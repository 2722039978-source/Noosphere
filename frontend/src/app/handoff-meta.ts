import { WORKSPACE_MODULES, type WorkspaceModule } from './modules';
import { modelSection } from './model-navigation';

export interface HandoffDescriptor {
  path: string;
  number: string;
  title: string;
  english: string;
  accent: string;
}

export const MODULE_PRESENTATION: Record<WorkspaceModule['id'], { title: string; subtitle: string }> = {
  assistant: { title: '对话与任务', subtitle: 'CONVERSATION' },
  codelens: { title: '代码分析', subtitle: 'CODE INTELLIGENCE' },
  nebula: { title: '项目记忆', subtitle: 'MEMORY ARCHIVE' },
  devops: { title: '系统诊断', subtitle: 'SYSTEM TELEMETRY' },
  gateway: { title: '本地模型', subtitle: 'MODEL ARCHIVE' },
};

export function describeRouteHandoff(pathname: string): HandoffDescriptor {
  if (pathname === '/settings') {
    return {
      path: '/settings',
      number: '06',
      title: '应用设置',
      english: 'PREFERENCES',
      accent: '#7b6a58',
    };
  }
  if (pathname === '/model-check' || pathname.startsWith('/model-lab') || pathname.startsWith('/evaluation-reports') || pathname.startsWith('/release-center')) {
    return {
      path: pathname,
      number: '05',
      title: '模型测评',
      english: 'MODEL EVALUATION',
      accent: '#586d84',
    };
  }
  const normalized = modelSection(pathname) ? '/model-hub' : pathname;
  const mod = WORKSPACE_MODULES.find(item => normalized === item.to || normalized.startsWith(item.to + '/'));
  if (mod) {
    const pres = MODULE_PRESENTATION[mod.id];
    return {
      path: mod.to,
      number: mod.number,
      title: pres.title,
      english: pres.subtitle,
      accent: mod.accent,
    };
  }
  return {
    path: '/chat',
    number: '01',
    title: '对话与任务',
    english: 'CONVERSATION',
    accent: '#8f5e34',
  };
}

export function routeAccent(pathname: string): string {
  return describeRouteHandoff(pathname).accent;
}
