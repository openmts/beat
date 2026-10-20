import { panelColors } from "@/lib/workspace-chart"
import { valueBinIndex, valueBounds } from "@/lib/workspace-value-bins"
import type { DashboardPanel, HistogramScale, MetricQueryResult, PanelColor, PanelType } from "@/types/metric-workspace"

/** 与服务端 internal/model 同规则：分桶 2..64、计数轴 linear/log、配色取面板调色板、可合并序列。 */
export const histogramScales = ["linear", "log"] as const
export const minHistogramBuckets = 2
export const maxHistogramBuckets = 64
export const defaultHistogramBuckets = 12
export const defaultHistogramScale: HistogramScale = "linear"
export const defaultHistogramColor: PanelColor = "blue"
export const defaultHistogramMerge = false
/** 柱子预算：8 条序列 × 64 桶已经超过 500 根，超过预算时提示而不是继续加宽画布。 */
export const maxHistogramBars = 512
/** 面板类型切换时需要一并清理的直方图字段；其它可视化类型不接受这些字段。 */
export const histogramFieldNames = ["histogram_buckets", "histogram_scale", "histogram_color", "histogram_merge"] as const

/** 直方图的可编辑子集：编辑器与导入导出都只碰这几个字段，导入时类型还是原始字符串。 */
export interface HistogramFields {
  type: string
  /** 面板标题；合并序列时作为唯一分布的图例名。 */
  title?: string
  /** 面板的序列定义，用来把结果序列映射回它自己的颜色。 */
  series?: { ref_id: string; color: PanelColor }[]
  histogram_buckets?: number
  histogram_scale?: string
  histogram_color?: string
  histogram_merge?: boolean
  /** 取值轴上下界；留空表示按样本范围自动取值。 */
  min?: number
  max?: number
}

export interface PanelHistogram { buckets: number; scale: HistogramScale; color: PanelColor; merge: boolean }

export interface HistogramBin { from: number; to: number; count: number }

export interface HistogramDistribution {
  refId: string
  label: string
  color: PanelColor
  unit: string
  /** 每桶的样本数量，下标与其它序列对齐（所有序列共用同一组分桶边界）。 */
  bins: HistogramBin[]
  total: number
}

/** panelHistogram 解析生效值，缺省与服务端一致：12 个分桶、线性刻度、蓝色、不合并序列。 */
export function panelHistogram(panel: HistogramFields): PanelHistogram {
  return {
    buckets: validBuckets(panel.histogram_buckets) ? (panel.histogram_buckets as number) : defaultHistogramBuckets,
    scale: histogramScales.find((value) => value === panel.histogram_scale) ?? defaultHistogramScale,
    color: (Object.keys(panelColors).find((value) => value === panel.histogram_color) ?? defaultHistogramColor) as PanelColor,
    merge: typeof panel.histogram_merge === "boolean" ? panel.histogram_merge : defaultHistogramMerge,
  }
}

/** histogramPatch 切换面板类型时清理字段：只有直方图接受这四项。 */
export function histogramPatch(type: PanelType): Pick<DashboardPanel, "histogram_buckets" | "histogram_scale" | "histogram_color" | "histogram_merge"> {
  if (type === "histogram") return {}
  return { histogram_buckets: undefined, histogram_scale: undefined, histogram_color: undefined, histogram_merge: undefined }
}

/** histogramError 返回目录键，null 表示可以保存；规则与服务端一致。 */
export function histogramError(panel: HistogramFields): string | null {
  const styled = panel.histogram_buckets !== undefined || Boolean(panel.histogram_scale) || Boolean(panel.histogram_color) || panel.histogram_merge !== undefined
  if (styled && panel.type !== "histogram") return "workspace.histogram_histogram_only"
  if (panel.histogram_buckets !== undefined && !validBuckets(panel.histogram_buckets)) return "workspace.histogram_buckets_invalid"
  if (panel.histogram_scale && !histogramScales.includes(panel.histogram_scale as HistogramScale)) return "workspace.histogram_scale_invalid"
  if (panel.histogram_color && !Object.hasOwn(panelColors, panel.histogram_color)) return "workspace.histogram_color_invalid"
  if (panel.histogram_merge !== undefined && typeof panel.histogram_merge !== "boolean") return "workspace.histogram_merge_invalid"
  return null
}

function validBuckets(value: number | undefined): boolean {
  return value !== undefined && Number.isInteger(value) && value >= minHistogramBuckets && value <= maxHistogramBuckets
}

/** histogramBins 把样本值分到等宽的取值区间；上下界之外的样本按边界裁剪，不会被丢弃。 */
export function histogramBins(values: readonly number[], options: { min?: number; max?: number; buckets: number; scale: HistogramScale }): HistogramBin[] {
  const finite = values.filter((value) => Number.isFinite(value))
  if (!finite.length) return []
  const bounds = valueBounds(finite, options)
  const bins = bounds.slice(0, -1).map((from, index) => ({ from, to: bounds[index + 1], count: 0 }))
  for (const value of finite) {
    const index = valueBinIndex(bounds, value)
    if (index >= 0 && index < bins.length) bins[index].count += 1
  }
  return bins
}

/**
 * histogramDistributions 把查询结果按序列（或合并成一个分布）聚合成直方图。
 * 合并时所有序列共用第一条序列的标签与直方图配色，避免合并后出现无法解释的图例。
 */
export function histogramDistributions(panel: HistogramFields, result: MetricQueryResult): HistogramDistribution[] {
  const style = panelHistogram(panel)
  const series = result.series.map((entry) => ({ entry, values: numericValues(entry.points) })).filter((item) => item.values.length > 0)
  if (!series.length) return []
  const boundsOptions = { min: panel.min, max: panel.max, buckets: style.buckets, scale: style.scale }
  if (style.merge) {
    const values = series.flatMap((item) => item.values)
    return [{
      refId: series[0].entry.ref_id, label: panel.title || series[0].entry.label, color: style.color, unit: series[0].entry.unit,
      bins: histogramBins(values, boundsOptions), total: values.length,
    }]
  }
  const bounds = valueBounds(series.flatMap((item) => item.values), boundsOptions)
  return series.map((item) => {
    const bins = bounds.slice(0, -1).map((from, index) => ({ from, to: bounds[index + 1], count: 0 }))
    for (const value of item.values) {
      const index = valueBinIndex(bounds, value)
      if (index >= 0 && index < bins.length) bins[index].count += 1
    }
    const color = panel.series?.find((entry) => entry.ref_id === item.entry.ref_id)?.color ?? style.color
    return { refId: item.entry.ref_id, label: item.entry.label || item.entry.ref_id, color, unit: item.entry.unit, bins, total: item.values.length }
  })
}

/** histogramBarRatio 把桶内样本数映射成 0..1 的柱高；对数刻度压高低频差异，让稀疏桶仍然可见。 */
export function histogramBarRatio(count: number, max: number, scale: HistogramScale): number {
  if (count <= 0 || max <= 0) return 0
  if (scale === "log") return Math.log1p(count) / Math.log1p(max)
  return Math.min(1, count / max)
}

/** numericValues 只保留有限数值样本：缺样本在查询结果里是 null，不能参与分桶。 */
function numericValues(points: readonly { value: number | null }[]): number[] {
  return points.filter((point): point is { value: number } => typeof point.value === "number" && Number.isFinite(point.value)).map((point) => point.value)
}

/** histogramPeak 返回所有分布里的最大桶计数，用于计数轴与柱高比例。 */
export function histogramPeak(distributions: readonly HistogramDistribution[]): number {
  return distributions.reduce((peak, distribution) => Math.max(peak, ...distribution.bins.map((bin) => bin.count)), 0)
}
