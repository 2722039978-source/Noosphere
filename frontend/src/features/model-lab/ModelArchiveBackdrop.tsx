import { memo } from 'react';
import './model-archive-background.css';

/** The image belongs to the workspace, so model tab navigation retains its DOM and decoded bitmap. */
export const ModelArchiveBackdrop = memo(function ModelArchiveBackdrop({ active }: { active: boolean }) {
  return <div
    className="ns-model-archive-backdrop"
    aria-hidden="true"
    data-testid="model-archive-backdrop"
    data-model-archive-backdrop
    data-model-archive-active={active}
    data-renderer="static-image"
    hidden={!active}
  >
    <img
      src="/assets/model-archive-background.webp"
      className="ns-model-archive-backdrop-image"
      alt=""
      decoding="async"
      loading="eager"
      draggable={false}
      data-testid="model-archive-backdrop-image"
      data-model-archive-image
    />
  </div>;
});
