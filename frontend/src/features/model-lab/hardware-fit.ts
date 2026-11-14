import type { ModelRevision } from '@/lib/model-engineering-types';

export const GIB = 1024 ** 3;

export function contextCacheEstimate(model: ModelRevision, context: number, cacheKiB?: number): number | null {
  if (cacheKiB == null || !Number.isFinite(cacheKiB) || cacheKiB < 0.25 || cacheKiB > 4096
    || !Number.isInteger(context) || context < 512 || context > 131072 || !model.size_bytes) return null;
  return Math.ceil(context * cacheKiB * 1024);
}

export function estimateModelFit(input: {
  model: ModelRevision;
  context: number;
  cacheKiB?: number;
  capacity?: number | null;
  reserveGiB: number;
}) {
  const { model, context, cacheKiB, capacity, reserveGiB } = input;
  const weights = model.size_bytes;
  if (typeof weights !== 'number' || !Number.isSafeInteger(weights) || weights <= 0) return { kind: 'unknown' as const };
  const kv = contextCacheEstimate(model, context, cacheKiB);
  const estimated = kv == null ? null : weights + kv + reserveGiB * GIB;
  const available = capacity && Number.isFinite(capacity) && capacity > 0 ? capacity : null;
  if (available == null) return { kind: 'unknown' as const, weights, kv, estimated };
  if (weights >= available) return { kind: 'over' as const, weights, kv, estimated };
  if (estimated == null) return { kind: 'partial' as const, weights, kv, estimated, weightHeadroom: available - weights };
  const headroom = available - estimated;
  return { kind: headroom < 0 ? 'over' as const : headroom < 0.75 * GIB ? 'tight' as const : 'trial' as const, weights, kv, estimated, headroom };
}

export function engineAdvice(model: ModelRevision, adapter: string, target: 'gpu' | 'cpu'): string {
  if (model.format === 'openvino_ir') {
    return target === 'gpu' && !/intel|arc|iris/i.test(adapter)
      ? '请先验证 OpenVINO GPU 设备，或从 CPU 开始。'
      : `使用 OpenVINO GenAI ${target.toUpperCase()}，并核对服务实际设备。`;
  }
  if (model.provider === 'ollama') {
    return `保留此版本的 Ollama 配置，检查实际 ${target === 'gpu' ? '显卡卸载比例' : 'CPU 运行状态'}。`;
  }
  if (model.format === 'gguf') {
    if (target === 'cpu') return '使用支持此量化格式的 llama.cpp CPU 构建。';
    if (/intel|arc|iris/i.test(adapter)) return '优先验证匹配该量化的 llama.cpp SYCL 或 Vulkan 构建。';
    if (/nvidia|geforce/i.test(adapter)) return '优先验证匹配该量化的 llama.cpp CUDA 构建。';
    if (/amd|radeon/i.test(adapter)) return '优先验证匹配该量化的 llama.cpp Vulkan 或 ROCm 构建。';
  }
  return '先核对模型格式、引擎和设备的兼容性。';
}
