import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { api, errMessage } from '@/lib/api';
import { Button } from '@/components/ui/button';
import { Surface } from '@/components/ui/surface';

export function AgentToolpackPanel({ online }: { online: boolean }) {
  const cache = useQueryClient();
  const key = ['agent-toolpack'];
  const state = useQuery({ queryKey: key, queryFn: () => api.agentToolpack(), enabled: online });
  const install = useMutation({ mutationFn: () => api.agentToolpack(true),
    onSettled: () => cache.invalidateQueries({ queryKey: key }) });
  const renderKey = ['agent-render-packs'];
  const render = useQuery({ queryKey: renderKey, enabled: online, queryFn: () => api.agentRenderPacks() });
  const addRenderer = useMutation({ mutationFn: (kind: 'html' | 'office') => api.agentRenderPacks(kind), onSettled: () => cache.invalidateQueries({queryKey: renderKey}) });
  const pack = state.data;
  const officePreview = render.data?.packs?.find(p => p.kind === 'office')?.description;
  return <Surface className="flex flex-wrap items-center gap-3 px-6 py-5">
    <div className="min-w-0 flex-1 basis-48">
      <h2 className="text-[15px] font-semibold text-text-1">Agent 工具包</h2>
      <p className="mt-1 text-[13px] text-text-2">{pack?.description || '安装工作区任务使用的受控生成工具。'}</p>
      {pack && <p className="mt-1 text-xs text-text-3">{pack.version} · {pack.installed ? '已安装' : pack.available ? '可安装' : '当前平台暂不可用'}
        {pack.size_bytes > 0 && ' · ' + (pack.size_bytes / 1048576).toFixed(1) + ' MiB'}</p>}
      {pack?.formats?.length ? <p className="mt-1 text-xs text-text-3">{pack.formats.join(' · ')}</p> : null}
      {pack?.renderer && <p className="mt-1 text-xs text-text-3">{pack.renderer}</p>}
      {(state.error || install.error) && <p role="alert" className="text-xs text-err">{errMessage(state.error || install.error)}</p>}
    </div>
    {pack?.available && !pack.installed && <Button size="sm" variant="outline" disabled={install.isPending || !online} onClick={() => install.mutate()}>
      {install.isPending ? '正在安装…' : '安装官方工具包'}</Button>}
    {state.isError && <Button size="sm" variant="outline" onClick={() => void state.refetch()}>重试读取</Button>}
    <div className="w-full space-y-2 text-xs text-text-2">
      <p>PDF 页面使用内置 PDFium 离线渲染。</p>
      {officePreview && <p>{officePreview}</p>}
      {render.data?.packs?.filter(p => p.available).map(p => <div className="flex flex-wrap items-center gap-2" key={p.kind}>
        <span className="flex-1 basis-56">{p.name} {p.version} · {(p.size_bytes / 1048576).toFixed(1)} MiB<br/>{p.description}</span>
        {p.installed ? <span>已安装</span> : <Button size="sm" variant="outline" disabled={addRenderer.isPending} onClick={() => addRenderer.mutate(p.kind)}>{addRenderer.isPending && addRenderer.variables === p.kind ? '正在下载安装…' : '下载并安装'}</Button>}
      </div>)}
      {render.isError && <Button size="sm" variant="outline" onClick={() => void render.refetch()}>重试读取排版器</Button>}
      {addRenderer.error && <p role="alert">{errMessage(addRenderer.error)}</p>}
    </div>
  </Surface>;
}
