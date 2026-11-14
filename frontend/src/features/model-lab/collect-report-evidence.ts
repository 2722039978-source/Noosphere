import type { ModelEngineeringRequest, ModelEngineeringResponses } from '@/lib/model-engineering-types';
import type { CollectedCase } from './experiment-bundle';

type CaseRequest = Extract<ModelEngineeringRequest, { operation: 'get_case' }>;
/** Collect every frozen case, irrespective of the visible report filter. */
export async function collectReportEvidence(ids: string[], scope: { workspace: string; evaluationID: string; reportID: string }, read: (request: CaseRequest) => Promise<ModelEngineeringResponses['get_case']>, signal: AbortSignal, onProgress: (completed: number) => void): Promise<CollectedCase[]> {
  const rows: CollectedCase[] = new Array(ids.length); let index = 0, completed = 0;
  const worker = async () => {
    while (!signal.aborted) {
      const position = index++; if (position >= ids.length) return;
      const id = ids[position];
      try {
        const detail = await read({ operation: 'get_case', workspace_id: scope.workspace, evaluation_id: scope.evaluationID, case_id: id, report_id: scope.reportID, limit: 100 });
        // Report-bound reads return the complete selected attempt set, not a page of later retries.
        rows[position] = detail.case.id !== id ? { id, detail: null, issue: '读取的案例与冻结目录不一致。' } : { id, detail, issue: detail.next_cursor ? '服务端返回了未读分页；此包标记为部分证据，未用后续重试补齐。' : undefined };
      } catch { rows[position] = { id, detail: null, issue: '本案例详细证据读取失败，请在应用中重试。' }; }
      if (!signal.aborted) onProgress(++completed);
    }
  };
  await Promise.all([worker(), worker()]);
  if (signal.aborted) throw new DOMException('已取消导出', 'AbortError');
  return rows;
}
