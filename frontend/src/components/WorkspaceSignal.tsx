import { memo, useEffect, useState } from 'react';
/** Small compositor-only motion; never keeps the 3D renderer awake. */
export const WorkspaceSignal = memo(function WorkspaceSignal() {
  const [visible, setVisible] = useState(() => !document.hidden);
  useEffect(() => {
    const update = () => setVisible(!document.hidden);
    document.addEventListener('visibilitychange', update);
    return () => document.removeEventListener('visibilitychange', update);
  }, []);
  return <div className="ns-workspace-signal" data-paused={!visible} aria-hidden="true"><i /><i /><span /><b /></div>;
});
