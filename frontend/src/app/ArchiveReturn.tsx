import { createContext, useCallback, useContext, useEffect, useLayoutEffect, useState, type ReactNode } from 'react';
import { useLocation, type Location } from 'react-router-dom';
import { WORKSPACE_MODULES } from './modules';

type Phase = 'departing' | 'loading' | 'arriving';
type ReturnState = { phase: Phase; sourcePath: string };
const ReturnContext = createContext<{ phase: Phase | 'idle'; sourcePath?: string; ready: () => void }>({ phase: 'idle', ready: () => {} });
export const useArchiveReturn = () => useContext(ReturnContext);

/** Only the optional legacy archive uses a covered return animation.
 * Overview and ordinary workspace navigation always render the current location immediately.
 */
export function ArchiveReturn({ children }: { children: (location: Location) => ReactNode }) {
  const location = useLocation();
  const [displayed, setDisplayed] = useState(location);
  const [transition, setTransition] = useState<ReturnState | null>(null);
  const ready = useCallback(() => setTransition(current => current?.phase === 'loading' ? { ...current, phase: 'arriving' } : current), []);
  useLayoutEffect(() => {
    if (location === displayed) return;
    const toHome = location.pathname === '/archive';
    const fromWorkspace = /^\/(chat|gateway|codelens|nebula|devops|settings)(\/|$)/.test(displayed.pathname);
    if (toHome && fromWorkspace && !matchMedia('(prefers-reduced-motion: reduce)').matches) {
      setTransition({ phase: 'departing', sourcePath: displayed.pathname });
      const timer = setTimeout(() => {
        setTransition({ phase: 'loading', sourcePath: displayed.pathname });
        setDisplayed(location);
      }, 420);
      return () => clearTimeout(timer);
    }
    setTransition(null);
    setDisplayed(location);
  }, [location, displayed]);
  useEffect(() => {
    if (transition?.phase === 'arriving') {
      const timer = setTimeout(() => setTransition(null), 680);
      return () => clearTimeout(timer);
    }
    // A failed/slow GPU must never trap navigation behind the cover.
    if (transition?.phase === 'loading') {
      const timer = setTimeout(ready, 6000);
      return () => clearTimeout(timer);
    }
  }, [transition?.phase, ready]);
  const source = WORKSPACE_MODULES.find(item => transition?.sourcePath.startsWith(item.to));
  return <ReturnContext.Provider value={{ phase: transition?.phase ?? 'idle', sourcePath: transition?.sourcePath, ready }}>
    <div className="ns-return-stage" data-phase={transition?.phase ?? 'idle'} style={{ '--module-accent': source?.accent ?? '#9b754c' } as React.CSSProperties}>
      <div className="ns-return-content" {...(transition ? { inert: '' } : {})}>{children(transition ? displayed : location)}</div>
      {transition && <div className={`ns-dossier-handoff ns-return-handoff is-${transition.phase}`} role="status" aria-live="polite">
        <span>FILE / {source?.number ?? 'OS'} · RETURN TO ARCHIVE</span>
        <strong>{source?.name ?? '工作区设置'}</strong>
        <small>{transition.phase === 'loading' ? '正在恢复工作台…' : '返回工作台'}</small>
        <i className="ns-return-rule" aria-hidden="true" />
      </div>}
    </div>
  </ReturnContext.Provider>;
}
