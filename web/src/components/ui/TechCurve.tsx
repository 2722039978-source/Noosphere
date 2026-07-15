"use client";

import { useCallback, useEffect, useRef, useState } from "react";
import { seededRandom } from "@/lib/utils";

const W = 600;
const H = 200;
const PAD = { top: 16, right: 12, bottom: 24, left: 40 };
const POINTS = 40;

/** Catmull-Rom → 三次贝塞尔，生成平滑路径 */
function smoothPath(pts: Array<[number, number]>): string {
  if (pts.length < 2) return "";
  let d = `M ${pts[0][0]},${pts[0][1]}`;
  for (let i = 0; i < pts.length - 1; i++) {
    const p0 = pts[Math.max(i - 1, 0)];
    const p1 = pts[i];
    const p2 = pts[i + 1];
    const p3 = pts[Math.min(i + 2, pts.length - 1)];
    const c1x = p1[0] + (p2[0] - p0[0]) / 6;
    const c1y = p1[1] + (p2[1] - p0[1]) / 6;
    const c2x = p2[0] - (p3[0] - p1[0]) / 6;
    const c2y = p2[1] - (p3[1] - p1[1]) / 6;
    d += ` C ${c1x},${c1y} ${c2x},${c2y} ${p2[0]},${p2[1]}`;
  }
  return d;
}

/**
 * 实时吞吐曲线 —— 单系列（标题即图例，无需图例框）。
 * 2px 线 + 渐变面积 + 隐性网格；hover 十字准线 + tooltip。
 * 每 1.2s 推入新数据点，模拟终端遥测流。
 */
export function TechCurve({ title, unit }: { title: string; unit: string }) {
  // 确定性初始序列，避免 SSR/CSR 不一致
  const [data, setData] = useState<number[]>(() => {
    const rand = seededRandom(7);
    const arr: number[] = [];
    let v = 62;
    for (let i = 0; i < POINTS; i++) {
      v = Math.max(20, Math.min(95, v + (rand() - 0.5) * 14));
      arr.push(v);
    }
    return arr;
  });
  const [hover, setHover] = useState<{ i: number; x: number; y: number } | null>(null);
  const svgRef = useRef<SVGSVGElement>(null);

  // 数据流推进
  useEffect(() => {
    const timer = setInterval(() => {
      setData((prev) => {
        const last = prev[prev.length - 1];
        const next = Math.max(20, Math.min(95, last + (Math.random() - 0.5) * 12));
        return [...prev.slice(1), next];
      });
    }, 1200);
    return () => clearInterval(timer);
  }, []);

  const innerW = W - PAD.left - PAD.right;
  const innerH = H - PAD.top - PAD.bottom;
  const toXY = useCallback(
    (v: number, i: number): [number, number] => [
      PAD.left + (i / (POINTS - 1)) * innerW,
      PAD.top + (1 - v / 100) * innerH,
    ],
    [innerW, innerH],
  );

  const pts = data.map((v, i) => toXY(v, i));
  const line = smoothPath(pts);
  const area = `${line} L ${PAD.left + innerW},${PAD.top + innerH} L ${PAD.left},${PAD.top + innerH} Z`;

  const onMove = (e: React.PointerEvent<SVGSVGElement>) => {
    const rect = svgRef.current?.getBoundingClientRect();
    if (!rect) return;
    const px = ((e.clientX - rect.left) / rect.width) * W;
    const i = Math.round(((px - PAD.left) / innerW) * (POINTS - 1));
    if (i < 0 || i >= POINTS) return setHover(null);
    const [x, y] = toXY(data[i], i);
    setHover({ i, x, y });
  };

  return (
    <div className="relative">
      <div className="mb-3 flex items-baseline justify-between">
        {/* 单系列：标题即图例 */}
        <span className="text-[13px] font-light tracking-widest text-silver">{title}</span>
        <span className="font-mono text-lg tabular-nums text-frost">
          {data[data.length - 1].toFixed(1)}
          <span className="ml-1 text-[10px] text-silver/50">{unit}</span>
        </span>
      </div>

      <svg
        ref={svgRef}
        viewBox={`0 0 ${W} ${H}`}
        className="w-full touch-none"
        onPointerMove={onMove}
        onPointerLeave={() => setHover(null)}
        role="img"
        aria-label={`${title}实时曲线`}
      >
        <defs>
          <linearGradient id="curve-fill" x1="0" y1="0" x2="0" y2="1">
            <stop offset="0%" stopColor="#FFB347" stopOpacity="0.14" />
            <stop offset="100%" stopColor="#FFB347" stopOpacity="0" />
          </linearGradient>
        </defs>

        {/* 隐性网格 */}
        {[0, 25, 50, 75, 100].map((g) => {
          const y = PAD.top + (1 - g / 100) * innerH;
          return (
            <g key={g}>
              <line
                x1={PAD.left}
                x2={PAD.left + innerW}
                y1={y}
                y2={y}
                stroke="rgba(255,255,255,0.05)"
                strokeWidth="1"
              />
              <text
                x={PAD.left - 8}
                y={y + 3}
                textAnchor="end"
                className="fill-silver/40 font-mono text-[9px]"
              >
                {g}
              </text>
            </g>
          );
        })}

        {/* 面积 + 线（2px，圆角端点）—— 计算吞吐 = 能量流动，琥珀橙 */}
        <path d={area} fill="url(#curve-fill)" />
        <path
          d={line}
          fill="none"
          stroke="#FFB347"
          strokeWidth="2"
          strokeLinecap="round"
          style={{ filter: "drop-shadow(0 0 3px rgba(255,179,71,0.25))" }}
        />

        {/* 十字准线 + 高亮点 */}
        {hover && (
          <g>
            <line
              x1={hover.x}
              x2={hover.x}
              y1={PAD.top}
              y2={PAD.top + innerH}
              stroke="rgba(255,179,71,0.35)"
              strokeWidth="1"
              strokeDasharray="3 3"
            />
            {/* 2px 底面描边使点与线分离 */}
            <circle cx={hover.x} cy={hover.y} r="5" fill="#050505" />
            <circle cx={hover.x} cy={hover.y} r="3.5" fill="#FFB347" />
          </g>
        )}
      </svg>

      {/* Tooltip —— 数值用文本色 */}
      {hover && (
        <div
          className="glass-strong pointer-events-none absolute z-10 rounded-md px-3 py-1.5 font-mono text-[11px] text-frost"
          style={{
            left: `${(hover.x / W) * 100}%`,
            top: `${(hover.y / H) * 100 - 18}%`,
            transform: "translate(-50%, -100%)",
          }}
        >
          {data[hover.i].toFixed(1)} {unit}
        </div>
      )}
    </div>
  );
}
