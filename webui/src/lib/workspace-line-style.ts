import { softAxisPatch } from "@/lib/workspace-axis"
import { axisScalePatch } from "@/lib/workspace-axis-scale"
import { axisLabelPatch } from "@/lib/workspace-axis-label"
import { axisDisplayPatch } from "@/lib/workspace-axis-display"
import { barChartPatch } from "@/lib/workspace-bar-chart"
import { barGaugePatch } from "@/lib/workspace-bar-gauge"
import { pieChartPatch } from "@/lib/workspace-pie-chart"
import { heatmapPatch } from "@/lib/workspace-heatmap"
import { histogramPatch } from "@/lib/workspace-histogram"
import { stateTimelinePatch } from "@/lib/workspace-state-timeline"
import type { DashboardPanel, PanelType } from "@/types/metric-workspace"

/** 与服务端 internal/model 同规则：线宽 1..4、填充 0..100%、数据点 auto/always/never、堆叠 none/normal。 */
export const minimumLineWidth = 1
export const maximumLineWidth = 4
export const defaultLineWidth = 2
export const maximumFillOpacity = 100
export const minimumPointSize = 1
export const maximumPointSize = 12
/** 未显式设置点大小时渲染器沿用的既有半径：always 比 auto 略大，保持历史观感。 */
export const automaticPointRadius = { auto: 1.8, always: 2.6 } as const
export const showPointsModes = ["auto", "always", "never"] as const
/** 堆叠模式：none 不堆叠、normal 按原始值堆叠、percent 把每个时间点归一化成 100%。 */
export const stackModes = ["none", "normal", "percent"] as const
export const gradientModes = ["none", "opacity", "hue"] as const
export const lineInterpolations = ["linear", "smooth", "step_before", "step_after"] as const
export const linePatterns = ["solid", "dashed", "dotted"] as const
export const connectNullsModes = ["none", "always"] as const
/** 打开堆叠时使用的默认填充：不透明的堆叠面积看不出层次，因此给一个可读的起点。 */
export const stackedFillOpacity = 10
/** 会真正堆叠起来的模式：百分比堆叠同样要求可见填充，否则面积/柱子不可见。 */
export function stacksSeries(stack: StackMode): boolean {
  return stack === "normal" || stack === "percent"
}
/** 打开渐变时的默认填充：0% 填充下渐变不可见，因此同样给一个可读的起点。 */
export const gradientFillOpacity = 20

export type ShowPoints = typeof showPointsModes[number]
export type StackMode = typeof stackModes[number]
export type GradientMode = typeof gradientModes[number]
export type LineInterpolation = typeof lineInterpolations[number]
export type LinePattern = typeof linePatterns[number]
export type ConnectNullsMode = typeof connectNullsModes[number]

export interface PanelLineStyle {
  lineWidth: number; fillOpacity: number; showPoints: ShowPoints; pointSize: number | null; stack: StackMode
  gradientMode: GradientMode; interpolation: LineInterpolation; pattern: LinePattern; connectNulls: ConnectNullsMode
}

/** 折线样式的可编辑子集：编辑器与导入导出都只碰这几个字段，导入时类型还是原始字符串。 */
export interface LineStyleFields {
  type: string
  line_width?: number
  fill_opacity?: number
  show_points?: string
  point_size?: number
  stack?: string
  gradient_mode?: string
  line_interpolation?: string
  line_pattern?: string
  connect_nulls?: string
  /** 百分比堆叠与对数轴互斥，校验需要同时看到两个字段。 */
  scale?: string
}

/** panelLineStyle 解析生效值，缺省与服务端一致：线宽 2、无填充、自动数据点、不堆叠。
 *  柱状图的填充缺省是实心（100%），否则「一条不透明的柱子」会被渲染成看不见的空框。 */
export function panelLineStyle(panel: LineStyleFields): PanelLineStyle {
  return {
    lineWidth: validLineWidth(panel.line_width) ? (panel.line_width as number) : defaultLineWidth,
    fillOpacity: validFillOpacity(panel.fill_opacity) ? (panel.fill_opacity as number) : (panel.type === "barchart" ? maximumFillOpacity : 0),
    showPoints: showPointsModes.find((mode) => mode === panel.show_points) ?? "auto",
    pointSize: validPointSize(panel.point_size) ? (panel.point_size as number) : null,
    stack: stackModes.find((mode) => mode === panel.stack) ?? "none",
    gradientMode: gradientModes.find((mode) => mode === panel.gradient_mode) ?? "none",
    interpolation: lineInterpolations.find((mode) => mode === panel.line_interpolation) ?? "linear",
    pattern: linePatterns.find((mode) => mode === panel.line_pattern) ?? "solid",
    connectNulls: connectNullsModes.find((mode) => mode === panel.connect_nulls) ?? "none",
  }
}

/** lineStyleError 返回目录键，null 表示可以保存；规则与服务端一致。 */
export function lineStyleError(panel: LineStyleFields): string | null {
  const styled = panel.line_width !== undefined || panel.fill_opacity !== undefined || Boolean(panel.show_points) ||
    panel.point_size !== undefined || Boolean(panel.stack) || Boolean(panel.gradient_mode) ||
    Boolean(panel.line_interpolation) || Boolean(panel.line_pattern) || Boolean(panel.connect_nulls)
  if (styled && panel.type === "barchart" &&
    (panel.line_width !== undefined || Boolean(panel.show_points) || panel.point_size !== undefined ||
      Boolean(panel.line_interpolation) || Boolean(panel.line_pattern) || Boolean(panel.connect_nulls))) return "workspace.line_style_line_only"
  if (styled && panel.type !== "timeseries" && panel.type !== "barchart") return "workspace.line_style_timeseries_only"
  if (panel.line_width !== undefined && !validLineWidth(panel.line_width)) return "workspace.line_width_invalid"
  if (panel.fill_opacity !== undefined && !validFillOpacity(panel.fill_opacity)) return "workspace.fill_opacity_invalid"
  if (panel.show_points && !showPointsModes.includes(panel.show_points as ShowPoints)) return "workspace.show_points_invalid"
  if (panel.stack && !stackModes.includes(panel.stack as StackMode)) return "workspace.stack_invalid"
  // 百分比堆叠固定 0..100%，对数轴既放不下 0 也没有意义，与服务端同规则直接拒绝。
  if (panelLineStyle(panel).stack === "percent" && panel.scale === "log") return "workspace.stack_percent_linear_axis"
  if (panel.gradient_mode && !gradientModes.includes(panel.gradient_mode as GradientMode)) return "workspace.gradient_mode_invalid"
  if (panel.line_interpolation && !lineInterpolations.includes(panel.line_interpolation as LineInterpolation)) return "workspace.line_interpolation_invalid"
  if (panel.line_pattern && !linePatterns.includes(panel.line_pattern as LinePattern)) return "workspace.line_pattern_invalid"
  if (panel.connect_nulls && !connectNullsModes.includes(panel.connect_nulls as ConnectNullsMode)) return "workspace.connect_nulls_invalid"
  if (panel.point_size !== undefined && !validPointSize(panel.point_size)) return "workspace.point_size_invalid"
  // 与「拒绝渲染器会忽略的配置」同一口径：不画点时点大小没有效果。
  if (panel.type === "timeseries" && panel.point_size !== undefined && panelLineStyle(panel).showPoints === "never") return "workspace.point_size_requires_points"
  if (panelGradientApplies(panel)) return "workspace.gradient_requires_fill"
  return null
}

/** panelGradientApplies 判断渐变是否真的会渲染：没有填充就没有渐变可画，与服务端同规则。
 *  折线缺省不填充，柱状图缺省实心，因此这里复用 panelLineStyle 的缺省口径。 */
function panelGradientApplies(panel: LineStyleFields): boolean {
  const mode = gradientModes.find((value) => value === panel.gradient_mode)
  return mode !== undefined && mode !== "none" && panelLineStyle(panel).fillOpacity === 0
}

function validLineWidth(value: number | undefined): boolean {
  return value !== undefined && Number.isInteger(value) && value >= minimumLineWidth && value <= maximumLineWidth
}

function validFillOpacity(value: number | undefined): boolean {
  return value !== undefined && Number.isInteger(value) && value >= 0 && value <= maximumFillOpacity
}

function validPointSize(value: number | undefined): boolean {
  return value !== undefined && Number.isInteger(value) && value >= minimumPointSize && value <= maximumPointSize
}

/** pointRadius 解析数据点的渲染半径：显式 point_size 优先，否则沿用渲染器的 auto/always 口径。 */
export function pointRadius(style: PanelLineStyle): number {
  if (style.pointSize !== null) return style.pointSize
  return style.showPoints === "always" ? automaticPointRadius.always : automaticPointRadius.auto
}

/** showPointsPatch 切到 never 时一并清掉点大小：服务端会拒绝「有点大小却不画点」的组合。 */
export function showPointsPatch(panel: LineStyleFields, mode: string): Pick<DashboardPanel, "show_points" | "point_size"> {
  const showPoints = showPointsModes.find((value) => value === mode) ?? "auto"
  return {
    show_points: showPoints === "auto" ? undefined : showPoints,
    point_size: showPoints === "never" ? undefined : panel.point_size,
  }
}

/** panelTypePatch 切换面板类型时同步清理只对折线、条状仪表或饼图有效的字段，避免保存出服务端会拒绝的组合。 */
export function panelTypePatch(type: PanelType, panel?: LineStyleFields): Pick<DashboardPanel, "type" | "line_width" | "fill_opacity" | "show_points" | "point_size" | "stack" | "gradient_mode" | "line_interpolation" | "line_pattern" | "connect_nulls">
  & Partial<Pick<DashboardPanel, "bar_orientation" | "bar_display" | "bar_unfilled" | "bar_width" | "bar_radius" | "soft_min" | "soft_max" | "scale" | "axis_label" | "grid" | "axis_placement" | "centered_zero" | "pie_chart_type" | "pie_label_mode" | "timeline_merge" | "timeline_show_values" | "histogram_buckets" | "histogram_scale" | "histogram_color" | "histogram_merge">> {
  const bars = barGaugePatch(type), barCharts = barChartPatch(type), axes = { ...softAxisPatch(type), ...axisScalePatch(type), ...axisLabelPatch(type), ...axisDisplayPatch(type) }, pies = pieChartPatch(type), heatmapFields = heatmapPatch(type), timelineFields = stateTimelinePatch(type), histogramFields = histogramPatch(type)
  if (type === "timeseries") return { type, ...bars, ...barCharts, ...axes, ...pies, ...heatmapFields, ...timelineFields, ...histogramFields }
  // 柱状图与折线共用填充与堆叠，因此切到柱状图时保留这两个字段，只清掉它渲染不了的线宽与数据点。
  if (type === "barchart") return { type, line_width: undefined, show_points: undefined, point_size: undefined, line_interpolation: undefined, line_pattern: undefined, connect_nulls: undefined, fill_opacity: panel?.fill_opacity, stack: stackModes.find((mode) => mode === panel?.stack), gradient_mode: keepGradient(panel), ...bars, ...barCharts, ...axes, ...pies, ...heatmapFields, ...timelineFields, ...histogramFields }
  return { type, line_width: undefined, fill_opacity: undefined, show_points: undefined, point_size: undefined, stack: undefined, gradient_mode: undefined, line_interpolation: undefined, line_pattern: undefined, connect_nulls: undefined, ...bars, ...barCharts, ...axes, ...pies, ...heatmapFields, ...timelineFields, ...histogramFields }
}

/** keepGradient 在切到柱状图时保留渐变：柱状图同样画填充，因此渐变继续有效；切到其它类型则清掉。 */
function keepGradient(panel?: LineStyleFields): GradientMode | undefined {
  return gradientModes.find((mode) => mode === panel?.gradient_mode && mode !== "none")
}

/** gradientPatch 打开渐变时补上可读的默认填充（柱状图缺省已实心，因此只在 0% 时补）；
 *  选回 none 时直接省略字段，避免保存出与服务端缺省等价的冗余配置。 */
export function gradientPatch(panel: LineStyleFields, mode: string): Pick<DashboardPanel, "gradient_mode" | "fill_opacity"> {
  const gradient = gradientModes.find((value) => value === mode)
  if (gradient === undefined || gradient === "none") return { gradient_mode: undefined }
  return panelLineStyle(panel).fillOpacity > 0 ? { gradient_mode: gradient } : { gradient_mode: gradient, fill_opacity: gradientFillOpacity }
}

/** linePatternDash 把线型映射成 SVG 虚线数组：solid 不设，dashed/dotted 给渲染器可直接使用的短数组。 */
export function linePatternDash(pattern: LinePattern): string | undefined {
  if (pattern === "dashed") return "6 4"
  if (pattern === "dotted") return "1 3"
  return undefined
}

/** connectNullsPatch 选回 none 时省略字段：它与服务端缺省等价，留着只会制造无意义的差异。 */
export function connectNullsPatch(mode: string): Pick<DashboardPanel, "connect_nulls"> {
  const value = connectNullsModes.find((candidate) => candidate === mode)
  return value === undefined || value === "none" ? { connect_nulls: undefined } : { connect_nulls: value }
}

/** linePatternPatch 选回 solid 时省略字段：它与服务端缺省等价，留着只会制造无意义的差异。 */
export function linePatternPatch(pattern: string): Pick<DashboardPanel, "line_pattern"> {
  const value = linePatterns.find((mode) => mode === pattern)
  return value === undefined || value === "solid" ? { line_pattern: undefined } : { line_pattern: value }
}

/** interpolationPatch 选回 linear 时省略字段：它与服务端缺省等价，留着只会制造无意义的差异。 */
export function interpolationPatch(mode: string): Pick<DashboardPanel, "line_interpolation"> {
  const interpolation = lineInterpolations.find((value) => value === mode)
  return interpolation === undefined || interpolation === "linear"
    ? { line_interpolation: undefined }
    : { line_interpolation: interpolation }
}

/** interpolationCurve 把插值方式映射到渲染器的曲线类型；只使用内置算法，不接受自定义路径。 */
export function interpolationCurve(mode: LineInterpolation): "linear" | "monotone" | "stepBefore" | "stepAfter" {
  if (mode === "smooth") return "monotone"
  if (mode === "step_before") return "stepBefore"
  if (mode === "step_after") return "stepAfter"
  return "linear"
}

/** stackPatch 打开堆叠时补上可读的默认填充；关闭堆叠只清除堆叠本身。 */
export function stackPatch(panel: LineStyleFields, mode: string): Pick<DashboardPanel, "stack" | "fill_opacity"> {
  const value = stackModes.find((candidate) => candidate === mode)
  if (value === undefined || value === "none") return { stack: undefined }
  const fill = panel.fill_opacity ?? (panel.type === "barchart" ? maximumFillOpacity : 0)
  return fill > 0 ? { stack: value } : { stack: value, fill_opacity: stackedFillOpacity }
}
