# RHINE LAB · ANALYSIS OS

把《明日方舟》特别映像「莱茵生命：访问」中的终端界面，复刻成可以实际操作的三维网页。由 GPT-6 Astra 协助编写代码，模型通过 Blender MCP 制作。

参考片段为原 PV 的 5–40 秒：[BV1rr4y1b7sz](https://www.bilibili.com/video/BV1rr4y1b7sz/)。应用运行时使用实时三维模型与原生界面动画，不播放原视频作为背景。

## 快速运行

安装 Node.js 22.12+（推荐 24 LTS），在项目目录运行：

```sh
npm ci
npm run dev
```

打开终端显示的本地地址，通常是 `http://127.0.0.1:5173/`。Windows 也可以双击 `启动终端.cmd`，首次运行会安装依赖并打开浏览器。首次安装需要网络，安装完成后可本地运行。

```sh
npm run build
npm run preview
```

生产文件输出到 `dist/`；使用本地 HTTP 服务预览，不要直接双击 `dist/index.html`。

## 交互

- 开场：逐字输入、标志绘制、身份验证、圆环扫描、欢迎转场，以及档案阵列展开和抽取特写。正常启动从白色画面开始。
- 按 Enter / Esc，或点击 ENTER SYSTEM，进入档案阵列。
- 左右方向键切换五类档案，上下方向键切换同列档案；每类 8 份，共 40 份。首尾连续循环，切回某列时保留该列的选择。
- 连续翻阅时编号滚动，标题闪动后收成横条；停下后恢复最终档案标题。
- 点击 ACCESS FILE、文件编号或按 Enter 读取档案；获得净空后可拖动档案盒旋转。返回时先转正再收回。
- `/` 打开检索，可按编号、标题、英文名、科室、负责人和分类查找。
- 详情页支持研究记录、收藏和导出 UTF-8 档案文本。
- 点击「360° 查看文档模型」进入独立查看页：拖动环绕、滚轮缩放、方向键平移、一键复位、拆解六组结构和连续重组。Esc 返回当前档案。
- 收藏与音效、减少动态效果、画质设置保存在当前浏览器中。

## 工程结构

技术栈：TypeScript、Three.js、Vite、Rolling Number。布局以 1920 × 1080 为基准，等比例适应窗口，主要面向桌面和横向屏幕。

| 目录或文件 | 用途 |
| --- | --- |
| `src/` | 开场、三维场景、循环阵列、模型查看器、检索与界面 |
| `public/assets/` | 运行所需 GLB 模型 |
| `public/archives/` | 40 份可下载的扩展演示档案 |
| `public/fonts/` | 小米官方 MiSans 原版 WOFF2、版权与许可 |
| `public/licenses/` | 其他第三方声明 |
| `art/` | Blender 源文件、建模与审阅脚本 |
| `scripts/` | 档案导出和行为检查 |
| `reference/` | 开发用时间轴、光照与动效对照工具 |
| `verification/` | 分阶段验证记录 |
| `DESIGN.md` | 视觉与运动约束 |

默认字体为 MiSans，四份官方字体文件约 19.7 MB，按实际使用加载。三维场景需要支持 WebGL 的现代浏览器。首次加载包括字体与 GLB，加载后交互在本机运行。原片时间轴使用 160 个阵列位置；交互模式使用可循环的可见窗口和外围补位。

## Blender 源文件

- `art/rhine-archive.blend`：档案盒基础模型与审阅灯光。
- `art/archive-assembly.blend`：支持六组拆解的档案盒。
- `art/build_archive.py`、`art/build_assembly.py`：重新生成模型及 GLB。
- `art/setup_studio.py`：资产审阅灯光与相机。

脚本从自身位置确定项目目录，项目移动后无需修改本机绝对路径。可在 Blender 的脚本环境中通过 `runpy.run_path()` 执行相应脚本；通过 Blender MCP 调用时同样使用实际脚本路径。重新生成会更新对应模型输出，通常直接使用包内现有模型即可。

## 开发复核

```sh
node scripts/check-motion.mjs
node scripts/check-loop.mjs
node scripts/check-appearance.mjs
node scripts/check-assembly.mjs
```

常用入口：`/?scene=archive`、`/?scene=detail`、`/?time=28&freeze=1`。原片对照页位于 `/reference/review.html` 和 `/reference/boot-review.html` 等，仅用于开发验证。原始 PV 不随源码包分发；需要运行视频对照工具时，请自行准备对应参考视频。

## 内容与资源说明

本项目是非官方的学习与交互复刻演示，与《明日方舟》及莱茵生命的官方制作方无隶属关系。原 PV、相关名称、标志和设定的权利归其各自权利人所有。参考片中没有展示的研究摘要、日期、记录等为扩展演示内容。

模型为重新制作，实时折射、景深、灯光及细节与原 PV 仍有差异。身份验证画面是演示状态机，不连接真实身份或业务服务。

MiSans 按其附带许可使用，版权与许可保留在 `public/fonts/`，设置页提供署名与许可入口；Rolling Number 的许可位于 `public/licenses/rolling-number.txt`。依赖各自遵循其原有许可。源码公开不改变这些第三方资源的权利。

源码包包含运行代码、模型、Blender 源文件、说明与验证脚本；不包含 `node_modules`、本机缓存、Git 工作目录、原 PV 或视频录制中间文件。
