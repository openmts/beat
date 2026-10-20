import { panelColors, aggregations, limits, keyPattern, metricPattern, reservedKeys, record, text, finite, importBoundedInteger, importTimeToken, importLegendValues, importLegendSort, importTooltip, importPanelType, importPanelSize, importThresholds } from "@/lib/workspace-import-core"
import { importRowCollapse, importStatStyles, importGaugeThresholds, importPanelDescription, importPanelText, importSparkline, importPanelLinks, importCellDisplayMode, importTableOrientation, importPanelNoValue } from "@/lib/workspace-import-panel-extras"
import { importLineStyle, importBarChart, importBarGauge, importPieChart, importHeatmap, importHistogram, importGeoMap, importStateTimeline, importCalculation, importThresholdMode } from "@/lib/workspace-import-panel-fields"
import { importTransformations, importOverrides, importValueMappings } from "@/lib/workspace-import-rules"
import { maximumQueryPoints, minimumQueryPoints } from "@/lib/workspace-query"
import { panelTimeShiftError, panelTimeOverrideError } from "@/lib/workspace-time-override"
import { lineStyleError } from "@/lib/workspace-line-style"
import { legendPlacementError, legendPlacements, legendWidthError, maxLegendWidth, minLegendWidth } from "@/lib/workspace-legend-placement"
import { axisError } from "@/lib/workspace-axis"
import { axisScaleError } from "@/lib/workspace-axis-scale"
import { axisLabelError, validAxisLabel } from "@/lib/workspace-axis-label"
import { axisDisplayError, centeredZeroError } from "@/lib/workspace-axis-display"
import type { DashboardPanel, DashboardSeries, DashboardVariable, PanelType } from "@/types/metric-workspace"

export function importPanels(value: unknown[], variables: DashboardVariable[]): DashboardPanel[] {
  if (value.length > limits.panels) throw new Error("workspace.import_invalid")
  const panels: DashboardPanel[] = []
  for (const entry of value) {
    const panel = importPanel(entry, variables)
    if (panels.some((candidate) => candidate.id === panel.id)) throw new Error("workspace.import_invalid")
    panels.push(panel)
  }
  return panels
}

export function importPanel(value: unknown, variables: DashboardVariable[]): DashboardPanel {
  const source = record(value)
  if (!source) throw new Error("workspace.import_invalid")
  const id = text(source.id, limits.key), title = text(source.title, limits.title)
  const decimals = finite(source.decimals), min = finite(source.min), max = finite(source.max)
  if (!id || !keyPattern.test(id) || reservedKeys.includes(id) || !title) throw new Error("workspace.import_invalid")
  const type = importPanelType(source.type)
  const thresholds = importThresholds(source.thresholds)
  const lineStyle = importLineStyle(source, type)
  const panel: DashboardPanel = { id, title, type, size: importPanelSize(source.size), ...lineStyle, ...importBarGauge(source, type), ...importBarChart(source, type), ...importPieChart(source, type), ...importHeatmap(source, type), ...importHistogram(source, type), ...importGeoMap(source, type), ...importStateTimeline(source, type), ...importThresholdMode(source, type), ...importCalculation(source, type), ...importValueMappings(source, type), ...importTransformations(source),
    ...importRowCollapse(source, type),
    ...importPanelText(source, type),
    ...importPanelDescription(source),
    ...importPanelNoValue(source, type),
    ...importOverrides(source, type, lineStyle.stack, min ?? undefined, max ?? undefined),
    ...importStatStyles(source, type),
    ...importGaugeThresholds(source, type, thresholds),
    ...importSparkline(source, type),
    ...importCellDisplayMode(source, type),
    ...importTableOrientation(source, type),
    ...importPanelLinks(source, type),
    legend: source.legend === true, thresholds, series: importSeries(source.series, type, variables) }
  const legendValues = importLegendValues(source.legend_values), legendSort = importLegendSort(source.legend_sort), tooltip = importTooltip(source.tooltip)
  const placement = legendPlacements.find((candidate) => candidate === source.legend_placement)
  if (placement === undefined && source.legend_placement !== undefined && source.legend_placement !== "") throw new Error("workspace.import_invalid")
  if (!panel.legend && (legendValues.length > 0 || legendSort !== "none" || placement !== undefined)) throw new Error("workspace.import_invalid")
  if (legendValues.length > 0) panel.legend_values = legendValues
  if (legendSort !== "none") panel.legend_sort = legendSort
  if (placement !== undefined) {
    panel.legend_placement = placement
    if (legendPlacementError(panel)) throw new Error("workspace.import_invalid")
  }
  if (source.legend_width !== undefined && source.legend_width !== "") {
    panel.legend_width = importBoundedInteger(source.legend_width, minLegendWidth, maxLegendWidth)
    if (legendWidthError(panel)) throw new Error("workspace.import_invalid")
  }
  if (tooltip !== "all") panel.tooltip = tooltip
  if (typeof source.unit === "string" && source.unit.length <= 32) panel.unit = source.unit
  if (source.repeat !== undefined) {
    if (typeof source.repeat !== "string" || !variables.some((variable) => variable.name === source.repeat)) throw new Error("workspace.import_invalid")
    panel.repeat = source.repeat
  }
  if (decimals !== null && decimals >= 0 && decimals <= 6) panel.decimals = Math.round(decimals)
  if (min !== null && max !== null && min >= max) throw new Error("workspace.import_invalid")
  if (min !== null && max !== null) { panel.min = min; panel.max = max }
  importSoftAxes(source, panel)
  importAxisScale(source, panel)
  // 百分比堆叠与对数轴互斥：堆叠在面板字面量里导入、刻度在这里导入，因此导入后要再校验一次组合。
  if (lineStyleError(panel)) throw new Error("workspace.import_invalid")
  importAxisLabel(source, panel)
  importAxisDisplay(source, panel)
  importTimeOverride(source, panel)
  if (source.max_points !== undefined) panel.max_points = importBoundedInteger(source.max_points, minimumQueryPoints, maximumQueryPoints)
  if (source.time_shift !== undefined && source.time_shift !== "") {
    panel.time_shift = text(source.time_shift, 16) ?? ""
    if (!panel.time_shift || panelTimeShiftError(panel)) throw new Error("workspace.import_invalid")
  }
  return panel
}

/** importSoftAxes 只接受折线与柱状面板上的有限柔性上下限，规则与服务端 validateDashboardAxes 一致。 */
export function importSoftAxes(source: Record<string, unknown>, panel: DashboardPanel): void {
  const softMin = finite(source.soft_min), softMax = finite(source.soft_max)
  // finite 对 undefined 与非法值都返回 null，因此声明过字段却解析失败必须单独判定。
  if (source.soft_min !== undefined && softMin === null || source.soft_max !== undefined && softMax === null) throw new Error("workspace.import_invalid")
  if (softMin !== null) panel.soft_min = softMin
  if (softMax !== null) panel.soft_max = softMax
  if (axisError(panel)) throw new Error("workspace.import_invalid")
}

/** importAxisScale 只接受折线与柱状面板上的 log；linear 与未声明等效，导入时直接按缺省处理。 */
export function importAxisScale(source: Record<string, unknown>, panel: DashboardPanel): void {
  const scale = source.scale
  if (scale === undefined || scale === "" || scale === "linear") return
  if (scale !== "log") throw new Error("workspace.import_invalid")
  panel.scale = "log"
  if (axisScaleError(panel)) throw new Error("workspace.import_invalid")
}

/** importAxisLabel 只接受折线与柱状面板上的短标题；超长或含控制字符按整份文件无效处理。 */
export function importAxisLabel(source: Record<string, unknown>, panel: DashboardPanel): void {
  const label = source.axis_label
  if (label === undefined || label === "") return
  if (typeof label !== "string" || !validAxisLabel(label)) throw new Error("workspace.import_invalid")
  panel.axis_label = label
  if (axisLabelError(panel)) throw new Error("workspace.import_invalid")
}

/**
 * importAxisDisplay 只接受折线与柱状面板上的网格线开关与轴位置：缺省值（grid true、left）
 * 与未声明等效，导入时直接省略，避免把无意义的默认值写进收起后的定义。
 */
export function importAxisDisplay(source: Record<string, unknown>, panel: DashboardPanel): void {
  if (source.grid !== undefined) {
    if (typeof source.grid !== "boolean") throw new Error("workspace.import_invalid")
    if (!source.grid) panel.grid = false
  }
  if (source.centered_zero !== undefined) {
    if (typeof source.centered_zero !== "boolean") throw new Error("workspace.import_invalid")
    if (source.centered_zero) panel.centered_zero = true
  }
  const placement = source.axis_placement
  if (placement !== undefined && placement !== "" && placement !== "left" && placement !== "right") throw new Error("workspace.import_invalid")
  if (placement === "right") panel.axis_placement = "right"
  if (axisDisplayError(panel) ?? centeredZeroError(panel)) throw new Error("workspace.import_invalid")
}

/** 面板时间覆盖随看板导入导出；语法不合法的覆盖让整份文件按无效处理。 */
export function importTimeOverride(source: Record<string, unknown>, panel: DashboardPanel): void {
  const from = importTimeToken(source.time_from), to = importTimeToken(source.time_to)
  if (from === null || to === null) throw new Error("workspace.import_invalid")
  if (!from && !to) return
  if (from) panel.time_from = from
  if (to) panel.time_to = to
  if (panelTimeOverrideError(panel)) throw new Error("workspace.import_invalid")
}

export function importSeries(value: unknown, type: PanelType, variables: DashboardVariable[]): DashboardSeries[] {
  const list = Array.isArray(value) ? value : []
  // 行面板只做分组与折叠，文本面板只渲染 Markdown，两者都没有查询，因此必须是空序列；
  // 其余类型至少一条、至多 8 条。
  if (type === "row" || type === "text") {
    if (list.length > 0) throw new Error("workspace.import_invalid")
    return []
  }
  const single = type === "stat" || type === "gauge"
  if (list.length < 1 || list.length > limits.series || single && list.length !== 1) throw new Error("workspace.import_invalid")
  const series: DashboardSeries[] = []
  for (const entry of list) {
    const item = importSeriesValue(entry, variables)
    if (series.some((candidate) => candidate.ref_id === item.ref_id)) throw new Error("workspace.import_invalid")
    series.push(item)
  }
  return series
}

export function importSeriesValue(value: unknown, variables: DashboardVariable[]): DashboardSeries {
  const source = record(value)
  if (!source) throw new Error("workspace.import_invalid")
  const refID = text(source.ref_id, limits.key), metric = text(source.metric, limits.metric)
  const aggregation = aggregations.find((candidate) => candidate === source.aggregation)
  const color = panelColors.find((candidate) => candidate === source.color)
  const label = typeof source.label === "string" && source.label.length <= limits.label ? source.label : ""
  if (!refID || !keyPattern.test(refID) || reservedKeys.includes(refID) || !metric || !metricPattern.test(metric) || !aggregation || !color) throw new Error("workspace.import_invalid")
  const series: DashboardSeries = { ref_id: refID, node_id: importNode(source.node_id, variables), metric, aggregation, color, label }
  if (typeof source.device_id === "string" && source.device_id.length <= limits.key) series.device_id = source.device_id
  return series
}

export function importNode(value: unknown, variables: DashboardVariable[]): string {
  if (typeof value !== "string") throw new Error("workspace.import_invalid")
  if (!value.startsWith("$") && keyPattern.test(value)) return value
  if (value.startsWith("$") && variables.some((variable) => variable.name === value.slice(1))) return value
  throw new Error("workspace.import_invalid")
}
