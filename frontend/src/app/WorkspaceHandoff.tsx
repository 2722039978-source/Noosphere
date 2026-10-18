import { useEffect, useState } from 'react';
import { useLocation } from 'react-router-dom';
import { WORKSPACE_MODULES } from './modules';

export function WorkspaceHandoff() {
  const location = useLocation();
  const [visible, setVisible] = useState(Boolean(location.state?.archiveHandoff));
  const module = WORKSPACE_MODULES.find(item => item.to === location.pathname);
  useEffect(() => {
    setVisible(Boolean(location.state?.archiveHandoff));
    const timer = setTimeout(() => setVisible(false), 650);
    return () => clearTimeout(timer);
  }, [location.key, location.state]);
  if (!visible || matchMedia('(prefers-reduced-motion: reduce)').matches) return null;
  return <div className="ns-dossier-handoff is-arriving" aria-hidden="true"><span>FILE / {module?.number ?? 'OS'}</span><strong>{module?.name ?? '工作区设置'}</strong><small>WORKSPACE / CONNECTED</small></div>;
}
