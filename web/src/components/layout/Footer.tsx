import { SERVICES } from "@/lib/services";

const CREATOR = {
  name: "Ktndeswua",
  login: "2722039978-source",
  url: "https://github.com/2722039978-source",
  avatar: "https://avatars.githubusercontent.com/u/246727781?v=4",
};

export function Footer() {
  return (
    <footer className="relative border-t border-white/[0.06]">
      {/* 危险条纹装饰 — 工业终端签名 */}
      <div className="h-1.5 w-full bg-hazard opacity-40" />

      <div className="mx-auto max-w-7xl px-6 py-14 lg:px-10">
        <div className="flex flex-col justify-between gap-10 md:flex-row md:items-end">
          <div>
            <p className="font-sans text-lg font-light tracking-[0.45em] text-frost">
              NOOSPHERE
            </p>
            <p className="mt-3 max-w-md text-[13px] font-light leading-relaxed text-silver">
              项目认知基础设施 · 不是给 AI 一个更大的书架，
              而是给它一份带目录、索引与批注的精装版。
            </p>
          </div>

          {/* 制作人 —— 信息来自 GitHub */}
          <div>
            <p className="hud-label mb-3">CREATOR // 制作人</p>
            <a
              href={CREATOR.url}
              target="_blank"
              rel="noreferrer"
              className="group flex items-center gap-3"
            >
              {/* eslint-disable-next-line @next/next/no-img-element -- 远端头像，避免 next/image 域名白名单配置 */}
              <img
                src={CREATOR.avatar}
                alt={`${CREATOR.name} 的 GitHub 头像`}
                width={36}
                height={36}
                loading="lazy"
                className="h-9 w-9 rounded-full border border-white/[0.12] transition-colors duration-300 group-hover:border-tech/60"
              />
              <span className="leading-tight">
                <span className="block text-[13px] font-light tracking-widest text-frost transition-colors duration-300 group-hover:text-tech">
                  {CREATOR.name}
                </span>
                <span className="block font-mono text-[10px] tracking-[0.15em] text-silver/60">
                  github.com/{CREATOR.login}
                </span>
              </span>
            </a>
          </div>

          {/* 服务终端一览 —— 真实端口，点击直达本机控制台 */}
          <div className="font-mono text-[11px] leading-6 text-silver/70">
            {SERVICES.map((s) => (
              <a
                key={s.id}
                href={s.console}
                target="_blank"
                rel="noreferrer"
                className="block transition-colors duration-300 hover:text-tech"
              >
                <span className="text-silver/50">▸</span> {s.codename} · {s.name} ·
                localhost:{s.port}
              </a>
            ))}
          </div>
        </div>

        <div className="mt-12 flex flex-col items-start justify-between gap-3 border-t border-white/[0.05] pt-6 font-mono text-[10px] tracking-[0.25em] text-silver/40 md:flex-row md:items-center">
          <span>© 2026 NOOSPHERE CONTRIBUTORS · MIT LICENSE</span>
          <span>
            CRAFTED BY{" "}
            <a
              href={CREATOR.url}
              target="_blank"
              rel="noreferrer"
              className="text-silver/60 transition-colors duration-300 hover:text-tech"
            >
              {CREATOR.name.toUpperCase()}
            </a>{" "}
            · RHODES-CLASS TERMINAL · BUILD 0.1.0
          </span>
        </div>
      </div>
    </footer>
  );
}
