import { memo } from 'react';
import '@/styles/workspace-art.css';

/** Retained with the workspace shell; route changes never recreate the scenery. */
export const WorkspaceBackdrop = memo(function WorkspaceBackdrop({ active }: { active: boolean }) {
  return <div className="ns-workspace-backdrop" data-workspace-backdrop data-renderer="static-image" aria-hidden="true" hidden={!active}>
    <img src="/assets/workspace-architecture.webp" className="ns-workspace-background-image" data-workspace-background-image alt="" decoding="async" draggable={false} />
  </div>;
});
