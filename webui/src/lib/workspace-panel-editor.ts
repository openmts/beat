import { acceleratorSeriesError } from "@/lib/accelerator-metrics"
import { legendSortModes, legendValueKinds, maximumLegendValues, tooltipModes } from "@/lib/workspace-chart"
import { panelTimeOverrideError, panelTimeShiftError } from "@/lib/workspace-time-override"
import { axisError } from "@/lib/workspace-axis"
import { axisScaleError } from "@/lib/workspace-axis-scale"
import { axisLabelError } from "@/lib/workspace-axis-label"
import { axisDisplayError, centeredZeroError } from "@/lib/workspace-axis-display"
import { barChartError } from "@/lib/workspace-bar-chart"
import { barGaugeError } from "@/lib/workspace-bar-gauge"
import { thresholdDisplayError, thresholdModePatch } from "@/lib/workspace-threshold-display"
import { calculationError, calculationPatch } from "@/lib/workspace-calculation"
import { pieChartError } from "@/lib/workspace-pie-chart"
import { heatmapError, heatmapPatch } from "@/lib/workspace-heatmap"
import { emptyGeoLocation, geoError, geoPatch } from "@/lib/workspace-geomap"
import { histogramError, histogramPatch } from "@/lib/workspace-histogram"
import { stateTimelineError, stateTimelinePatch } from "@/lib/workspace-state-timeline"
import { lineStyleError, panelTypePatch } from "@/lib/workspace-line-style"
import { valueMappingError } from "@/lib/workspace-value-mapping"
import { transformationError } from "@/lib/workspace-transformations"
import { noValueError, overrideError, overrideTypePatch, noValuePatch } from "@/lib/workspace-overrides"
import { emptyRowPanel, isRowPanel, isTextPanel, rowPatch } from "@/lib/workspace-row"
import { emptyTextPanel, textPanelError, textPatch } from "@/lib/workspace-text"
import { sparklineError, sparklinePatch } from "@/lib/workspace-sparkline"
import { cellDisplayModeError, tableOrientationError, tablePatch } from "@/lib/workspace-table"
import { panelLinks, panelLinksError } from "@/lib/workspace-links"
import { panelDescriptionError } from "@/lib/workspace-panel-description"
import { legendPlacementError, legendWidthError, legendWidthPatch } from "@/lib/workspace-legend-placement"
import { statStyleError, statStylePatch } from "@/lib/workspace-stat"
import { gaugeThresholdError, gaugeThresholdPatch } from "@/lib/workspace-gauge"
import { maximumQueryPoints, minimumQueryPoints } from "@/lib/workspace-query"
import { randomUUID } from "@/lib/random-id"
import type { DashboardDefinition, DashboardPanel, DashboardSeries, DashboardVariable, MetricDefinition, PanelColor, PanelType } from "@/types/metric-workspace"

export function newWorkspacePanel(metrics: MetricDefinition[], definition: DashboardDefinition): DashboardPanel {
  const metric = metrics.find((value) => value.enabled && value.key === "cpu") ?? metrics.find((value) => value.enabled)
  return { id: randomUUID(), title: metric?.label ?? "", type: "timeseries", size: "medium", legend: true, tooltip: "all", unit: "auto", thresholds: [],
    series: [{ ref_id: "A", node_id: definition.variables[0] ? `$${definition.variables[0].name}` : "", metric: metric?.key ?? "", aggregation: metric?.default_aggregation ?? "avg", label: "", color: "blue" }] }
}

export function additionalWorkspaceSeries(panel: DashboardPanel): DashboardSeries {
  const ref = ["A", "B", "C", "D", "E", "F", "G", "H"].find((value) => !panel.series.some((series) => series.ref_id === value)) ?? "H"
  const colors: PanelColor[] = ["blue", "green", "amber", "rose", "violet", "cyan", "slate"]
  return { ...panel.series[0], ref_id: ref, label: "", color: colors[panel.series.length % colors.length] }
}

/** switchWorkspacePanelType 切换可视化类型：行面板与查询面板的字段集合不兼容，两侧都需要按新类型重置。 */
export function switchWorkspacePanelType(
  panel: DashboardPanel, type: PanelType, metrics: MetricDefinition[], definition: DashboardDefinition,
): DashboardPanel {
  const styling = { ...panelTypePatch(type, panel), ...overrideTypePatch(type, panel.overrides), ...noValuePatch(type), ...rowPatch(type), ...textPatch(type), ...sparklinePatch(type), ...statStylePatch(type), ...gaugeThresholdPatch(type), ...legendWidthPatch(type), ...thresholdModePatch(type, panel.threshold_mode),
    ...calculationPatch(type, panel.calculation), ...heatmapPatch(type), ...histogramPatch(type), ...stateTimelinePatch(type), ...geoPatch(type), ...tablePatch(type, panel) }
  if (type === "row") return emptyRowPanel({ ...panel, ...styling })
  if (type === "text") return emptyTextPanel({ ...panel, ...styling })
  // 行或文本面板没有序列，切回可视化类型时补一条与「新增面板」一致的默认查询。
  if (isRowPanel(panel) || isTextPanel(panel)) {
    return { ...newWorkspacePanel(metrics, definition), id: panel.id, title: panel.title, size: panel.size, type, ...(type === "geomap" ? { geo_locations: [emptyGeoLocation()] } : {}) }
  }
  // geo map 必须有地点表，切到该类型时先给一条空地点，避免保存时才发现少了必填项。
  const seed = type === "geomap" && !(panel.geo_locations?.length) ? { geo_locations: [emptyGeoLocation()] } : {}
  return { ...panel, ...styling, ...seed }
}

export function validateWorkspacePanel(panel: DashboardPanel, metrics: MetricDefinition[], variables: DashboardVariable[] = []): string | null {
  if (!panel.title.trim() || new TextEncoder().encode(panel.title).length > 120) return "workspace.invalid_panel"
  const descriptionError = panelDescriptionError(panel)
  if (descriptionError) return descriptionError
  const placementError = legendPlacementError(panel)
  if (placementError) return placementError
  const legendWidthFailure = legendWidthError(panel)
  if (legendWidthFailure) return legendWidthFailure
  const statError = statStyleError(panel)
  if (statError) return statError
  const gaugeError = gaugeThresholdError(panel)
  if (gaugeError) return gaugeError
  // 行面板只承载分组与折叠状态：没有查询，就不参与序列与可视化选项的校验。
  if (isRowPanel(panel)) return panel.repeat || panelLinks(panel).length ? "workspace.invalid_panel" : null
  // 文本面板同样不查询：只校验 Markdown 内容与数据链接。
  if (isTextPanel(panel)) return panel.repeat ? "workspace.invalid_panel" : textPanelError(panel) ?? panelLinksError(panelLinks(panel))
  if (panel.series.length < 1 || panel.series.length > 8) return "workspace.invalid_panel"
  if (panel.repeat && !variables.some((variable) => variable.name === panel.repeat)) return "workspace.repeat_unknown_variable"
  if ((panel.type === "stat" || panel.type === "gauge") && panel.series.length !== 1) return "workspace.single_series"
  if (panel.series.some((series) => !series.node_id || !metrics.some((metric) => metric.enabled && metric.key === series.metric && metric.aggregations.includes(series.aggregation)))) return "workspace.invalid_panel"
  const deviceError = panel.series.map(acceleratorSeriesError).find(Boolean)
  if (deviceError) return deviceError
  // 面板级时间覆盖的错误与字段覆盖同名会让导入的 overrideError 被遮蔽，这里用显式的本地名。
  const timeOverrideFailure = panelTimeOverrideError(panel)
  if (timeOverrideFailure) return timeOverrideFailure
  if (panel.max_points !== undefined && !validQueryPoints(panel.max_points)) return "workspace.invalid_points"
  const shiftError = panelTimeShiftError(panel)
  if (shiftError) return shiftError
  const styleError = lineStyleError(panel)
  if (styleError) return styleError
  const barError = barGaugeError(panel)
  if (barError) return barError
  const barChartFailure = barChartError(panel)
  if (barChartFailure) return barChartFailure
  const axisFailure = axisError(panel)
  if (axisFailure) return axisFailure
  const scaleFailure = axisScaleError(panel)
  if (scaleFailure) return scaleFailure
  const axisLabelFailure = axisLabelError(panel)
  if (axisLabelFailure) return axisLabelFailure
  const axisDisplayFailure = axisDisplayError(panel) ?? centeredZeroError(panel)
  if (axisDisplayFailure) return axisDisplayFailure
  const thresholdError = thresholdDisplayError(panel)
  if (thresholdError) return thresholdError
  const calculationFailure = calculationError(panel)
  if (calculationFailure) return calculationFailure
  const sparklineFailure = sparklineError(panel)
  if (sparklineFailure) return sparklineFailure
  const tableOrientationFailure = tableOrientationError(panel)
  if (tableOrientationFailure) return tableOrientationFailure
  const cellDisplayFailure = cellDisplayModeError(panel)
  if (cellDisplayFailure) return cellDisplayFailure
  const pieError = pieChartError(panel)
  if (pieError) return pieError
  const heatmapFailure = heatmapError(panel)
  if (heatmapFailure) return heatmapFailure
  const histogramFailure = histogramError(panel)
  if (histogramFailure) return histogramFailure
  const timelineFailure = stateTimelineError(panel)
  if (timelineFailure) return timelineFailure
  const geoFailure = geoError(panel)
  if (geoFailure) return geoFailure
  const mappingError = valueMappingError(panel)
  if (mappingError) return mappingError
  const transformationFailure = transformationError(panel)
  if (transformationFailure) return transformationFailure
  const overrideFailure = overrideError(panel)
  if (overrideFailure) return overrideFailure
  const noValueFailure = noValueError(panel)
  if (noValueFailure) return noValueFailure
  const linkFailure = panelLinksError(panelLinks(panel))
  if (linkFailure) return linkFailure
  const units = panel.series.map((series) => metrics.find((metric) => metric.key === series.metric)!.unit + (series.aggregation === "rate" ? "_per_second" : ""))
  if (new Set(units).size > 1 || panel.unit && panel.unit !== "auto" && panel.unit !== units[0]) return "workspace.unit_conflict"
  return validWorkspaceAxes(panel) && validWorkspaceLegend(panel) ? null : "workspace.invalid_panel"
}

function validWorkspaceLegend(panel: DashboardPanel): boolean {
  const values = panel.legend_values ?? [], sort = panel.legend_sort ?? "none", tooltip = panel.tooltip ?? "all"
  if (values.length > maximumLegendValues || new Set(values).size !== values.length) return false
  if (values.some((value) => !legendValueKinds.includes(value))) return false
  if (!legendSortModes.includes(sort) || !tooltipModes.includes(tooltip)) return false
  return panel.legend || values.length === 0 && sort === "none"
}

function validWorkspaceAxes(panel: DashboardPanel): boolean {
  if (panel.decimals !== undefined && (!Number.isInteger(panel.decimals) || panel.decimals < 0 || panel.decimals > 6)) return false
  if ([panel.min, panel.max].some((value) => value !== undefined && !Number.isFinite(value))) return false
  if (panel.min !== undefined && panel.max !== undefined && panel.min >= panel.max) return false
  return panel.thresholds.length <= 4 && panel.thresholds.every((threshold, index) => Number.isFinite(threshold.value) && (index === 0 || threshold.value > panel.thresholds[index - 1].value))
}

/** 面板点数必须落在服务端查询预算内；越界直接拦住保存。 */
function validQueryPoints(value: number): boolean {
  return Number.isInteger(value) && value >= minimumQueryPoints && value <= maximumQueryPoints
}
