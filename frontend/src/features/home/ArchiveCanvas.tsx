import { useEffect, useRef, useState } from 'react';
import { ArchiveScene } from './scene/scene';
import { FrameBudget } from './render-budget';

type Props = { selected: number; onSelect: (index: number) => void; highQuality: boolean; inspecting: boolean; onFailure: () => void; onAutoQuality: () => void; adaptive: boolean; onReady?: () => void };

/** Reference scene with a bounded React lifetime and demand-driven rendering. */
export default function ArchiveCanvas(props: Props) {
  const host = useRef<HTMLDivElement>(null);
  const current = useRef(props);
  current.current = props;
  const sceneRef = useRef<ArchiveScene | null>(null);
  const wake = useRef<() => void>(() => {});
  const [loaded, setLoaded] = useState(false);
  useEffect(() => {
    const scene = sceneRef.current;
    if (loaded && scene) {
      scene.select(props.selected);
      scene.setMode(props.inspecting ? 'detail' : 'archive');
      wake.current();
    }
  }, [props.selected, props.inspecting, loaded]);
  useEffect(() => { sceneRef.current?.setQuality(props.highQuality); wake.current(); }, [props.highQuality]);

  useEffect(() => {
    const container = host.current;
    if (!container) return;
    let scene: ArchiveScene;
    try { scene = new ArchiveScene(container); } catch { current.current.onFailure(); return; }
    sceneRef.current = scene;
    let disposed = false, ready = false, frame = 0, until = 0;
    const budget = new FrameBudget();
    const draw = (now: number) => {
      frame = 0;
      if (disposed || document.hidden || !ready) return;
      // Follow display frames while moving; millisecond gates skip irregularly
      // on 75/144 Hz displays. Quality controls resolution, not motion cadence.
      try { scene.update(now / 1000); } catch { current.current.onFailure(); return; }
      if (current.current.adaptive && current.current.highQuality && budget.sample(now)) current.current.onAutoQuality();
      if (now < until) frame = requestAnimationFrame(draw);
    };
    const requestDraw = (duration = 4200) => {
      if (!frame) budget.reset();
      until = Math.max(until, performance.now() + duration);
      if (ready && !disposed && !document.hidden && !frame) frame = requestAnimationFrame(draw);
    };
    wake.current = requestDraw;
    const resize = () => { if (!disposed) { scene.resize(); requestDraw(); } };
    const observer = new ResizeObserver(resize);
    observer.observe(container);
    const visibility = () => { if (document.hidden) { cancelAnimationFrame(frame); frame = 0; budget.reset(); } else requestDraw(); };
    const fail = (event: Event) => { event.preventDefault(); if (!disposed) current.current.onFailure(); };
    scene.renderer.domElement.addEventListener('webglcontextlost', fail);
    const pointerMove = (event: PointerEvent) => requestDraw(event.buttons ? 2200 : 500);
    const pointerUp = () => requestDraw();
    const pointerLeave = () => requestDraw(500);
    container.addEventListener('pointermove', pointerMove, { passive: true });
    container.addEventListener('pointerup', pointerUp, { passive: true });
    container.addEventListener('pointerleave', pointerLeave, { passive: true });
    document.addEventListener('visibilitychange', visibility);
    scene.onSelect = (index) => { current.current.onSelect(index); requestDraw(); };
    scene.setQuality(current.current.highQuality);
    void scene.load().then(async () => {
      if (disposed) return;
      scene.setMode(current.current.inspecting ? 'detail' : 'archive');
      scene.select(current.current.selected);
      await scene.prepare();
      if (disposed) return;
      ready = true;
      setLoaded(true);
      requestDraw();
      current.current.onReady?.();
    }).catch(() => { if (!disposed) current.current.onFailure(); });
    return () => {
      disposed = true; cancelAnimationFrame(frame); observer.disconnect();
      document.removeEventListener('visibilitychange', visibility);
      container.removeEventListener('pointermove', pointerMove);
      container.removeEventListener('pointerup', pointerUp);
      container.removeEventListener('pointerleave', pointerLeave);
      scene.renderer.domElement.removeEventListener('webglcontextlost', fail);
      wake.current = () => {}; sceneRef.current = null; scene.dispose();
    };
  }, []);
  return <div ref={host} className="ns-full-scene" data-loaded={loaded}>{!loaded && <span className="ns-scene-loading" role="status">正在载入三维档案…</span>}</div>;
}
