// 「填充到另一条序列」（Grafana 的 Overrides → Graph styles → Fill below to）的几何：
// 渲染层把填充带拆成「不可见的基线层 + 可见的增量层」两条堆叠面积，
// 增量层的上沿正好落在被覆盖序列自己的折线上，缺口处两层同时缺失，面积因此断开。
import { seriesOverride, workspaceSeriesAxisPlacement } from "@/lib/workspace-overrides"
import type { AxisSide } from "@/lib/workspace-axis-sides"
import type { DashboardOverride, MetricResultSeries } from "@/types/metric-workspace"

/** 派生数据键前缀：以双下划线开头，不会和面板 id/序列 id 的命名空间撞车。 */
const fillBelowBasePrefix = "__fill_base_"
const fillBelowDeltaPrefix = "__fill_delta_"
/** 覆盖里填充不透明度为 0 时给一个可读的起点，与堆叠、渐变的既有口径一致。 */
export const fillBelowFillOpacity = 10

export type FillBelowRow = Record<string, number | null>

export interface FillBelowBand {
  /** 被覆盖的序列：填充带画在它的折线下面。 */
  refId: string
  /** 填充终点序列的 ref_id。 */
  targetRefId: string
  /** 不可见基线层的键，取值是终点序列在该时间点的读数。 */
  baseKey: string
  /** 可见增量层的键，取值是「被覆盖序列 − 终点序列」。 */
  deltaKey: string
  /** 两层共用的堆叠组：每个被覆盖序列一组，互不影响。 */
  stackId: string
  /** 终点序列所在的轴；与折线不同轴时不画填充带。 */
  side: AxisSide
}

/** 面板里与填充带相关的字段：覆盖列表、类型与堆叠模式。 */
export interface FillBelowFields {
  type?: string
  stack?: string
  overrides?: DashboardOverride[]
}

export function fillBelowBaseKey(refId: string): string {
  return `${fillBelowBasePrefix}${refId}`
}

export function fillBelowDeltaKey(refId: string): string {
  return `${fillBelowDeltaPrefix}${refId}`
}

export function fillBelowStackId(refId: string): string {
  return `beat-fill-${refId}`
}

/** fillBelowBands 解析每条命中覆盖的填充终点；终点缺失、同轴条件不成立时该序列没有填充带。 */
export function fillBelowBands(panel: FillBelowFields | undefined, series: MetricResultSeries[]): FillBelowBand[] {
  if (panel?.type !== undefined && panel.type !== "timeseries") return []
  // 堆叠面板按层叠面积绘制：服务端同样拒绝这两个属性的组合，这里保持一致。
  if (panel?.stack !== undefined && panel.stack !== "" && panel.stack !== "none") return []
  if (!panel?.overrides?.length) return []
  return series.flatMap((source) => {
    const label = seriesOverride(panel, source).fillBelowTo
    const target = label ? fillBelowTarget(source, label, series) : undefined
    if (!target) return []
    return [{
      refId: source.ref_id, targetRefId: target.ref_id, baseKey: fillBelowBaseKey(source.ref_id),
      deltaKey: fillBelowDeltaKey(source.ref_id), stackId: fillBelowStackId(source.ref_id),
      side: workspaceSeriesAxisPlacement(panel, target),
    }]
  })
}

/** fillBelowTarget 先在同一节点里找同名序列：多节点看板里 min/max 两条查询各自成对，
 *  没有同节点候选时才退回结果里的第一条同名序列。 */
function fillBelowTarget(source: MetricResultSeries, label: string, series: MetricResultSeries[]): MetricResultSeries | undefined {
  // 同名同 ref_id 的序列在行数据里共用一列，差值恒为 0：只接受来自另一条查询的终点。
  const candidates = series.filter((candidate) => candidate.label === label && candidate.ref_id !== source.ref_id)
  return candidates.find((candidate) => candidate.node_id === source.node_id) ?? candidates[0]
}

/** applyFillBelowRows 写入两个派生键；任一侧缺样本时两层都写 null，让面积在缺口处断开。 */
export function applyFillBelowRows(rows: FillBelowRow[], bands: FillBelowBand[]): FillBelowRow[] {
  if (bands.length === 0) return rows
  return rows.map((row) => {
    const next = Object.assign(Object.create(null), row) as FillBelowRow
    for (const band of bands) {
      const own = sampleValue(row, band.refId), target = sampleValue(row, band.targetRefId)
      const usable = own !== null && target !== null
      next[band.baseKey] = usable ? target : null
      next[band.deltaKey] = usable ? own - target : null
    }
    return next
  })
}

/** fillBelowTooltipValue 让 tooltip 继续显示被覆盖序列自己的读数：增量层的数据键值是两条线之差。 */
export function fillBelowTooltipValue(value: number, item: unknown, bands: FillBelowBand[]): number {
  const dataKey = String((item as { dataKey?: unknown } | undefined)?.dataKey ?? "")
  const band = bands.find((candidate) => candidate.deltaKey === dataKey)
  if (!band) return value
  const own = sampleValue((item as { payload?: FillBelowRow } | undefined)?.payload, band.refId)
  return own ?? value
}

/** fillBelowOpacity 返回填充带的百分比不透明度：0 会让填充带整条不可见，因此退回可读的起点。 */
export function fillBelowOpacity(fillOpacity: number): number {
  return fillOpacity > 0 ? fillOpacity : fillBelowFillOpacity
}

function sampleValue(row: FillBelowRow | undefined, key: string): number | null {
  const value = row?.[key]
  return typeof value === "number" && Number.isFinite(value) ? value : null
}
