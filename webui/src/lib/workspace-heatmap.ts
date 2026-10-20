import { panelColors } from "@/lib/workspace-chart"
import { valueBinIndex, valueBounds } from "@/lib/workspace-value-bins"
import type { DashboardPanel, HeatmapScale, MetricQueryResult, PanelColor, PanelType } from "@/types/metric-workspace"

/** 与服务端 internal/model 同规则：分桶 2..64、刻度 linear/log、配色取面板调色板。 */
export const heatmapScales = ["linear", "log"] as const
export const minHeatmapBuckets = 2
export const maxHeatmapBuckets = 64
export const defaultHeatmapBuckets = 12
export const defaultHeatmapScale: HeatmapScale = "linear"
export const defaultHeatmapColor: PanelColor = "blue"
/** 单元格预算：热力图按「时间列 × 取值分桶」画方块，超过预算就合并时间列，避免一次渲染上万个节点。 */
export const maxHeatmapCells = 3000
/** 面板类型切换时需要一并清理的热力图字段；其它可视化类型不接受这些字段。 */
export const heatmapFieldNames = ["heatmap_buckets", "heatmap_scale", "heatmap_color"] as const

/** 热力图的可编辑子集：编辑器与导入导出都只碰这几个字段，导入时类型还是原始字符串。 */
export interface HeatmapFields {
  type: string
  heatmap_buckets?: number
  heatmap_scale?: string
  heatmap_color?: string
  /** 取值轴上下界；留空表示按样本范围自动取值。 */
  min?: number
  max?: number
}

export interface PanelHeatmap {
  buckets: number
  scale: HeatmapScale
  color: PanelColor
}

export interface HeatmapGrid {
  /** 时间列，按时间升序；列宽相等，覆盖全部样本的时间范围。 */
  columns: { from: number; to: number }[]
  /** 取值分桶，按取值升序。 */
  rows: { from: number; to: number }[]
  /** counts[row][column] 是落在该单元格里的样本数量。 */
  counts: number[][]
  /** 单元格最大样本数，用于彩色标尺。 */
  max: number
  /** 参与统计的样本总数。 */
  total: number
}

/** panelHeatmap 解析生效值，缺省与服务端一致：12 个分桶、线性刻度、蓝色。 */
export function panelHeatmap(panel: HeatmapFields): PanelHeatmap {
  return {
    buckets: validBuckets(panel.heatmap_buckets) ? (panel.heatmap_buckets as number) : defaultHeatmapBuckets,
    scale: heatmapScales.find((value) => value === panel.heatmap_scale) ?? defaultHeatmapScale,
    color: (Object.keys(panelColors).find((value) => value === panel.heatmap_color) ?? defaultHeatmapColor) as PanelColor,
  }
}

/** heatmapPatch 切换面板类型时清理字段：只有热力图接受这三项。 */
export function heatmapPatch(type: PanelType): Pick<DashboardPanel, "heatmap_buckets" | "heatmap_scale" | "heatmap_color"> {
  if (type === "heatmap") return {}
  return { heatmap_buckets: undefined, heatmap_scale: undefined, heatmap_color: undefined }
}

/** heatmapError 返回目录键，null 表示可以保存；规则与服务端一致。 */
export function heatmapError(panel: HeatmapFields): string | null {
  const styled = panel.heatmap_buckets !== undefined || Boolean(panel.heatmap_scale) || Boolean(panel.heatmap_color)
  if (styled && panel.type !== "heatmap") return "workspace.heatmap_heatmap_only"
  if (panel.heatmap_buckets !== undefined && !validBuckets(panel.heatmap_buckets)) return "workspace.heatmap_buckets_invalid"
  if (panel.heatmap_scale && !heatmapScales.includes(panel.heatmap_scale as HeatmapScale)) return "workspace.heatmap_scale_invalid"
  if (panel.heatmap_color && !Object.hasOwn(panelColors, panel.heatmap_color)) return "workspace.heatmap_color_invalid"
  return null
}

function validBuckets(value: number | undefined): boolean {
  return value !== undefined && Number.isInteger(value) && value >= minHeatmapBuckets && value <= maxHeatmapBuckets
}

/** heatmapColumnBudget 按取值分桶数算出允许的时间列上限，保证总单元格数不会超出渲染预算。 */
export function heatmapColumnBudget(buckets: number): number {
  return Math.max(1, Math.floor(maxHeatmapCells / Math.max(1, buckets)))
}

/**
 * heatmapValueBounds 生成分桶边界；与直方图共用 `workspace-value-bins` 的实现，
 * 两种面板对同一批样本会得到完全一致的区间。
 */
export function heatmapValueBounds(values: readonly number[], options: { min?: number; max?: number; buckets: number; scale: HeatmapScale }): number[] {
  return valueBounds(values, options)
}

/** heatmapTimeColumns 把时间范围等分成至多 columns 列；样本时间落在同一分钟内也能各自成列。 */
export function heatmapTimeColumns(timestamps: readonly number[], columns: number): { from: number; to: number }[] {
  // 去重后再决定列数：同一时刻的多条序列属于同一列，不能各占一列。
  const distinct = [...new Set(timestamps.filter((value) => Number.isFinite(value)))].sort((left, right) => left - right)
  if (!distinct.length) return []
  const first = distinct[0], last = distinct[distinct.length - 1]
  const count = Math.max(1, Math.min(columns, distinct.length))
  if (last <= first || count === 1) return [{ from: first, to: last }]
  const width = (last - first) / count
  return Array.from({ length: count }, (_, index) => ({ from: first + width * index, to: first + width * (index + 1) }))
}

/** heatmapIntensity 把样本数映射成 0..1 的强度；对数刻度压高低频差异，让稀疏区间仍然可见。 */
export function heatmapIntensity(count: number, max: number, scale: HeatmapScale): number {
  if (count <= 0 || max <= 0) return 0
  if (scale === "log") return Math.log1p(count) / Math.log1p(max)
  return Math.min(1, count / max)
}

/** heatmapCellColor 用调色板颜色按强度生成填充色；强度为零时留一层近乎透明的底，网格仍然可见。 */
export function heatmapCellColor(color: PanelColor | string, intensity: number): string {
  const hex = panelColors[color as PanelColor] ?? panelColors[defaultHeatmapColor]
  const red = Number.parseInt(hex.slice(1, 3), 16), green = Number.parseInt(hex.slice(3, 5), 16), blue = Number.parseInt(hex.slice(5, 7), 16)
  const alpha = 0.06 + 0.9 * Math.max(0, Math.min(1, intensity))
  return `rgba(${red}, ${green}, ${blue}, ${alpha.toFixed(3)})`
}

/** heatmapGrid 把查询结果聚合成「时间列 × 取值分桶」的计数网格；非有限样本不参与统计。 */
export function heatmapGrid(panel: HeatmapFields, result: MetricQueryResult): HeatmapGrid {
  const style = panelHeatmap(panel)
  const samples: { timestamp: number; value: number }[] = []
  for (const series of result.series) for (const point of series.points) {
    if (typeof point.value === "number" && Number.isFinite(point.value) && Number.isFinite(point.timestamp)) samples.push({ timestamp: point.timestamp, value: point.value })
  }
  if (!samples.length) return { columns: [], rows: [], counts: [], max: 0, total: 0 }
  const bounds = heatmapValueBounds(samples.map((sample) => sample.value), { min: panel.min, max: panel.max, buckets: style.buckets, scale: style.scale })
  const columns = heatmapTimeColumns(samples.map((sample) => sample.timestamp), heatmapColumnBudget(bounds.length - 1))
  const rows = bounds.slice(0, -1).map((from, index) => ({ from, to: bounds[index + 1] }))
  const counts = rows.map(() => columns.map(() => 0))
  for (const sample of samples) {
    const row = valueBinIndex(bounds, sample.value)
    const column = columnIndex(columns, sample.timestamp)
    if (row >= 0 && column >= 0) counts[row][column] += 1
  }
  const max = counts.reduce((highest, row) => Math.max(highest, ...row), 0)
  return { columns, rows, counts, max, total: samples.length }
}

/** columnIndex 返回时间所属的列下标，越界值按边界裁剪。 */
function columnIndex(columns: readonly { from: number; to: number }[], timestamp: number): number {
  if (!columns.length) return -1
  for (let index = 0; index < columns.length; index += 1) if (timestamp < columns[index].to) return index
  return columns.length - 1
}
