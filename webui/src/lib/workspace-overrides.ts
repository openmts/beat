// 字段覆盖的解析层：把面板里的覆盖规则套到一条序列上，得到颜色、单位、图例可见性、
// 线型、轴选项与 Standard options（显示名、小数位、值域）。
// 规则口径见 ./workspace-override-contract.ts，编辑器与导入导出的校验见 ./workspace-override-validation.ts。
import { panelColors, workspaceSeriesLabel } from "./workspace-chart"
import { maximumFillOpacity, maximumLineWidth, minimumLineWidth } from "./workspace-line-style"
import type { AxisSide } from "./workspace-axis-sides"
import { thresholdColorDisabled, type ThresholdDisplayFields } from "./workspace-threshold-display"
import { overrideSeriesTypeMatches, overrideSeriesValueMatches, overrideMatcherTypeList, type OverrideFields, type OverridePanelFields } from "./workspace-override-contract"
import type {
  DashboardOverride, DashboardOverrideMatcher, ExpandedSeries, MetricResultSeries, OverrideMatcherType, PanelColor,
} from "@/types/metric-workspace"

export * from "./workspace-override-contract"
export * from "./workspace-override-validation"

/** 覆盖匹配读序列的标签与 ref_id；「按字段类型」「按值」还需要单位与采样点，
 *  这两项在只要标签的调用点可以缺省，缺省时那两种匹配方式不命中。 */
export type OverridableSeries = Pick<MetricResultSeries, "label" | "ref_id"> &
  Partial<Pick<MetricResultSeries, "unit" | "points">>


/** 命中后真正生效的显示属性：未覆盖的属性保持 undefined，调用方沿用面板或序列默认值。 */
export interface SeriesOverride {
  color?: PanelColor
  unit?: string
  hideLegend?: boolean
  /** 覆盖后的折线宽度（1..4）；undefined 表示沿用面板线宽。 */
  lineWidth?: number
  /** 覆盖后的填充不透明度（0..100，百分比）；undefined 表示沿用面板填充。 */
  fillOpacity?: number
  /** 覆盖后的值轴位置；undefined 表示沿用面板声明的轴位置。 */
  axisPlacement?: AxisSide
  /** 覆盖后的阈值色带；undefined 表示沿用面板阈值。 */
  thresholds?: { value: number; color: PanelColor }[]
  /** 覆盖后的填充目标序列名称；undefined 表示填充到坐标轴。 */
  fillBelowTo?: string
  /** 覆盖后的值轴标题；undefined 表示沿用面板级轴标题。 */
  axisLabel?: string
  /** 覆盖后的值轴柔性上下限；undefined 表示沿用面板级设置。 */
  axisSoftMin?: number
  axisSoftMax?: number
  /** 覆盖后的值轴宽度；undefined 表示按单位与轴标题自动计算。 */
  axisWidth?: number
  /** 覆盖后的显示名（Grafana 的 Display name）；undefined 表示沿用序列标签。 */
  displayName?: string
  /** 覆盖后的小数位数（0..6）；undefined 表示沿用面板级 decimals。 */
  decimals?: number
  /** 覆盖后的值域（Grafana 的 Min / Max）；undefined 表示沿用面板级上下限。 */
  min?: number
  max?: number
}

/** 逐序列轴选项：一根值轴上命中的覆盖按顺序叠加后的结果。 */
export interface AxisOverrideOptions {
  label?: string
  softMin?: number
  softMax?: number
  /** 逐序列轴宽（Grafana 的 Overrides → Axis → Width）；0 与缺省一样表示自动。 */
  width?: number
}

/**
 * 覆盖按数组顺序叠加，后面的覆盖赢；坏正则视为不命中，不制造二次损坏。
 * 覆盖只影响显示，因此这里不排序也不过滤——被隐藏图例的序列仍然绘制。
 */
export function seriesOverride(panel: OverrideFields | undefined, series: OverridableSeries): SeriesOverride {
  const overrides = panel?.overrides ?? []
  const applied: SeriesOverride = {}
  for (const override of overrides) {
    if (!overrideMatches(override.matcher, series)) continue
    if (override.color) applied.color = override.color
    if (override.unit) applied.unit = override.unit
    if (override.hide_legend !== undefined) applied.hideLegend = override.hide_legend
    if (override.line_width !== undefined) applied.lineWidth = override.line_width
    if (override.fill_opacity !== undefined) applied.fillOpacity = override.fill_opacity
    if (override.axis_placement !== undefined) applied.axisPlacement = override.axis_placement
    if (override.thresholds?.length) applied.thresholds = override.thresholds
    if (override.fill_below_to) applied.fillBelowTo = override.fill_below_to
    if (override.axis_label) applied.axisLabel = override.axis_label
    if (override.axis_soft_min !== undefined) applied.axisSoftMin = override.axis_soft_min
    if (override.axis_soft_max !== undefined) applied.axisSoftMax = override.axis_soft_max
    if (override.axis_width !== undefined) applied.axisWidth = override.axis_width
    if (override.display_name) applied.displayName = override.display_name
    if (override.decimals !== undefined) applied.decimals = override.decimals
    if (override.min !== undefined) applied.min = override.min
    if (override.max !== undefined) applied.max = override.max
  }
  return applied
}

/**
 * workspaceSideAxisOptions 解析一根值轴上的逐序列轴选项（Grafana 的 Overrides → Axis → Label /
 * Soft min / Soft max）：命中该侧任意序列的覆盖按顺序叠加，后面的赢；没有任何覆盖命中时返回空对象，
 * 调用方沿用面板级轴设置。
 */
export function workspaceSideAxisOptions(panel: OverrideFields | undefined, members: OverridableSeries[]): AxisOverrideOptions {
  const options: AxisOverrideOptions = {}
  for (const override of panel?.overrides ?? []) {
    if (!members.some((member) => overrideMatches(override.matcher, member))) continue
    if (override.axis_label) options.label = override.axis_label
    if (override.axis_soft_min !== undefined) options.softMin = override.axis_soft_min
    if (override.axis_soft_max !== undefined) options.softMax = override.axis_soft_max
    if (override.axis_width !== undefined) options.width = override.axis_width
  }
  return options
}

/**
 * 命中判定：by_name 精确匹配标签，by_regex 用正则匹配标签（非法正则不命中），by_ref_id 精确匹配
 * 查询序列 id，by_type 用序列的单位族匹配（Grafana 的 Fields with type），by_value 匹配最新样本
 * 等于给定数值的序列（Grafana 的 Fields with values）。
 */
export function overrideMatches(matcher: DashboardOverrideMatcher, series: OverridableSeries): boolean {
  if (matcher.type === "by_name") return series.label === matcher.value
  if (matcher.type === "by_ref_id") return series.ref_id === matcher.value
  if (matcher.type === "by_type") return overrideSeriesTypeMatches(matcher.value, series)
  if (matcher.type === "by_value") return overrideSeriesValueMatches(matcher.value, series)
  let expression: RegExp
  try {
    expression = new RegExp(matcher.value)
  } catch (_) {
    void _
    return false
  }
  return expression.test(series.label)
}

/** 调色板顺序：派生序列（二元运算、group_by 聚合结果）没有声明颜色时按渲染顺序轮流取色。 */
const paletteOrder: PanelColor[] = ["blue", "green", "amber", "rose", "violet", "cyan", "slate"]

/**
 * workspaceSeriesColor 解析序列的最终颜色：字段覆盖 > 变量展开后的颜色 > 面板里声明的颜色 > 调色板缺省；
 * 值映射仍然由调用方优先处理，因此这里不碰映射。index 只在序列没有任何声明颜色时参与取色，
 * 让同一面板里的派生序列不会都落成同一个颜色。
 */
export function workspaceSeriesColor(panel: OverridePanelFields | undefined, series: MetricResultSeries, expanded?: ExpandedSeries[], index = 0): string {
  const override = seriesOverride(panel, series).color
  if (override) return panelColors[override]
  const expandedColor = expanded?.find((entry) => entry.ref_id === series.ref_id)?.color
  const declared = panel?.series?.find((entry) => entry.ref_id === series.ref_id)?.color
  return panelColors[expandedColor ?? declared ?? paletteOrder[index % paletteOrder.length]]
}

/** workspaceSeriesAxisPlacement 解析序列最终落在哪根值轴上：覆盖优先，其次面板声明的轴位置（缺省左侧）。 */
export function workspaceSeriesAxisPlacement(panel: OverrideFields | undefined, series: MetricResultSeries): AxisSide {
  return seriesOverride(panel, series).axisPlacement ?? (panel?.axis_placement === "right" ? "right" : "left")
}

/** workspaceSeriesThresholds 解析序列使用的阈值色带：覆盖优先，其次面板声明；都没有时返回空数组。 */
export function workspaceSeriesThresholds(panel: OverrideFields | undefined, series: OverridableSeries | undefined): { value: number; color: PanelColor }[] {
  if (!series) return panel?.thresholds ?? []
  return seriesOverride(panel, series).thresholds ?? panel?.thresholds ?? []
}

/** workspaceSeriesThresholdColor 解析某条序列的阈值取色：覆盖阈值优先，其次面板阈值；
 *  阈值显示关闭或没有数值时回到序列自身声明的颜色，与面板级阈值取色的口径一致。 */
export function workspaceSeriesThresholdColor(panel: OverridePanelFields | undefined, series: OverridableSeries | undefined, value: number | null): string {
  let color: PanelColor = panel?.series?.find((entry) => entry.ref_id === series?.ref_id)?.color ?? panel?.series?.[0]?.color ?? "blue"
  const display = thresholdDisplayFields(panel)
  if (value === null || !display || thresholdColorDisabled(display)) return panelColors[color]
  for (const threshold of workspaceSeriesThresholds(panel, series)) if (value >= threshold.value) color = threshold.color
  return panelColors[color]
}

/** thresholdDisplayFields 抽掉阈值显示只关心的三项；缺 type 的调用点不做面板类型门禁。 */
function thresholdDisplayFields(panel: OverrideFields | undefined): ThresholdDisplayFields | null {
  return panel ? { type: panel.type ?? "", threshold_mode: panel.threshold_mode, thresholds: panel.thresholds ?? [] } : null
}

/** workspaceSeriesUnit 解析序列的显示单位：覆盖优先，其次用查询结果里的单位。 */
export function workspaceSeriesUnit(panel: OverrideFields | undefined, series: MetricResultSeries): string {
  return seriesOverride(panel, series).unit ?? series.unit
}

/** workspaceSeriesDecimals 解析序列的小数位数：逐序列覆盖优先，其次面板级 decimals（可能都没有）。 */
export function workspaceSeriesDecimals(panel: OverrideFields | undefined, series: OverridableSeries | undefined): number | undefined {
  const override = series ? seriesOverride(panel, series).decimals : undefined
  return override !== undefined && Number.isInteger(override) && override >= 0 && override <= 6 ? override : panel?.decimals
}

/** workspaceSeriesName 解析序列的显示名：覆盖优先，其次沿用标签（展开视图会带上节点名）。 */
export function workspaceSeriesName(panel: OverrideFields | undefined, series: MetricResultSeries, expanded?: ExpandedSeries[]): string {
  return seriesOverride(panel, series).displayName ?? workspaceSeriesLabel(series, expanded)
}

/** workspaceSeriesLimits 解析序列的值域：覆盖优先，其次面板级 min/max，越界的覆盖值按未覆盖处理。 */
export function workspaceSeriesLimits(panel: OverrideFields | undefined, series: OverridableSeries | undefined): { min?: number; max?: number } {
  const override = series ? seriesOverride(panel, series) : {}
  const minimum = boundedLimit(override.min) ? override.min : panel?.min
  const maximum = boundedLimit(override.max) ? override.max : panel?.max
  // 覆盖只给一侧而另一侧沿用面板值时可能得到空量程，退回只给覆盖的那一侧，由调用方取默认值。
  if (minimum !== undefined && maximum !== undefined && minimum >= maximum) return { min: override.min }
  return { min: minimum, max: maximum }
}

function boundedLimit(value: number | undefined): value is number {
  return value !== undefined && Number.isFinite(value)
}

/** workspaceSeriesLineWidth 解析序列的折线宽度：覆盖优先，其次面板线宽；越界的覆盖值按未覆盖处理。 */
export function workspaceSeriesLineWidth(panel: OverrideFields | undefined, series: MetricResultSeries, fallback: number): number {
  const width = seriesOverride(panel, series).lineWidth
  return width !== undefined && Number.isInteger(width) && width >= minimumLineWidth && width <= maximumLineWidth ? width : fallback
}

/** workspaceSeriesFillOpacity 解析序列的填充不透明度（0..100）：越界的覆盖值同样按未覆盖处理。 */
export function workspaceSeriesFillOpacity(panel: OverrideFields | undefined, series: MetricResultSeries, fallback: number): number {
  const opacity = seriesOverride(panel, series).fillOpacity
  return opacity !== undefined && Number.isInteger(opacity) && opacity >= 0 && opacity <= maximumFillOpacity ? opacity : fallback
}

/** workspaceSeriesFillBelowTo 解析序列的填充目标名称：覆盖优先，没有覆盖时返回 undefined。 */
export function workspaceSeriesFillBelowTo(panel: OverrideFields | undefined, series: OverridableSeries): string | undefined {
  return seriesOverride(panel, series).fillBelowTo
}

/** legendHidden 只决定图例是否列出该序列；隐藏的序列仍然绘制在面板里。 */
export function legendHidden(panel: OverrideFields | undefined, series: OverridableSeries): boolean {
  return seriesOverride(panel, series).hideLegend === true
}

/** overrideColor 返回覆盖指定的颜色；没有覆盖时返回 null，调用方继续按阈值或调色板取值。 */
export function overrideColor(panel: OverrideFields | undefined, series: OverridableSeries): string | null {
  const color = seriesOverride(panel, series).color
  return color ? panelColors[color] : null
}

/** 新增覆盖给一个可用的起点：按节点名前缀匹配并改成绿色，与 label 过滤的缺省一致。 */
export function emptyOverride(): DashboardOverride {
  return { matcher: { type: "by_regex", value: "^node-" }, color: "green" }
}

/** 新增一条逐序列阈值：与面板级阈值一样从 70 起步、每次加 10，避免写出一组无用刻度。 */
export function appendedOverrideThreshold(thresholds: { value: number; color: PanelColor }[] | undefined): { value: number; color: PanelColor }[] {
  const current = thresholds ?? []
  return [...current, { value: (current.at(-1)?.value ?? 70) + 10, color: "amber" }]
}

/** 切换匹配方式时保留匹配值与属性，只换解释方式，避免把整条覆盖清空。
 *  换成「按字段类型」时匹配值必须换成枚举里的类型，否则整条规则会立刻变成非法配置。 */
export function switchOverrideMatcherType(override: DashboardOverride, type: string): DashboardOverride {
  const matcherType = overrideMatcherTypeList.includes(type as OverrideMatcherType) ? type as OverrideMatcherType : "by_name"
  const value = matcherType === "by_type" ? "percent" : override.matcher.value
  return { ...override, matcher: { type: matcherType, value } }
}
