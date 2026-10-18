import { useEffect, useState } from "react";
import { Cpu, Plug, Plus, Trash2, Zap } from "lucide-react";
import { Link } from 'react-router-dom';
import { useBackend } from "@/hooks/useBackend";
import { Button } from "@/components/ui/button";
import { Surface } from "@/components/ui/surface";
import { PageHeader } from "@/components/ui/page-header";
import { Badge } from "@/components/ui/badge";
import { EmptyState } from "@/components/ui/feedback";
import { ModelForm } from "./ModelForm";
import { api, errMessage, type ModelConfig } from "@/lib/api";
import { cn } from "@/lib/utils";


/** Provider → 模块色 */
function providerColor(p: string): string {
  if (p.includes("claude") || p.includes("anthropic")) return "text-acc-devops";
  if (p.includes("gemini") || p.includes("google")) return "text-acc-codelens";
  if (p.includes("ollama")) return "text-acc-nebula";
  return "text-acc-gateway";
}

export function GatewayPage() {
  const backend = useBackend();
  const { models, deleteModel } = backend;
  const [actionError, setActionError] = useState("");
  const [deleting, setDeleting] = useState<string | null>(null);
  const remove = async (id: string) => { if (deleting) return; setDeleting(id); setActionError(""); try { await deleteModel(id); } catch (error) { setActionError(errMessage(error)); } finally { setDeleting(null); } };
  const [showForm, setShowForm] = useState(false);
  const [editing, setEditing] = useState<ModelConfig>();
  const [testing, setTesting] = useState<string | null>(null);
  const [switching, setSwitching] = useState(false);
  const [checks, setChecks] = useState<Record<string, { ok: boolean; message: string }>>({});
  async function test(id: string) {
    if (testing) return;
    setTesting(id);
    try {
      const result = await api.testModel(id);
      setChecks(old => ({ ...old, [id]: { ok: result.status === 'ok', message: result.status === 'ok' ? `连接成功 · ${Math.round(result.latency_ms ?? 0)} ms` : connectionError(result.error || '未返回有效回复') } }));
    } catch (error) { setChecks(old => ({ ...old, [id]: { ok: false, message: connectionError(errMessage(error)) } })); }
    finally { setTesting(null); }
  }
  async function activate(model: ModelConfig) {
    if (switching) return;
    setSwitching(true); setActionError('');
    try { await backend.addModel({ ...model, api_key: '', is_default: true }); }
    catch (error) { setActionError(errMessage(error)); }
    finally { setSwitching(false); }
  }

  useEffect(() => { void backend.loadModels(); }, [backend.loadModels]);


  return (
    <div className="flex-1 overflow-y-auto">
      <div className="ns-page-wrap">
        <PageHeader
          icon={<Zap size={20} />}
          accent="var(--acc-gateway)"
          title="Gateway"
          subtitle={`模型管理中心 · 大模型统一接入与配置（已接入 ${models.length} 个模型）`}
          actions={
            <Button className="gap-1.5" aria-expanded={showForm} onClick={() => setShowForm((v) => !v)}>
              <Plus size={14} /> {showForm ? "收起" : "添加模型"}
            </Button>
          }
        />

        <div className="ns-task-path"><strong>{models.length} 个模型配置</strong><span>/</span><span>默认模型：{models.find(model => model.is_default)?.name ?? '尚未设置'}</span><span className="ml-auto">配置接入 → 返回 AI 对话使用</span></div>
        {actionError && <p role="alert" className="ns-form-error">{actionError}</p>}
        {(showForm || editing) && <ModelForm key={editing?.id ?? 'new'} initial={editing} onSaved={() => { if (editing) setChecks(old => { const next = { ...old }; delete next[editing.id]; return next; }); setShowForm(false); setEditing(undefined); }} onCancel={() => { setShowForm(false); setEditing(undefined); }} />}

        {models.length === 0 ? (
          <EmptyState
            icon={<Zap size={20} className="text-acc-gateway" />}
            title="尚未配置模型"
            description="支持 OpenAI 兼容接口、Claude、Gemini 与 Ollama，配置后即可启用 AI 对话。"
            action={
              <Button className="gap-1.5" onClick={() => setShowForm(true)}>
                <Plus size={14} /> 添加第一个模型
              </Button>
            }
          />
        ) : (
          <div className="grid grid-cols-[repeat(auto-fill,minmax(min(100%,300px),1fr))] gap-4">
            {models.map((m) => {
              const color = providerColor(m.provider);
              return (
                <Surface key={m.id} className="ns-lift px-5 py-5">
                  <div className="flex items-start gap-3">
                    <span className={cn("flex size-10 shrink-0 items-center justify-center rounded-none bg-secondary", color)}>
                      <Cpu size={17} />
                    </span>
                    <div className="min-w-0 flex-1">
                      <div className="flex items-center gap-2 text-[14px] font-medium">
                        <span className="truncate text-text-1">{m.name}</span>
                        {m.is_default && <Badge className="shrink-0 text-xs">默认</Badge>}
                      </div>
                      <div className="mt-1 font-mono text-xs text-text-3">{m.provider}</div>
                    </div>
                    <button
                      onClick={() => void remove(m.id)}
                      disabled={deleting !== null}
                      aria-label={`删除模型 ${m.name}`}
                      title="删除模型"
                      className="cursor-pointer p-1 text-text-3 hover:text-err"
                    >
                      <Trash2 size={15} />
                    </button>
                  </div>

                  <div className="mt-4 flex flex-col gap-1.5 font-mono text-xs text-text-3">
                    <div className="flex items-center gap-2">
                      <Plug size={12} className="shrink-0" />
                      <span className="truncate">{m.base_url}</span>
                    </div>
                    <div>模型：{m.model_name || "—"}</div>
                  </div>

                  <div className="mt-4 flex items-center gap-2">
                    <Badge variant={m.enabled === false ? "secondary" : "ok"} className="text-xs">{m.enabled === false ? "已停用" : "已配置"}</Badge>
                    <span className="text-xs text-text-3">支持流式输出</span>
                  </div>
                  <div className="ns-provider-actions"><Button variant="outline" size="sm" disabled={testing !== null} onClick={() => void test(m.id)}>{testing === m.id ? '连接测试中…' : '测试连接'}</Button><Button variant="ghost" size="sm" onClick={() => { setShowForm(false); setEditing(m); }}>编辑配置</Button>{!m.is_default && <Button variant="ghost" size="sm" disabled={switching} onClick={() => void activate(m)}>设为默认</Button>}{m.is_default && <Button asChild variant="ghost" size="sm"><Link to="/chat">开始对话</Link></Button>}</div>
                  {checks[m.id] && <p role={checks[m.id].ok ? 'status' : 'alert'} className={checks[m.id].ok ? 'ns-result-status' : 'ns-form-error'}>{checks[m.id].message}</p>}
                </Surface>
              );
            })}
          </div>
        )}
      </div>
    </div>
  );
}
function connectionError(message: string) {
  if (/401|403/.test(message)) return '密钥无效或无访问权限，请编辑配置更新密钥后重试。';
  if (/402|insufficient.*balance/i.test(message)) return '账户余额不足，请充值后重新测试。';
  if (/429/.test(message)) return '请求过于频繁，请稍后重试。';
  if (/404/.test(message)) return '接口或模型不存在，请检查地址与模型名称。';
  return `连接失败：${message}`;
}
