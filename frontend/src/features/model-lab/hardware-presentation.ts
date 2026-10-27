import type { HardwareInventory } from '@/lib/hardware-types';

export function hardwareCapacity(bytes?: number | null): string {
  return typeof bytes === 'number' && Number.isFinite(bytes) && bytes > 0 ? `${(bytes / 1024 ** 3).toFixed(2)} GiB` : '未知';
}
const record = (v: unknown): v is Record<string, unknown> => !!v && typeof v === 'object' && !Array.isArray(v);
const strings = (v: Record<string, unknown>, keys: string[]) => keys.every(k => typeof v[k] === 'string');
const count = (v: unknown) => typeof v === 'number' && Number.isSafeInteger(v) && v >= 0;
const capacity = (v: unknown) => v === null || (count(v) && Number(v) > 0);
/** Version checks keep legacy or future history inspectable without inventing hardware. */
export function readHardware(v: unknown): HardwareInventory | null {
  if (!record(v) || v.schema_version !== 1 || !strings(v, ['collector_version', 'platform', 'status']) || !['complete', 'partial', 'unavailable', 'unsupported'].includes(String(v.status))) return null;
  for (const key of ['system', 'motherboard']) if (!record(v[key]) || !strings(v[key], ['manufacturer', 'model', 'source'])) return null;
  if (!record(v.os) || !strings(v.os, ['name', 'version', 'build', 'architecture', 'source'])) return null;
  if (!Array.isArray(v.cpus) || !v.cpus.every(c => record(c) && strings(c, ['manufacturer', 'model', 'source']) && ['physical_cores', 'logical_cores', 'max_clock_mhz'].every(k => count(c[k])))) return null;
  if (!record(v.memory) || !capacity(v.memory.installed_bytes) || !strings(v.memory, ['source']) || !Array.isArray(v.memory.modules) || !v.memory.modules.every(m => record(m) && strings(m, ['manufacturer', 'part_number', 'source']) && capacity(m.capacity_bytes) && capacity(m.speed_mts))) return null;
  if (!Array.isArray(v.gpus) || !v.gpus.every(g => record(g) && strings(g, ['model', 'manufacturer', 'driver_version', 'source', 'vram_source']) && capacity(g.total_vram_bytes))) return null;
  if (!Array.isArray(v.system_disks) || !v.system_disks.every(d => record(d) && strings(d, ['model', 'interface_type', 'source']) && capacity(d.capacity_bytes))) return null;
  if (!Array.isArray(v.issues) || !v.issues.every(i => record(i) && strings(i, ['component', 'code', 'message'])) || !Array.isArray(v.limitations) || !v.limitations.every(x => typeof x === 'string')) return null;
  return v as unknown as HardwareInventory;
}
