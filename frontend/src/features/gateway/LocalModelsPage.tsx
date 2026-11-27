import { useNavigate, useSearchParams } from 'react-router-dom';
import { useBackend } from '@/hooks/useBackend';
import { useWorkspaceStore } from '@/stores/workspace-store';
import { LoadingState } from '@/components/ui/feedback';
import { modelChatDestination } from '@/features/model-lab/hub-presentation';
import { ModelConnectionWizard } from './ModelConnectionWizard';

export function LocalModelsPage() {
  const navigate = useNavigate();
  const [search] = useSearchParams();
  const target = search.get('target') ?? '';
  const requested = search.get('model');
  const { models, modelsReady, modelsError } = useBackend();
  const workspaceId = useWorkspaceStore(state => state.activeId);
  const preferred = requested ? models.find(model => model.id === requested) : undefined;
  return <div className="ns-local-model-page">
    <section className="ns-local-model-main">
      <header>
        <h1>本地模型</h1>
        <p>选择模型，启动后即可对话。</p>
      </header>
      {requested && !modelsReady && !modelsError ? <LoadingState label="正在查找所选模型…" /> :
        <ModelConnectionWizard key={workspaceId + ':' + (preferred?.id ?? target)} initialTarget={target} preferred={preferred} standalone onUse={model => {
          if (useWorkspaceStore.getState().activeId !== workspaceId) throw new Error('工作区已切换，请重新选择模型。');
          navigate(modelChatDestination(model));
        }} />}
    </section>
  </div>;
}
