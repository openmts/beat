import { panelColors, record, finite, importBoundedInteger, byteLength } from "@/lib/workspace-import-core"
import { barAlignments, barChartError, maxBarRadius, maxBarWidth, minBarRadius, minBarWidth } from "@/lib/workspace-bar-chart"
import { barDisplays, barGaugeError, barOrientations } from "@/lib/workspace-bar-gauge"
import { pieChartError, pieChartTypes, pieLabelModes } from "@/lib/workspace-pie-chart"
import { heatmapError, heatmapScales, maxHeatmapBuckets, minHeatmapBuckets } from "@/lib/workspace-heatmap"
import { histogramError, histogramScales, maxHistogramBuckets, minHistogramBuckets } from "@/lib/workspace-histogram"
import { stateTimelineError, timelineValueModes } from "@/lib/workspace-state-timeline"
import { geoError, geoLayers, geoMaxLatitude, maxGeoLabelBytes, maxGeoLocations, maxGeoMarkerSize, maxGeoMatchBytes, maxGeoZoom, minGeoMarkerSize, minGeoZoom } from "@/lib/workspace-geomap"
import { connectNullsModes, gradientModes, lineInterpolations, linePatterns, lineStyleError, maximumFillOpacity, maximumLineWidth, maximumPointSize, minimumLineWidth, minimumPointSize, showPointsModes, stackModes } from "@/lib/workspace-line-style"
import { thresholdDisplayError } from "@/lib/workspace-threshold-display"
import { calculationError } from "@/lib/workspace-calculation"
import type { DashboardGeoLocation, DashboardPanel, PanelCalculation, PanelType, ThresholdDisplayMode } from "@/types/metric-workspace"

/** 折线样式随看板导入导出：越界值、未知渐变或非 timeseries/barchart 面板上的设置都让整份文件按无效处理。 */
export function importLineStyle(source: Record<string, unknown>, type: string): Pick<DashboardPanel, "line_width" | "fill_opacity" | "show_points" | "point_size" | "stack" | "gradient_mode" | "line_interpolation" | "line_pattern" | "connect_nulls"> {
  const fields: Pick<DashboardPanel, "line_width" | "fill_opacity" | "show_points" | "point_size" | "stack" | "gradient_mode" | "line_interpolation" | "line_pattern" | "connect_nulls"> = {}
  if (source.line_width !== undefined) fields.line_width = importBoundedInteger(source.line_width, minimumLineWidth, maximumLineWidth)
  if (source.fill_opacity !== undefined) fields.fill_opacity = importBoundedInteger(source.fill_opacity, 0, maximumFillOpacity)
  const showPoints = showPointsModes.find((mode) => mode === source.show_points)
  if (showPoints) fields.show_points = showPoints
  else if (source.show_points !== undefined && source.show_points !== "") throw new Error("workspace.import_invalid")
  if (source.point_size !== undefined) fields.point_size = importBoundedInteger(source.point_size, minimumPointSize, maximumPointSize)
  const stack = stackModes.find((mode) => mode === source.stack)
  if (stack) fields.stack = stack
  else if (source.stack !== undefined && source.stack !== "") throw new Error("workspace.import_invalid")
  const gradient = gradientModes.find((mode) => mode === source.gradient_mode)
  if (gradient) fields.gradient_mode = gradient
  else if (source.gradient_mode !== undefined && source.gradient_mode !== "") throw new Error("workspace.import_invalid")
  const interpolation = lineInterpolations.find((mode) => mode === source.line_interpolation)
  if (interpolation) fields.line_interpolation = interpolation
  else if (source.line_interpolation !== undefined && source.line_interpolation !== "") throw new Error("workspace.import_invalid")
  const pattern = linePatterns.find((mode) => mode === source.line_pattern)
  if (pattern) fields.line_pattern = pattern
  else if (source.line_pattern !== undefined && source.line_pattern !== "") throw new Error("workspace.import_invalid")
  const connect = connectNullsModes.find((mode) => mode === source.connect_nulls)
  if (connect) fields.connect_nulls = connect
  else if (source.connect_nulls !== undefined && source.connect_nulls !== "") throw new Error("workspace.import_invalid")
  if (lineStyleError({ type, ...fields })) throw new Error("workspace.import_invalid")
  return fields
}

/** 柱状图随看板导入导出：越界值或非 barchart 面板上的设置都让整份文件按无效处理。 */
export function importBarChart(source: Record<string, unknown>, type: string): Pick<DashboardPanel, "bar_width" | "bar_radius" | "bar_alignment"> {
  const fields: Pick<DashboardPanel, "bar_width" | "bar_radius" | "bar_alignment"> = {}
  if (source.bar_width !== undefined) fields.bar_width = importBoundedInteger(source.bar_width, minBarWidth, maxBarWidth)
  if (source.bar_radius !== undefined) fields.bar_radius = importBoundedInteger(source.bar_radius, minBarRadius, maxBarRadius)
  if (source.bar_alignment !== undefined && source.bar_alignment !== "") {
    const alignment = barAlignments.find((value) => value === source.bar_alignment)
    if (alignment === undefined) throw new Error("workspace.import_invalid")
    // auto 与服务端的缺省等价，导入时直接收敛，避免配置文件留下无意义的差异。
    if (alignment !== "auto") fields.bar_alignment = alignment
  }
  if (barChartError({ type, ...fields })) throw new Error("workspace.import_invalid")
  return fields
}

/** 条状仪表随看板导入导出：未知枚举或非 bargauge 面板上的设置都让整份文件按无效处理。 */
export function importBarGauge(source: Record<string, unknown>, type: string): Pick<DashboardPanel, "bar_orientation" | "bar_display" | "bar_unfilled"> {
  const fields: Pick<DashboardPanel, "bar_orientation" | "bar_display" | "bar_unfilled"> = {}
  const orientation = barOrientations.find((value) => value === source.bar_orientation)
  if (orientation) fields.bar_orientation = orientation
  else if (source.bar_orientation !== undefined && source.bar_orientation !== "") throw new Error("workspace.import_invalid")
  const display = barDisplays.find((value) => value === source.bar_display)
  if (display) fields.bar_display = display
  else if (source.bar_display !== undefined && source.bar_display !== "") throw new Error("workspace.import_invalid")
  if (source.bar_unfilled !== undefined) {
    if (typeof source.bar_unfilled !== "boolean") throw new Error("workspace.import_invalid")
    fields.bar_unfilled = source.bar_unfilled
  }
  if (barGaugeError({ type, ...fields })) throw new Error("workspace.import_invalid")
  return fields
}

/** 饼图随看板导入导出：未知枚举或非 piechart 面板上的设置都让整份文件按无效处理。 */
export function importPieChart(source: Record<string, unknown>, type: string): Pick<DashboardPanel, "pie_chart_type" | "pie_label_mode"> {
  const fields: Pick<DashboardPanel, "pie_chart_type" | "pie_label_mode"> = {}
  const chartType = pieChartTypes.find((value) => value === source.pie_chart_type)
  if (chartType) fields.pie_chart_type = chartType
  else if (source.pie_chart_type !== undefined && source.pie_chart_type !== "") throw new Error("workspace.import_invalid")
  const labelMode = pieLabelModes.find((value) => value === source.pie_label_mode)
  if (labelMode) fields.pie_label_mode = labelMode
  else if (source.pie_label_mode !== undefined && source.pie_label_mode !== "") throw new Error("workspace.import_invalid")
  if (pieChartError({ type, ...fields })) throw new Error("workspace.import_invalid")
  return fields
}

/** 热力图随看板导入导出：未知枚举或非 heatmap 面板上的设置都让整份文件按无效处理。 */
export function importHeatmap(source: Record<string, unknown>, type: string): Pick<DashboardPanel, "heatmap_buckets" | "heatmap_scale" | "heatmap_color"> {
  const fields: Pick<DashboardPanel, "heatmap_buckets" | "heatmap_scale" | "heatmap_color"> = {}
  if (source.heatmap_buckets !== undefined) fields.heatmap_buckets = importBoundedInteger(source.heatmap_buckets, minHeatmapBuckets, maxHeatmapBuckets)
  const scale = heatmapScales.find((value) => value === source.heatmap_scale)
  if (scale) fields.heatmap_scale = scale
  else if (source.heatmap_scale !== undefined && source.heatmap_scale !== "") throw new Error("workspace.import_invalid")
  const color = panelColors.find((value) => value === source.heatmap_color)
  if (color) fields.heatmap_color = color
  else if (source.heatmap_color !== undefined && source.heatmap_color !== "") throw new Error("workspace.import_invalid")
  if (heatmapError({ type, ...fields })) throw new Error("workspace.import_invalid")
  return fields
}

/** 直方图随看板导入导出：未知枚举或非 histogram 面板上的设置都让整份文件按无效处理。 */
export function importHistogram(source: Record<string, unknown>, type: string): Pick<DashboardPanel, "histogram_buckets" | "histogram_scale" | "histogram_color" | "histogram_merge"> {
  const fields: Pick<DashboardPanel, "histogram_buckets" | "histogram_scale" | "histogram_color" | "histogram_merge"> = {}
  if (source.histogram_buckets !== undefined) fields.histogram_buckets = importBoundedInteger(source.histogram_buckets, minHistogramBuckets, maxHistogramBuckets)
  const scale = histogramScales.find((value) => value === source.histogram_scale)
  if (scale) fields.histogram_scale = scale
  else if (source.histogram_scale !== undefined && source.histogram_scale !== "") throw new Error("workspace.import_invalid")
  const color = panelColors.find((value) => value === source.histogram_color)
  if (color) fields.histogram_color = color
  else if (source.histogram_color !== undefined && source.histogram_color !== "") throw new Error("workspace.import_invalid")
  if (source.histogram_merge !== undefined) {
    if (typeof source.histogram_merge !== "boolean") throw new Error("workspace.import_invalid")
    fields.histogram_merge = source.histogram_merge
  }
  if (histogramError({ type, ...fields })) throw new Error("workspace.import_invalid")
  return fields
}

/** geo map 随看板导入导出：地点表、图层、视口与底图模板不合法都让整份文件按无效处理。 */
export function importGeoMap(source: Record<string, unknown>, type: string): Pick<DashboardPanel, "geo_locations" | "geo_layer" | "geo_tile_url" |
  "geo_zoom" | "geo_center_latitude" | "geo_center_longitude" | "geo_marker_size"> {
  const fields: Pick<DashboardPanel, "geo_locations" | "geo_layer" | "geo_tile_url" | "geo_zoom" |
    "geo_center_latitude" | "geo_center_longitude" | "geo_marker_size"> = {}
  if (source.geo_locations !== undefined) {
    if (!Array.isArray(source.geo_locations) || source.geo_locations.length > maxGeoLocations) throw new Error("workspace.import_invalid")
    fields.geo_locations = source.geo_locations.map(importGeoLocation)
  }
  const layer = geoLayers.find((value) => value === source.geo_layer)
  if (layer) fields.geo_layer = layer
  else if (source.geo_layer !== undefined && source.geo_layer !== "") throw new Error("workspace.import_invalid")
  if (source.geo_tile_url !== undefined && source.geo_tile_url !== "") {
    if (typeof source.geo_tile_url !== "string") throw new Error("workspace.import_invalid")
    fields.geo_tile_url = source.geo_tile_url
  }
  if (source.geo_zoom !== undefined) fields.geo_zoom = importBoundedInteger(source.geo_zoom, minGeoZoom, maxGeoZoom)
  if (source.geo_marker_size !== undefined) fields.geo_marker_size = importBoundedInteger(source.geo_marker_size, minGeoMarkerSize, maxGeoMarkerSize)
  const centerLatitude = geoCoordinate(source.geo_center_latitude, geoMaxLatitude), centerLongitude = geoCoordinate(source.geo_center_longitude, 180)
  if ((centerLatitude === null) !== (centerLongitude === null)) throw new Error("workspace.import_invalid")
  if (centerLatitude !== null && centerLongitude !== null) { fields.geo_center_latitude = centerLatitude; fields.geo_center_longitude = centerLongitude }
  if (geoError({ type, ...fields })) throw new Error("workspace.import_invalid")
  return fields
}

/** importGeoLocation 收敛单条地点：匹配串、显示名与坐标必须满足服务端同样的边界。 */
export function importGeoLocation(value: unknown): DashboardGeoLocation {
  const source = record(value)
  const match = source && typeof source.match === "string" ? source.match.trim() : ""
  const label = source && typeof source.label === "string" ? source.label : ""
  const latitude = geoCoordinate(source?.latitude, geoMaxLatitude), longitude = geoCoordinate(source?.longitude, 180)
  if (!source || !match || byteLength(match) > maxGeoMatchBytes || byteLength(label) > maxGeoLabelBytes || latitude === null || longitude === null) throw new Error("workspace.import_invalid")
  return { match, ...(label ? { label } : {}), latitude, longitude }
}

/** geoCoordinate 只接受范围内的有限数值；缺省或越界都返回 null 交给调用方裁决。 */
export function geoCoordinate(value: unknown, limit: number): number | null {
  const coordinate = finite(value)
  if (coordinate === null || Math.abs(coordinate) > limit) return null
  return coordinate
}

/** 状态时间线随看板导入导出：未知枚举或非 statetimeline 面板上的设置都让整份文件按无效处理。 */
export function importStateTimeline(source: Record<string, unknown>, type: string): Pick<DashboardPanel, "timeline_merge" | "timeline_show_values"> {
  const fields: Pick<DashboardPanel, "timeline_merge" | "timeline_show_values"> = {}
  if (source.timeline_merge !== undefined) {
    if (typeof source.timeline_merge !== "boolean") throw new Error("workspace.import_invalid")
    fields.timeline_merge = source.timeline_merge
  }
  const mode = timelineValueModes.find((value) => value === source.timeline_show_values)
  if (mode) fields.timeline_show_values = mode
  else if (source.timeline_show_values !== undefined && source.timeline_show_values !== "") throw new Error("workspace.import_invalid")
  if (stateTimelineError({ type, ...fields })) throw new Error("workspace.import_invalid")
  return fields
}

/** importCalculation 只接受服务端认可的枚举；未声明或 last 视为缺省，不写入字段。 */
export function importCalculation(source: Record<string, unknown>, type: PanelType): Pick<DashboardPanel, "calculation"> {
  const calculation = source.calculation
  if (calculation === undefined || calculation === "" || calculation === "last") return {}
  if (typeof calculation !== "string" || calculationError({ type, calculation })) throw new Error("workspace.import_invalid")
  return { calculation: calculation as PanelCalculation }
}

/** importThresholdMode 只接受服务端认可的枚举；未声明或 line 视为缺省，不写入字段。 */
export function importThresholdMode(source: Record<string, unknown>, type: PanelType): Pick<DashboardPanel, "threshold_mode"> {
  const mode = source.threshold_mode
  if (mode === undefined || mode === "" || mode === "line") return {}
  if (typeof mode !== "string" || thresholdDisplayError({ type, thresholds: [], threshold_mode: mode })) throw new Error("workspace.import_invalid")
  return { threshold_mode: mode as ThresholdDisplayMode }
}
