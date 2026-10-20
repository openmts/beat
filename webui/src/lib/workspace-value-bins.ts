import type { HeatmapScale } from "@/types/metric-workspace"

/** 分桶刻度：linear 等分区间；log 把边界按等比数列排布，因此低取值区分得更细。 */
export type ValueBinScale = HeatmapScale

export interface ValueBoundsOptions {
  min?: number
  max?: number
  buckets: number
  scale: ValueBinScale
}

/**
 * valueBounds 生成分桶边界（长度 = buckets + 1）。热力图与直方图共用同一套取值分桶，
 * 保证同一批样本在两种面板里落到完全一致的区间。对数刻度要求下界大于零，否则退回线性，
 * 而不是产生无法解释的负边界。
 */
export function valueBounds(values: readonly number[], options: ValueBoundsOptions): number[] {
  const finite = values.filter((value) => Number.isFinite(value))
  const lower = options.min ?? (finite.length ? Math.min(...finite) : 0)
  const upper = options.max ?? (finite.length ? Math.max(...finite) : lower + 1)
  // 全部样本相同时上下界相等，给一个单位区间，否则除零会得到全 0 或 NaN 的边界。
  const max = upper > lower ? upper : lower + 1
  const buckets = Math.max(1, options.buckets)
  if (options.scale === "log" && lower > 0) {
    const ratio = max / lower
    return Array.from({ length: buckets + 1 }, (_, index) => lower * ratio ** (index / buckets))
  }
  return Array.from({ length: buckets + 1 }, (_, index) => lower + ((max - lower) * index) / buckets)
}

/** valueBinIndex 返回取值所属的分桶下标；等于上界的样本归入最后一桶，避免被丢掉。 */
export function valueBinIndex(bounds: readonly number[], value: number): number {
  if (!bounds.length) return -1
  for (let index = 0; index < bounds.length - 1; index += 1) if (value < bounds[index + 1]) return index
  return bounds.length - 2
}
