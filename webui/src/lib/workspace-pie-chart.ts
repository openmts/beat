import type { DashboardPanel, PanelType, PieChartType, PieLabelMode } from "@/types/metric-workspace"

/** 与服务端 internal/model 同规则：形态 pie/donut、标签 percent/value/both。 */
export const pieChartTypes = ["pie", "donut"] as const
export const pieLabelModes = ["percent", "value", "both"] as const
/** 面板类型切换时需要一并清理的饼图字段；其它可视化类型不接受这些字段。 */
export const pieChartFieldNames = ["pie_chart_type", "pie_label_mode"] as const

export interface PieChartFields {
  type: string
  pie_chart_type?: string
  pie_label_mode?: string
}

export interface PanelPieChart {
  chartType: PieChartType
  labelMode: PieLabelMode
}

/** panelPieChart 解析生效值，缺省与服务端一致：实心饼图、只标注百分比。 */
export function panelPieChart(panel: PieChartFields): PanelPieChart {
  return {
    chartType: pieChartTypes.find((value) => value === panel.pie_chart_type) ?? "pie",
    labelMode: pieLabelModes.find((value) => value === panel.pie_label_mode) ?? "percent",
  }
}

/** pieChartError 返回目录键，null 表示可以保存；规则与服务端一致。 */
export function pieChartError(panel: PieChartFields): string | null {
  const styled = panel.pie_chart_type !== undefined || panel.pie_label_mode !== undefined
  if (styled && panel.type !== "piechart") return "workspace.pie_chart_piechart_only"
  if (panel.pie_chart_type !== undefined && !pieChartTypes.includes(panel.pie_chart_type as PieChartType)) return "workspace.pie_chart_type_invalid"
  if (panel.pie_label_mode !== undefined && !pieLabelModes.includes(panel.pie_label_mode as PieLabelMode)) return "workspace.pie_label_mode_invalid"
  return null
}

/**
 * pieSliceFractions 把各序列的最新值换算成占比。零、负数与非有限值都不参与占比：它们只占位置，
 * 不改变其它扇区的相对大小，避免一个坏读数把整张图拉偏。总量为零时全部返回 0。
 */
export function pieSliceFractions(values: readonly (number | null | undefined)[]): number[] {
  const positive = values.map((value) => (typeof value === "number" && Number.isFinite(value) && value > 0 ? value : 0))
  const total = positive.reduce((sum, value) => sum + value, 0)
  if (total <= 0) return positive.map(() => 0)
  return positive.map((value) => value / total)
}

export interface PieArcGeometry {
  /** 外半径，单位与调用方一致（SVG 用户单位）。 */
  radius: number
  /** 内半径：实心饼图为 0，环形图大于 0。 */
  innerRadius: number
}

/**
 * pieArcPath 生成一段扇区路径。起点与终点用 0..1 的比例表示，按钟表方向从左上方开始。
 * 整圆（占比为 1）无法用两段等角度弧表示，调用方需要改用整圆元素。
 */
export function pieArcPath(geometry: PieArcGeometry, start: number, end: number): string {
  const center = geometry.radius + geometry.innerRadius
  const clamped = Math.max(0, Math.min(1, end - start))
  if (clamped <= 0) return ""
  const from = pointOnCircle(center, geometry.radius, start)
  const to = pointOnCircle(center, geometry.radius, end)
  const largeArc = clamped > 0.5 ? 1 : 0
  if (geometry.innerRadius <= 0) {
    return `M ${center} ${center} L ${from.x} ${from.y} A ${geometry.radius} ${geometry.radius} 0 ${largeArc} 1 ${to.x} ${to.y} Z`
  }
  const innerFrom = pointOnCircle(center, geometry.innerRadius, end)
  const innerTo = pointOnCircle(center, geometry.innerRadius, start)
  return `M ${from.x} ${from.y} A ${geometry.radius} ${geometry.radius} 0 ${largeArc} 1 ${to.x} ${to.y} `
    + `L ${innerFrom.x} ${innerFrom.y} A ${geometry.innerRadius} ${geometry.innerRadius} 0 ${largeArc} 0 ${innerTo.x} ${innerTo.y} Z`
}

/** pointOnCircle 把 0..1 的比例换算成圆周上的坐标；起点在正上方。 */
export function pointOnCircle(center: number, radius: number, fraction: number): { x: number; y: number } {
  const angle = fraction * Math.PI * 2 - Math.PI / 2
  return { x: round(center + radius * Math.cos(angle)), y: round(center + radius * Math.sin(angle)) }
}

function round(value: number): number {
  return Math.round(value * 100) / 100
}

/** 扇区开始位置：前面所有扇区的占比之和。 */
export function pieSliceStarts(fractions: readonly number[]): number[] {
  const starts: number[] = []
  let running = 0
  for (const fraction of fractions) {
    starts.push(running)
    running += fraction
  }
  return starts
}

/** pieChartPatch 只在目标是饼图时保留字段；切走时同步清理，避免保存出服务端会拒绝的组合。 */
export function pieChartPatch(type: PanelType): Partial<Pick<DashboardPanel, "pie_chart_type" | "pie_label_mode">> {
  return type === "piechart" ? {} : { pie_chart_type: undefined, pie_label_mode: undefined }
}
