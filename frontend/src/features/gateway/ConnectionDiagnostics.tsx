import type { ModelDiagnostic } from '@/lib/api';

const stageLabel: Record<string, string> = { configuration: '配置', service: '服务连接', authentication: '鉴权', enumeration: '模型枚举', loading: '模型加载', inference: '真实推理', ready: '就绪' };
const service = { online: '在线', offline: '离线', unknown: '待确认' };
const discovery = { found: '已发现', missing: '未发现', unknown: '待确认' };
const loaded = { loaded: '已加载', not_loaded: '未加载', unknown: '待确认' };
const inference = { succeeded: '成功', failed: '失败', skipped: '待验证' };

export function ConnectionDiagnostics({ result }: { result: ModelDiagnostic }) {
  return <div className="space-y-2 text-xs text-text-2" aria-label="连接诊断">
    <p role={result.status === 'error' ? 'alert' : 'status'} className={result.status === 'error' ? 'text-err' : 'text-ok'}>{result.status === 'ok' ? result.inference === 'succeeded' ? '真实推理验证通过' : '连接检查通过' : `${stageLabel[result.stage] ?? '连接检查'}：${result.error || result.code || '失败'}`}</p>
    {result.temperature_omitted && <p>验证使用服务端默认采样设置，未发送温度参数。</p>}
    {(result.service || result.model_discovery) && <dl className="grid grid-cols-2 gap-x-4 gap-y-2 sm:grid-cols-4">
      <div><dt className="text-text-3">服务</dt><dd>{service[result.service] ?? '待确认'}</dd></div>
      <div><dt className="text-text-3">模型</dt><dd>{discovery[result.model_discovery] ?? '待确认'}</dd></div>
      <div><dt className="text-text-3">加载</dt><dd>{loaded[result.model_loaded] ?? '待确认'}</dd></div>
      <div><dt className="text-text-3">推理</dt><dd>{inference[result.inference] ?? '待验证'}</dd></div>
    </dl>}
    {result.endpoint && <details className="ns-record-detail"><summary>请求详情</summary><div className="ns-detail-body space-y-2 break-all">
      <p>{result.provider} · {result.model || '模型待选'}</p>{result.reported_model && <p>服务返回模型 {result.reported_model}</p>}<p>{result.endpoint}</p>
      <ul className="space-y-1">{result.checks?.map((item, index) => <li key={index}>{stageLabel[item.stage] ?? item.stage} · {item.http_status ? `HTTP ${item.http_status}` : item.code || item.status} · {item.endpoint}</li>)}</ul>
    </div></details>}
  </div>;
}
