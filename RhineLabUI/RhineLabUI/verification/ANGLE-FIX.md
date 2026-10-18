# 侧边角度条纹修复

基线：`ca578332ebe35107740e94d2406f208ed4624c03`，标签 `baseline-2026-09-08`。本地完整快照保存在 `backups/render-baseline-20260908-021308`。

标注位置对应 Amber_Lightguide。导光条前表面仅比透明盖板前表面靠前 0.002 世界单位，原有远距离透视镜头在部分角度下无法稳定区分深度，形成斜向条纹。以 -0.8 弧度稳定复现。

仅给 Amber_Lightguide 材质添加 polygonOffset，factor=-1、units=-2。模型 GLB 与基线 SHA256 相同；相机、裁剪面、灯光、AO、颜色、透射与动效均未改变。

验证：

- 实际场景在 -0.8、-0.45、0、+0.8 弧度对照；标注侧条的斜纹消失，螺钉和标签的遮挡正常。
- 关闭 AO 后基线问题仍存在；临时提高近裁剪面能消除问题但改变 AO 层次，因此最终只修正局部材质。
- 切换档案并归位时，当前和旧卡片都保留同一深度偏移，旧卡片仍先转正再下降。
- `node scripts/check-appearance.mjs`、`node scripts/check-motion.mjs`、`npm run build` 通过。

本地复核页面（开发服务）：

- 修复前：`/reference/detail-transition.html?t=5&angle=-0.8&sharp=1&close=1&offset=0`
- 修复后：`/reference/detail-transition.html?t=5&angle=-0.8&sharp=1&close=1`

`sharp` 仅在验证页关闭景深以方便观察，产品仍使用原有景深。
