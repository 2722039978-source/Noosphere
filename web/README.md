<div align="center">

# Noosphere Web

**Noosphere 官网 —— Apple 级设计语言 × 明日方舟终端美学**

Noosphere monorepo 的前端模块（`web/`），展示并实时连接三个后端服务。

`Next.js 14` · `TypeScript` · `Tailwind CSS` · `Framer Motion` · `GSAP` · `Three.js` · `Lenis`

</div>

---

## 快速开始

> 需要 [Node.js 18.17+](https://nodejs.org/)（本项目开发时本机未安装 Node，首次运行如遇类型小问题请提 issue）

```bash
cd web
npm install
npm run dev        # http://localhost:3000
```

官网运行在 **:3000**，与 Noosphere 三服务（8730 / 8740 / 8765）互不冲突。

### 连接真实服务（可选）

页面会自动探测本机 Noosphere 服务：

```bash
cd .. && docker compose up -d      # 在仓库根目录启动全部服务（含本官网 :3000）
```

启动后官网自动切换为真实数据：

| 位置 | 数据来源 |
|------|---------|
| 导航栏 `SYS n/3` | 三服务 `/health` 健康检查（15s 轮询） |
| 遥测面板 CPU / 内存 / 磁盘 | DevOps `GET /api/v1/devops/metrics`（5s 轮询，离线时标注 SIMULATED） |
| 产品矩阵 ONLINE/STANDBY 徽章 | 各服务健康状态，「进入控制台」直达真实端口 |

非本机部署时用环境变量覆盖服务地址：`NEXT_PUBLIC_CODELENS_URL` / `NEXT_PUBLIC_NEBULA_URL` / `NEXT_PUBLIC_DEVOPS_URL`。

---

## 架构

```
src/
├── app/
│   ├── layout.tsx        # 字体（Inter/思源黑体/JetBrains Mono）+ 全局壳
│   ├── page.tsx          # 章节组装
│   ├── globals.css       # 设计系统：玻璃拟态 / HUD 角标 / 扫描线 / 网格
│   └── icon.svg
├── components/
│   ├── providers/SmoothScroll.tsx   # Lenis + GSAP ScrollTrigger 时钟同步
│   ├── layout/           # Navbar（滚动感知玻璃导航）/ Footer
│   ├── hero/             # Hero（电影级开场时序）+ ParticleField（Three.js 粒子核心）
│   ├── three/            # ProductStage（可拖拽 3D 展台：惯性旋转/滚轮缩放/环绕灯光）
│   ├── sections/         # Systems / DataViz / Architecture / Products / Roadmap
│   └── ui/               # GlassCard(3D 倾斜+光斑) / CountUp / RadialGauge / TechCurve / Reveal / CursorGlow
├── hooks/                # useServiceStatus / useLiveMetrics / usePrefersReducedMotion
└── lib/                  # data(全站文案) / services(端口配置) / motion(动画令牌) / utils
```

## 动画系统

| 层 | 技术 | 用途 |
|----|------|------|
| 滚动物理 | Lenis | 全站惯性平滑滚动，与 GSAP 共用一个 ticker |
| 滚动驱动 | GSAP ScrollTrigger | 架构区分层视差、数据流线生长（scrub 跟手） |
| 入场/微交互 | Framer Motion | 逐字显影、模糊消散、卡片 3D 倾斜、弹簧光斑 |
| 三维 | Three.js（原生，无 R3F） | Hero 粒子核心、产品展台 |

统一缓动曲线 `cubic-bezier(0.16, 1, 0.3, 1)`（类 easeOutExpo），全部动画走 transform/opacity 合成层。

## 性能策略

- Three.js 场景 `dynamic(..., { ssr: false })` 按需加载；DPR 钳制 ≤ 2
- IntersectionObserver + visibilitychange：离屏/切后台自动停帧
- 卸载时完整 dispose 几何体/材质/渲染器，无 WebGL 上下文泄漏
- `prefers-reduced-motion`：三维降为静帧，全站动画时长归零
- 字体 `next/font` 自托管 + `display: swap`，零布局偏移

## 设计令牌

| 令牌 | 值 | 用途 |
|------|-----|------|
| `void` | `#050505` | 主背景 |
| `graphite` | `#151515` | 面板 |
| `tech` | `#00A8FF` | 唯一强调色（在 void 上对比度 ≈ 7.8:1，AA 达标） |
| `frost` | `#F5F5F7` | 主文字 |
| `silver` | `#9BA1A6` | 次级文字 |

可视化遵循：单系列不设图例框、状态标识 = 图标 + 文字（不依赖颜色）、数值用文本色而非系列色、隐性网格、hover 十字准线 + tooltip。
