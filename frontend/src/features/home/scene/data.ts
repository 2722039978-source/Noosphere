import { WORKSPACE_MODULES } from '@/app/modules';
// The same five real modules repeat in the visual array; no demo archive data.
export const records = WORKSPACE_MODULES;
export const archiveColumns = ['代码智能', '记忆档案', '智能终端', '系统观测', '模型网关'];
const indices = [1, 2, 0, 3, 4];
export function columnFiles(lane: number) { return [indices[((lane % 5) + 5) % 5]]; }
export function fileLocation(index: number) { const lane = indices.indexOf(index); return { lane, row: 12, slot: lane * 32 + 12 }; }
export function fileAtSlot(slot: number) { return columnFiles(Math.floor(slot / 32))[0]; }
