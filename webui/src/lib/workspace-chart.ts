import { thresholdColorDisabled } from "@/lib/workspace-threshold-display"
import type { DashboardPanel, ExpandedSeries, LegendSort, LegendValue, MetricQueryResult, MetricResultSeries, PanelColor, TooltipMode } from "@/types/metric-workspace"

export const panelColors: Record<PanelColor, string> = {
  blue: "#3b82f6", green: "#16a34a", amber: "#d97706", rose: "#e11d48", violet: "#8b5cf6", cyan: "#0891b2", slate: "#64748b",
}

export function mergeWorkspaceSeries(result: MetricQueryResult): Record<string, number | null>[] {
  const rows = new Map<number, Record<string, number | null>>()
  for (const series of result.series) for (const point of series.points) {
    const row = rows.get(point.timestamp) ?? Object.assign(Object.create(null), { timestamp: point.timestamp })
    row[series.ref_id] = point.value
    rows.set(point.timestamp, row)
  }
  return [...rows.values()].sort((a, b) => Number(a.timestamp) - Number(b.timestamp))
}

export const legendValueKinds: LegendValue[] = ["last", "min", "max", "mean", "sum", "count"]

/** 单个图例最多同时显示四类取值；可用取值增加到六类后仍按服务端上限拦截。 */
export const maximumLegendValues = 4
export const legendSortModes: LegendSort[] = ["none", "asc", "desc"]
export const tooltipModes: TooltipMode[] = ["all", "single", "off"]

export type LegendStatistics = Record<LegendValue, number | null>

/** 单次遍历即可得到图例需要的四类取值，避免长窗口上的多次扫描。 */
export function seriesLegendStatistics(series: MetricResultSeries): LegendStatistics {
  let count = 0, sum = 0, last: number | null = null, lowest: number | null = null, highest: number | null = null
  for (const point of series.points) {
    const value = point.value
    if (value === null || !Number.isFinite(value)) continue
    count += 1; sum += value; last = value
    lowest = lowest === null ? value : Math.min(lowest, value)
    highest = highest === null ? value : Math.max(highest, value)
  }
  // sum/count 与「无有效样本」保持可区分：空序列是 null，而不是 0（0 会被误读成真实零值）。
  return { last, min: lowest, max: highest, mean: count ? sum / count : null, sum: count ? sum : null, count: count ? count : null }
}

/** 图例排序以首个显示取值为准，未显示取值时按最新值排序，缺失样本始终排在最后。 */
export function workspaceLegendOrder(panel: DashboardPanel, result: MetricQueryResult): MetricResultSeries[] {
  const mode = panel.legend_sort ?? "none"
  if (mode === "none") return result.series
  const kind = panel.legend_values?.[0] ?? "last"
  const ranked = result.series.map((series) => ({ series, value: seriesLegendStatistics(series)[kind] }))
  ranked.sort((left, right) => {
    if (left.value === null || right.value === null) return left.value === right.value ? 0 : left.value === null ? 1 : -1
    return mode === "asc" ? left.value - right.value : right.value - left.value
  })
  return ranked.map((entry) => entry.series)
}

export function workspaceSeriesLabel(series: MetricResultSeries, expanded?: ExpandedSeries[]): string {
  const presentation = expanded?.find((value) => value.ref_id === series.ref_id)
  return [presentation?.label || series.label, presentation?.node_name].filter(Boolean).join(" · ")
}

export function latestWorkspaceValue(series: MetricResultSeries): number | null {
  return latestWorkspacePoint(series)?.value ?? null
}

/** seriesColor 解析某条序列的渲染颜色：展开视图里的覆盖色优先，否则用面板内联展示的默认色。 */
export function seriesColor(series: MetricResultSeries, expanded?: ExpandedSeries[]): string {
  return panelColors[expanded?.find((value) => value.ref_id === series.ref_id)?.color ?? "blue"]
}

export function latestWorkspacePoint(series: MetricResultSeries) {
  return series.points.findLast((point) => point.value !== null && Number.isFinite(point.value)) ?? null
}

export function workspaceThresholdColor(panel: DashboardPanel, value: number | null): string {
  let color = panel.series[0]?.color ?? "blue"
  // 关闭阈值取色时，只有值映射还能改颜色；stat/gauge 回到系列自身的颜色。
  if (value !== null && !thresholdColorDisabled(panel)) for (const threshold of panel.thresholds) if (value >= threshold.value) color = threshold.color
  return panelColors[color]
}

export function chartTimestamp(value: unknown): number | null {
  if (typeof value !== "number" && typeof value !== "string" || value === "") return null
  const parsed = Number(value)
  return Number.isFinite(parsed) && parsed > 0 ? parsed : null
}
