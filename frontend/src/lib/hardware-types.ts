/** Read-only host-reported facts. Total capacity never attests free memory. */
export interface HardwareInventory {
  schema_version: 1;
  collector_version: string;
  platform: string;
  status: 'complete' | 'partial' | 'unavailable' | 'unsupported';
  system: { manufacturer: string; model: string; source: string; missing_reason?: string };
  motherboard: { manufacturer: string; model: string; source: string; missing_reason?: string };
  os: { name: string; version: string; build: string; architecture: string; source: string; missing_reason?: string };
  cpus: { manufacturer: string; model: string; physical_cores: number; logical_cores: number; max_clock_mhz: number; source: string }[];
  memory: { installed_bytes: number | null; modules: { manufacturer: string; part_number: string; capacity_bytes: number | null; speed_mts: number | null; source: string }[]; source: string; missing_reason?: string };
  gpus: { model: string; manufacturer: string; driver_version: string; total_vram_bytes: number | null; source: string; vram_source: string; vram_missing_reason?: string }[];
  system_disks: { model: string; capacity_bytes: number | null; interface_type: string; source: string }[];
  issues: { component: string; code: string; message: string }[];
  limitations: string[];
}
