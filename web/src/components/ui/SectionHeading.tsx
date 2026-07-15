import { Reveal, RevealItem } from "./Reveal";

/**
 * 区块标题 —— Apple 式排版层级：
 * HUD 小标签（战术编号）→ 超大极细主标题 → 银灰副标题
 */
export function SectionHeading({
  index,
  label,
  title,
  subtitle,
}: {
  index: string;
  label: string;
  title: string;
  subtitle?: string;
}) {
  return (
    <Reveal className="mb-16 md:mb-24">
      <RevealItem>
        <div className="mb-6 flex items-center gap-4">
          <span className="hud-label">{index}</span>
          <span className="h-px w-12 bg-white/20" />
          <span className="hud-label">{label}</span>
        </div>
      </RevealItem>
      <RevealItem>
        <h2 className="max-w-3xl text-4xl font-thin leading-[1.15] tracking-wide text-frost md:text-6xl">
          {title}
        </h2>
      </RevealItem>
      {subtitle && (
        <RevealItem>
          <p className="mt-6 max-w-xl text-base font-light leading-relaxed tracking-wide text-silver md:text-lg">
            {subtitle}
          </p>
        </RevealItem>
      )}
    </Reveal>
  );
}
