import { mappedWorkspaceValue } from "@/lib/workspace-value-mapping"
import { workspaceSeriesThresholds } from "@/lib/workspace-overrides"
import { thresholdColorDisabled } from "@/lib/workspace-threshold-display"
import type { DashboardPanel, MetricQueryResult, MetricResultSeries, PanelColor, PanelType, TimelineShowValues } from "@/types/metric-workspace"

/** 与服务端 internal/model 同规则：标签模式 auto/always/never，合并相邻相同状态缺省开启。 */
export const timelineValueModes = ["auto", "always", "never"] as const
export const defaultTimelineMerge = true
export const defaultTimelineShowValues: TimelineShowValues = "auto"
/** 面板类型切换时需要一并清理的状态时间线字段；其它可视化类型不接受这些字段。 */
export const timelineFieldNames = ["timeline_merge", "timeline_show_values"] as const

/**
 * 每行的色块预算：状态时间线按采样点画块，一小时窗口的 600 个点会产生 600 个节点。
 * 超出预算时改成等宽时间桶并取桶内最后一个状态，既保住时间线形状也让 DOM 有上界。
 */
export const maxTimelineSegments = 120
/** 取值标签的最小宽度占比：always 更宽容，auto 只在块足够宽时显示，避免文字互相压住。 */
const labelRatioAlways = 0.05
const labelRatioAuto = 0.15

/** 状态时间线的可编辑子集：编辑器与导入导出都只碰这几个字段，导入时类型还是原始字符串。 */
/** 只读选项子集：类型与两个状态时间线字段，导入时类型还是原始字符串。 */
export interface StateTimelineOptions {
  type: string
  timeline_merge?: boolean
  timeline_show_values?: string
}

/** 渲染状态时间线所需的面板子集：状态取色还要读序列颜色、阈值与值映射。 */
export interface StateTimelineFields extends StateTimelineOptions, Pick<DashboardPanel, "series" | "thresholds" | "threshold_mode" | "value_mappings"> {}

export interface PanelStateTimeline { merge: boolean; showValues: TimelineShowValues }
/** 一个离散状态：key 决定相邻块能否合并，text 是值映射给出的显示文本（没有映射时按 value 格式化取值）。 */
export interface TimelineState { key: string; text: string | null; value: number | null; color: PanelColor }
export interface TimelineSegment { from: number; to: number; state: TimelineState | null }
export interface TimelineRow { refId: string; label: string; segments: TimelineSegment[] }
export interface StateTimelineSummary {
  from: number
  to: number
  rows: TimelineRow[]
  /** 出现过的状态，按块数降序；图例只列出真正渲染出来的状态。 */
  states: { state: TimelineState; count: number }[]
  segments: number
}

/** panelStateTimeline 解析生效值，缺省与服务端一致：合并相邻相同状态、标签 auto。 */
export function panelStateTimeline(panel: StateTimelineOptions): PanelStateTimeline {
  return {
    merge: panel.timeline_merge ?? defaultTimelineMerge,
    showValues: timelineValueModes.find((mode) => mode === panel.timeline_show_values) ?? defaultTimelineShowValues,
  }
}

/** stateTimelinePatch 切换面板类型时清理字段：只有状态时间线接受这两项。 */
export function stateTimelinePatch(type: PanelType): Pick<DashboardPanel, "timeline_merge" | "timeline_show_values"> {
  if (type === "statetimeline") return {}
  return { timeline_merge: undefined, timeline_show_values: undefined }
}

/** stateTimelineError 返回目录键，null 表示可以保存；规则与服务端一致。 */
export function stateTimelineError(panel: StateTimelineOptions): string | null {
  if (panel.timeline_show_values && !timelineValueModes.includes(panel.timeline_show_values as TimelineShowValues)) return "workspace.state_timeline_show_values_invalid"
  const styled = panel.timeline_merge !== undefined || Boolean(panel.timeline_show_values)
  if (styled && panel.type !== "statetimeline") return "workspace.state_timeline_statetimeline_only"
  return null
}

/** timelineShowsLabel 判断某个宽度的色块是否显示取值标签。 */
export function timelineShowsLabel(mode: TimelineShowValues, ratio: number): boolean {
  if (mode === "never") return false
  return ratio >= (mode === "always" ? labelRatioAlways : labelRatioAuto)
}

/** stateTimelineState 返回一个样本对应的离散状态：值映射优先，其次阈值取色，最后回落到序列自身颜色。 */
export function stateTimelineState(panel: StateTimelineFields, series: MetricResultSeries, value: number | null): TimelineState | null {
  const mapped = mappedWorkspaceValue(panel, value)
  if (mapped) return { key: `m:${mapped.text ?? ""}:${mapped.color ?? ""}`, text: mapped.text ?? null, value, color: mapped.color ?? fallbackColor(panel, series, value) }
  if (value === null) return null
  return { key: `v:${value}`, text: null, value, color: fallbackColor(panel, series, value) }
}

/**
 * stateTimeline 生成整块时间线：每条序列一行，行内是按时间顺序排列的状态色块。
 * 合并只作用于相邻的相同状态，因此连续的同状态采样会变成一条长色块。
 */
export function stateTimeline(panel: StateTimelineFields, result: MetricQueryResult, options: { label: (series: MetricResultSeries) => string; budget?: number }): StateTimelineSummary {
  const budget = Math.max(1, Math.floor(options.budget ?? maxTimelineSegments))
  const from = rangeStart(result)
  const to = rangeEnd(result, from)
  const rows: TimelineRow[] = []
  const states = new Map<string, { state: TimelineState; count: number }>()
  for (const series of result.series) {
    const segments = rowSegments(panel, series, from, to, budget)
    // 一行完全没有已知状态时只画一条空轨道，既没有信息也占版面，直接跳过。
    if (!segments.some((segment) => segment.state !== null)) continue
    rows.push({ refId: series.ref_id, label: options.label(series), segments })
    for (const segment of segments) {
      if (!segment.state) continue
      const entry = states.get(segment.state.key) ?? { state: segment.state, count: 0 }
      entry.count += 1
      states.set(segment.state.key, entry)
    }
  }
  return { from, to, rows, states: [...states.values()].sort((left, right) => right.count - left.count), segments: rows.reduce((total, row) => total + row.segments.length, 0) }
}

/** 时间轴起点取查询窗口的起点；无法解析时回落到最早样本。 */
function rangeStart(result: MetricQueryResult): number {
  const parsed = Date.parse(result.from ?? "")
  if (Number.isFinite(parsed)) return parsed
  const stamps = result.series.flatMap((series) => series.points.map((point) => point.timestamp)).filter(Number.isFinite)
  return stamps.length ? Math.min(...stamps) : 0
}

/** 时间轴终点取查询窗口的终点；不晚于起点时回落到最晚样本，保证块宽始终为正。 */
function rangeEnd(result: MetricQueryResult, from: number): number {
  const parsed = Date.parse(result.to ?? "")
  if (Number.isFinite(parsed) && parsed > from) return parsed
  const stamps = result.series.flatMap((series) => series.points.map((point) => point.timestamp)).filter((value) => Number.isFinite(value) && value > from)
  return stamps.length ? Math.max(...stamps) : from + 1
}

function rowSegments(panel: StateTimelineFields, series: MetricResultSeries, from: number, to: number, budget: number): TimelineSegment[] {
  const samples = series.points.filter((point) => Number.isFinite(point.timestamp)).sort((left, right) => left.timestamp - right.timestamp)
  if (!samples.length) return []
  const merge = panelStateTimeline(panel).merge
  const runs = runSegments(panel, series, samples, to, merge)
  return runs.length <= budget ? runs : bucketSegments(panel, series, samples, from, to, budget, merge)
}

/** 逐个采样点生成色块：每块延伸到下一个采样点，最后一块延伸到窗口终点。 */
function runSegments(panel: StateTimelineFields, series: MetricResultSeries, samples: MetricResultSeries["points"], to: number, merge: boolean): TimelineSegment[] {
  const segments: TimelineSegment[] = []
  for (const [index, sample] of samples.entries()) {
    const end = samples[index + 1]?.timestamp ?? to
    if (!(end > sample.timestamp)) continue
    const state = stateTimelineState(panel, series, sample.value)
    const previous = segments[segments.length - 1]
    if (merge && previous && previous.to === sample.timestamp && sameState(previous.state, state)) {
      previous.to = end
      continue
    }
    segments.push({ from: sample.timestamp, to: end, state })
  }
  return segments
}

/** 超预算时按等宽时间桶取样：桶内取最后一个采样点的状态，再合并相邻的相同状态。 */
function bucketSegments(panel: StateTimelineFields, series: MetricResultSeries, samples: MetricResultSeries["points"], from: number, to: number, budget: number, merge: boolean): TimelineSegment[] {
  const width = (to - from) / budget
  const buckets = new Map<number, TimelineSegment>()
  for (const sample of samples) {
    const index = Math.min(budget - 1, Math.max(0, Math.floor((sample.timestamp - from) / width)))
    const start = from + index * width
    buckets.set(index, { from: start, to: index === budget - 1 ? to : start + width, state: stateTimelineState(panel, series, sample.value) })
  }
  const ordered = [...buckets.entries()].sort((left, right) => left[0] - right[0]).map(([, segment]) => segment)
  if (!merge) return ordered
  const merged: TimelineSegment[] = []
  for (const segment of ordered) {
    const previous = merged[merged.length - 1]
    if (previous && previous.to === segment.from && sameState(previous.state, segment.state)) {
      previous.to = segment.to
      continue
    }
    merged.push({ ...segment })
  }
  return merged
}

function sameState(left: TimelineState | null, right: TimelineState | null): boolean {
  return left?.key === right?.key
}

/** 阈值取色：关闭阈值取色时只有值映射能改颜色，序列回到自身的颜色。 */
function fallbackColor(panel: StateTimelineFields, series: MetricResultSeries, value: number | null): PanelColor {
  const base = panel.series.find((candidate) => candidate.ref_id === series.ref_id)?.color ?? panel.series[0]?.color ?? "blue"
  if (value === null || thresholdColorDisabled(panel)) return base
  let color: PanelColor = base
  for (const threshold of workspaceSeriesThresholds(panel, series)) if (value >= threshold.value) color = threshold.color
  return color
}
