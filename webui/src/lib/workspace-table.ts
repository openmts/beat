// 表格面板的行模型（Grafana 的 Table）：把结果里的每条采样摊成一行，供排序与 CSV 导出复用。
// 排序规则与检查器的 Stats 表一致——空值恒排最后、值相等保持原有顺序。

import type { CSSProperties } from "react"
import { workspaceCSVCell } from "@/lib/workspace-export"
import type { WorkspaceFrameColumn, WorkspaceFrameRow } from "@/lib/workspace-transformations"
import { cellDisplayModes, tableOrientations, type DashboardCellDisplayMode, type DashboardPanel, type DashboardTableOrientation, type ExpandedSeries, type MetricQueryResult, type PanelType } from "@/types/metric-workspace"
import { workspaceSeriesName, workspaceSeriesThresholdColor, workspaceSeriesThresholds, workspaceSeriesUnit, type OverrideFields, type OverridableSeries } from "@/lib/workspace-overrides"

/** 表格面板一次渲染的行数上限：超过时给出提示，完整样本仍可通过导出拿到。 */
export const maximumTableRows = 100

export interface WorkspaceTableRow {
  key: string
  /** 序列标识：同一条序列的多个采样共享，用于行高亮与去重。 */
  seriesKey: string
  refID: string
  label: string
  unit: string
  timestamp: number
  value: number | null
}

/** 可排序的列：与表格的显示列一一对应。 */
export type WorkspaceTableColumn = "timestamp" | "label" | "value"
export const workspaceTableColumns: WorkspaceTableColumn[] = ["timestamp", "label", "value"]

export interface WorkspaceTableRowOptions {
  /** 丢掉「所有序列都为空」的时间点：查询按 max_points 返回定长桶网格，表格面板不丢就会满屏空行。 */
  dropEmptyBuckets?: boolean
  /** 面板用于解析字段覆盖后的显示单位；检查器传入面板以渲染同样的单位。 */
  panel?: OverrideFields
}

export function workspaceTableRows(result: MetricQueryResult, expanded?: ExpandedSeries[], options: WorkspaceTableRowOptions = {}): WorkspaceTableRow[] {
  // 定长桶网格里多数桶是空的（60 秒窗口也会给 600 个桶）：面板丢掉它们，检查器与 Explore 保留原始结果。
  const sampled = options.dropEmptyBuckets ? sampledTimestamps(result) : null
  return result.series.flatMap((series) => {
    const label = workspaceSeriesName(options.panel, series, expanded)
    const seriesKey = series.device_id ? `${series.ref_id}-${series.node_id}-${series.device_id}` : `${series.ref_id}-${series.node_id}`
    const points = sampled ? series.points.filter((point) => sampled.has(point.timestamp)) : series.points
    return points.map((point) => ({
      key: `${seriesKey}@${point.timestamp}`, seriesKey, refID: series.ref_id, label, unit: workspaceSeriesUnit(options.panel, series),
      timestamp: point.timestamp, value: point.value,
    }))
  })
}

/** sampledTimestamps 收集真正有采样的时间点；空桶不属于其中。 */
function sampledTimestamps(result: MetricQueryResult): Set<number> {
  const sampled = new Set<number>()
  for (const series of result.series) for (const point of series.points) if (point.value !== null) sampled.add(point.timestamp)
  return sampled
}

/** sortWorkspaceTableRows 按列排序：时间戳与数值按数值比较，序列名按当前语言比较。 */
export function sortWorkspaceTableRows(
  rows: WorkspaceTableRow[], column: WorkspaceTableColumn, direction: "asc" | "desc",
): WorkspaceTableRow[] {
  const factor = direction === "desc" ? -1 : 1
  return rows.slice().sort((left, right) => {
    if (column === "label") return left.label.localeCompare(right.label) * factor || left.timestamp - right.timestamp
    if (column === "timestamp") return (left.timestamp - right.timestamp) * factor
    return compareValues(left.value, right.value, factor)
  })
}

function compareValues(first: number | null, second: number | null, factor: number): number {
  if (first === null && second === null) return 0
  if (first === null) return 1
  if (second === null) return -1
  return (first - second) * factor
}

/** 导出给定行为 CSV：表头由调用方给出（已本地化），时间戳用 ISO，数值保持原始精度。 */
export function workspaceTableCSV(rows: WorkspaceTableRow[], headers: string[]): string {
  const lines = [headers.map(workspaceCSVCell).join(",")]
  for (const row of rows) {
    lines.push([new Date(row.timestamp).toISOString(), row.label, row.value === null ? "" : String(row.value)].map(workspaceCSVCell).join(","))
  }
  return lines.join("\r\n")
}

/** 表格形态：缺省等于 long，也就是历史的一行一个采样。 */
export function panelTableOrientation(panel: DashboardPanel): DashboardTableOrientation {
  return tableOrientations.includes(panel.table_orientation as DashboardTableOrientation)
    ? panel.table_orientation as DashboardTableOrientation
    : "long"
}

/** 表格形态只对表格面板生效；其它类型带上它会被服务端拒绝，编辑器同样报错。 */
export function tableOrientationError(panel: DashboardPanel): string | null {
  if (panel.table_orientation === undefined) return null
  if (panel.type !== "table") return "workspace.table_orientation_table_only"
  return tableOrientations.includes(panel.table_orientation) ? null : "workspace.table_orientation_invalid"
}

/** tablePatch 切换面板类型时清理表格专属字段：非表格面板带上它会被服务端拒绝。 */
export function tablePatch(type: PanelType, panel?: DashboardPanel): Pick<DashboardPanel, "table_orientation"> {
  if (type === "table") return {}
  return panel?.table_orientation === undefined ? {} : { table_orientation: undefined }
}

/** 单元格显示模式：缺省等于 text，也就是只有值映射能改文本颜色的历史行为。 */
export function panelCellDisplayMode(panel: DashboardPanel): DashboardCellDisplayMode {
  return cellDisplayModes.includes(panel.cell_display_mode as DashboardCellDisplayMode)
    ? panel.cell_display_mode as DashboardCellDisplayMode
    : "text"
}

/** 单元格显示模式只对表格面板生效；其它类型带上它会被服务端拒绝，编辑器同样报错。 */
export function cellDisplayModeError(panel: DashboardPanel): string | null {
  if (panel.cell_display_mode === undefined) return null
  if (panel.type !== "table") return "workspace.cell_display_table_only"
  return cellDisplayModes.includes(panel.cell_display_mode) ? null : "workspace.cell_display_invalid"
}

const gradientAlphaFloor = 0.15
const gradientAlphaCeiling = 0.85

/** 单元格取色只需要行的数值与所属序列：序列用来解析逐序列阈值与声明色。 */
export interface WorkspaceTableCell {
  value: number | null
  series?: OverridableSeries
}

/**
 * 表格单元格样式：值映射的颜色优先，其次按阈值取色。text/color_text 只改文本颜色，
 * color_background 给单元格上色并配可读前景，gradient 在当前阈值区间内插值背景透明度。
 */
export function tableCellStyle(panel: DashboardPanel, cell: WorkspaceTableCell, mappedColor?: string): CSSProperties | undefined {
  const mode = panelCellDisplayMode(panel)
  const value = cell.value
  if (mode === "text") return mappedColor ? { color: mappedColor } : undefined
  if (value === null || !Number.isFinite(value)) return mappedColor ? { color: mappedColor } : undefined
  const thresholds = workspaceSeriesThresholds(panel, cell.series)
  const tone = mappedColor ?? workspaceSeriesThresholdColor(panel, cell.series, value)
  if (mode === "color_text") return { color: tone }
  if (mode === "color_background") return { backgroundColor: tone, color: readableTextColor(tone) }
  return { backgroundColor: withAlpha(tone, gradientAlpha(thresholds, value)) }
}

/** 在所属阈值区间内的相对位置决定透明度：最低一档最淡，开放区间的最后一档最浓。 */
function gradientAlpha(thresholds: { value: number }[], value: number): number {
  const bands = thresholds.map((threshold) => threshold.value)
  let index = -1
  for (let current = 0; current < bands.length; current += 1) if (value >= bands[current]!) index = current
  const start = index < 0 ? Number.NEGATIVE_INFINITY : bands[index]!
  const end = index + 1 < bands.length ? bands[index + 1]! : Number.POSITIVE_INFINITY
  const position = Number.isFinite(start) && Number.isFinite(end) && end > start ? (value - start) / (end - start) : index < 0 ? 0 : 1
  return gradientAlphaFloor + (gradientAlphaCeiling - gradientAlphaFloor) * Math.min(1, Math.max(0, position))
}

/** 阈值色是固定的十六进制调色板，按相对亮度选深色或浅色前景，保证背景模式下的对比度。 */
function readableTextColor(hex: string): string {
  const [red, green, blue] = hexChannels(hex)
  const luminance = (0.2126 * red + 0.7152 * green + 0.0722 * blue) / 255
  return luminance > 0.6 ? "#0f172a" : "#f8fafc"
}

function withAlpha(hex: string, alpha: number): string {
  const [red, green, blue] = hexChannels(hex)
  return `rgba(${red}, ${green}, ${blue}, ${alpha.toFixed(2)})`
}

function hexChannels(hex: string): [number, number, number] {
  const value = hex.replace("#", "")
  return [Number.parseInt(value.slice(0, 2), 16), Number.parseInt(value.slice(2, 4), 16), Number.parseInt(value.slice(4, 6), 16)]
}

/** 宽表的一列：一条序列。refID 与 label 供单元格取色解析逐序列覆盖与阈值。 */
export interface WorkspaceWideColumn {
  key: string
  refID: string
  label: string
  unit: string
}

/** 宽表的一行：一个时间点；cells 与列一一对应，null 表示该序列在这个时间点没有样本。 */
export interface WorkspaceWideRow {
  key: string
  timestamp: number
  cells: (number | null)[]
}

export interface WorkspaceWideOptions {
  /** 面板用于解析字段覆盖后的显示单位；检查器不传，保持原始单位。 */
  panel?: OverrideFields
}

/** workspaceWideColumns 按序列顺序给出列：Grafana 的宽表一条序列一列。 */
export function workspaceWideColumns(result: MetricQueryResult, expanded?: ExpandedSeries[], options: WorkspaceWideOptions = {}): WorkspaceWideColumn[] {
  return result.series.map((series) => ({
    key: series.device_id ? `${series.ref_id}-${series.node_id}-${series.device_id}` : `${series.ref_id}-${series.node_id}`,
    refID: series.ref_id, label: workspaceSeriesName(options.panel, series, expanded), unit: workspaceSeriesUnit(options.panel, series),
  }))
}

/** workspaceWideRows 把多条序列按时间戳并成一行；所有序列都为空的时间点不占一行。 */
export function workspaceWideRows(result: MetricQueryResult, columns: WorkspaceWideColumn[]): WorkspaceWideRow[] {
  const rows = new Map<number, (number | null)[]>()
  result.series.forEach((series, index) => {
    for (const point of series.points) {
      if (point.value === null || !Number.isFinite(point.value)) continue
      const cells = rows.get(point.timestamp) ?? columns.map(() => null)
      cells[index] = point.value
      rows.set(point.timestamp, cells)
    }
  })
  return [...rows.entries()].sort(([left], [right]) => left - right)
    .map(([timestamp, cells]) => ({ key: String(timestamp), timestamp, cells }))
}

/** sortWorkspaceWideRows 按时间轴或某一列排序；column 为 null 时表示时间轴，空值恒排最后。 */
export function sortWorkspaceWideRows(rows: WorkspaceWideRow[], column: number | null, direction: "asc" | "desc"): WorkspaceWideRow[] {
  const factor = direction === "desc" ? -1 : 1
  if (column === null) return rows.slice().sort((left, right) => (left.timestamp - right.timestamp) * factor)
  return rows.slice().sort((left, right) => compareValues(left.cells[column] ?? null, right.cells[column] ?? null, factor) || left.timestamp - right.timestamp)
}

/** workspaceWideCSV 导出宽表：表头由调用方给出（已本地化），缺失值留空。 */
export function workspaceWideCSV(rows: WorkspaceWideRow[], headers: string[]): string {
  const lines = [headers.map(workspaceCSVCell).join(",")]
  for (const row of rows) {
    const cells = row.cells.map((value) => value === null || !Number.isFinite(value) ? "" : String(value))
    lines.push([new Date(row.timestamp).toISOString(), ...cells].map(workspaceCSVCell).join(","))
  }
  return lines.join("\r\n")
}

/** sortWorkspaceFrameRows 按列排序结果帧：键列按当前语言比较文本，数值列按数值比较、空值恒排最后。 */
export function sortWorkspaceFrameRows(rows: WorkspaceFrameRow[], columns: WorkspaceFrameColumn[], column: number, direction: "asc" | "desc"): WorkspaceFrameRow[] {
  const factor = direction === "desc" ? -1 : 1
  const isValue = columns[column]?.value === true
  return rows.slice().sort((left, right) => {
    const first = left.cells[column], second = right.cells[column]
    if (!isValue) return (first?.text ?? "").localeCompare(second?.text ?? "") * factor || left.key.localeCompare(right.key)
    return compareValues(first?.value ?? null, second?.value ?? null, factor)
  })
}

/** workspaceFrameCSV 导出结果帧：表头由调用方给出（已本地化），键列给文本、数值列给原始读数。 */
export function workspaceFrameCSV(rows: WorkspaceFrameRow[], headers: string[]): string {
  const lines = [headers.map(workspaceCSVCell).join(",")]
  for (const row of rows) lines.push(row.cells.map((cell) => workspaceCSVCell(cell.text ?? (cell.value === undefined || cell.value === null ? "" : String(cell.value)))).join(","))
  return lines.join("\r\n")
}
