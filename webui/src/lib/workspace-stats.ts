import { workspaceSeriesName, workspaceSeriesUnit, type OverrideFields } from "@/lib/workspace-overrides"
import { workspaceCSVCell } from "@/lib/workspace-export"
import type { ExpandedSeries, MetricQueryResult, MetricResultSeries } from "@/types/metric-workspace"

/** 单条序列的统计摘要，字段与 Grafana 检查器的 Stats 视图一致。 */
export interface WorkspaceSeriesStats {
  key: string
  label: string
  unit: string
  min: number | null
  max: number | null
  mean: number | null
  first: number | null
  last: number | null
  samples: number
  gaps: number
}

/**
 * 计算检查器 Stats 表：空点（分桶无数据）不参与统计，只计入 gaps，
 * 因此「无数据」不会把最小值拉成 0；首个/末个取首个/末个有效样本。
 */
export function workspaceSeriesStats(result: MetricQueryResult, expanded?: ExpandedSeries[], panel?: OverrideFields): WorkspaceSeriesStats[] {
  return result.series.map((series) => seriesStats(series, expanded, panel))
}

function seriesStats(series: MetricResultSeries, expanded?: ExpandedSeries[], panel?: OverrideFields): WorkspaceSeriesStats {
  const stats: WorkspaceSeriesStats = {
    key: `${series.ref_id}-${series.node_id}-${series.device_id ?? ""}`,
    label: workspaceSeriesName(panel, series, expanded),
    unit: workspaceSeriesUnit(panel, series),
    min: null,
    max: null,
    mean: null,
    first: null,
    last: null,
    samples: 0,
    gaps: 0,
  }
  let total = 0
  for (const point of series.points) {
    const value = point.value
    if (value === null || !Number.isFinite(value)) {
      stats.gaps += 1
      continue
    }
    total += value
    stats.samples += 1
    stats.min = stats.min === null ? value : Math.min(stats.min, value)
    stats.max = stats.max === null ? value : Math.max(stats.max, value)
    if (stats.first === null) stats.first = value
    stats.last = value
  }
  stats.mean = stats.samples === 0 ? null : total / stats.samples
  return stats
}

/** Stats 表的可排序列；label 按语言比较，其余列按数值比较。 */
export type WorkspaceStatsColumn = "label" | "min" | "max" | "mean" | "first" | "last" | "samples" | "gaps"

/**
 * 按列排序。数值列把空值固定排在最后（升序与降序都一样），避免「没有数据」靠方向翻到最前面；
 * 值相等时保持原有序列顺序，排序结果稳定。
 */
export function sortWorkspaceStats(rows: WorkspaceSeriesStats[], column: WorkspaceStatsColumn, direction: "asc" | "desc"): WorkspaceSeriesStats[] {
  const factor = direction === "desc" ? -1 : 1
  return rows.slice().sort((left, right) => {
    if (column === "label") return left.label.localeCompare(right.label) * factor
    const first = left[column], second = right[column]
    if (first === null && second === null) return 0
    if (first === null) return 1
    if (second === null) return -1
    return (first - second) * factor
  })
}

/** 导出 Stats 表为 CSV：表头由调用方给出（已本地化），数值保持原始精度，空值导出为空单元格。 */
export function workspaceStatsCSV(rows: WorkspaceSeriesStats[], headers: string[]): string {
  const lines = [headers.map(workspaceCSVCell).join(",")]
  for (const row of rows) {
    const values = [row.label, row.min, row.max, row.mean, row.first, row.last, row.samples, row.gaps]
    lines.push(values.map((value) => workspaceCSVCell(value === null ? "" : String(value))).join(","))
  }
  return lines.join("\r\n")
}

/** Stats 表的表尾小计：序列条数、有效采样与缺口总量；没有采样时覆盖率返回 null。 */
export interface WorkspaceStatsTotals { series: number; samples: number; gaps: number; coverage: number | null }

/**
 * 统计整张表的覆盖情况。缺口是「分桶里没有数据」的数量，所以覆盖率 = 采样 / (采样 + 缺口)；
 * 空表返回 null 而不是 0，调用方据此显示占位符。合计行不进入 CSV，导出的仍是逐条序列。
 */
export function workspaceStatsTotals(rows: WorkspaceSeriesStats[]): WorkspaceStatsTotals {
  let samples = 0
  let gaps = 0
  for (const row of rows) {
    samples += row.samples
    gaps += row.gaps
  }
  const buckets = samples + gaps
  return { series: rows.length, samples, gaps, coverage: buckets === 0 ? null : samples / buckets }
}
