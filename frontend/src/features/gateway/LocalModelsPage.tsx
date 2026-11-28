import { Layers3 } from 'lucide-react';
import { useNavigate, useSearchParams } from 'react-router-dom';
import { useBackend } from '@/hooks/useBackend';
import { useWorkspaceStore } from '@/stores/workspace-store';
import { LoadingState } from '@/components/ui/feedback';
import { PageHeader } from '@/components/ui/page-header';
import { Surface } from '@/components/ui/surface';
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
    <div className="ns-page-wrap ns-local-model-wrap">
      <PageHeader
        icon={<Layers3 size={20} />}
        title="本地模型"
        subtitle="发现并启动本机推理引擎与模型，连接后即可直接进入对话。"
      />
      <Surface className="ns-local-model-surface">
        {requested && !modelsReady && !modelsError ? <LoadingState label="正在查找所选模型…" /> :
          <ModelConnectionWizard key={workspaceId + ':' + (preferred?.id ?? target)} initialTarget={target} preferred={preferred} standalone onUse={model => {
            if (useWorkspaceStore.getState().activeId !== workspaceId) throw new Error('工作区已切换，请重新选择模型。');
            navigate(modelChatDestination(model));
          }} />}
      </Surface>
    </div>
  </div>;
}
