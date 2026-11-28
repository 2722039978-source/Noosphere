import { useEffect, useState, type CSSProperties } from 'react';
import { useLocation } from 'react-router-dom';
import { describeRouteHandoff, type HandoffDescriptor } from './handoff-meta';

function readHandoff(state: unknown, pathname: string): HandoffDescriptor | null {
  const payload = (state as { archiveHandoff?: boolean | HandoffDescriptor } | null)?.archiveHandoff;
  if (!payload) return null;
  if (typeof payload === 'object' && typeof payload.title === 'string') return payload;
  return describeRouteHandoff(pathname);
}

export function WorkspaceHandoff() {
  const location = useLocation();
  const [handoff, setHandoff] = useState<HandoffDescriptor | null>(() => readHandoff(location.state, location.pathname));

  useEffect(() => {
    const next = readHandoff(location.state, location.pathname);
    setHandoff(next);
    if (!next) return;
    const timer = setTimeout(() => setHandoff(null), 500);
    return () => clearTimeout(timer);
  }, [location.key, location.pathname, location.state]);

  if (!handoff || matchMedia('(prefers-reduced-motion: reduce)').matches) return null;
  return (
    <div
      className="ns-dossier-handoff is-arriving"
      style={{ '--module-accent': handoff.accent } as CSSProperties}
      aria-hidden="true"
    >
      <span className="ns-handoff-kicker"><b>{handoff.number}</b><span>{handoff.english}</span></span>
      <strong>{handoff.title}</strong>
      <i className="ns-handoff-rule" />
    </div>
  );
}
