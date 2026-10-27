import type { ReactNode } from 'react';
import { asModelEngineeringError } from '@/lib/model-engineering-client';
import { Button } from '@/components/ui/button';
import { ModelSectionNav } from './ModelSectionNav';

export function LabShell({ title, subtitle, children }: { title: string; subtitle: string; children: ReactNode }) {
  return <div className="ns-model-archive-scroll flex-1 min-h-0 overflow-y-auto" data-model-archive-page={title}>
    <div className="ns-page-wrap ns-model-lab ns-model-archive-page">
      <header className="ns-model-archive-heading ns-hub-heading" data-model-archive-heading>
        <p className="ns-model-archive-breadcrumb ns-hub-breadcrumb"><img src="/noosphere-mark.png" alt="" /> <span>›</span> 工作台 <span>/</span> MODULE 07 <span>/</span> {title.toUpperCase()}</p>
        <h1>{title}</h1><p>{subtitle}</p>
      </header>
      <ModelSectionNav variant="hub" />{children}
    </div>
  </div>;
}
export function LabError({ error, onRetry }: { error: unknown; onRetry?: () => void }) {
  if (!error) return null;
  const e = asModelEngineeringError(error);
  return <div role="alert" className="ns-lab-error"><strong>{e.code || '连接或操作失败'}</strong><p>{e.message}</p>
    {e.details?.current_revision !== undefined && <p>当前服务端版本：{e.details.current_revision}，请刷新后重新决定。</p>}
    {e.details?.gates?.map((g, i) => <p key={i}>{g.id} · {g.status} · {g.reason}</p>)}
    {onRetry && <Button size="sm" variant="outline" onClick={onRetry}>重试同一请求</Button>}</div>;
}
export function JsonView({ value, title = '查看原始证据' }: { value: unknown; title?: string }) {
  return <details className="ns-lab-details"><summary>{title}</summary><pre>{JSON.stringify(value, null, 2)}</pre></details>;
}
