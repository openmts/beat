import type { BarAlignment, DashboardPanel, PanelType } from "@/types/metric-workspace"

/** 与服务端 internal/model 同规则：柱宽 4..40px（缺省 12）、柱圆角 0..10（缺省 0）。 */
export const minBarWidth = 4
export const maxBarWidth = 40
export const defaultBarWidth = 12
export const minBarRadius = 0
export const maxBarRadius = 10
/** 面板类型切换时需要一并清理的柱状图字段；其它可视化类型不接受这些字段。 */
export const barChartFieldNames = ["bar_width", "bar_radius", "bar_alignment"] as const

/** Grafana 的 Bar alignment：auto 是缺省，等同于 centered（柱子中心落在时间点）。 */
export const barAlignments = ["auto", "before", "centered", "after"] as const

export interface BarChartFields {
  type: string
  bar_width?: number
  bar_radius?: number
  bar_alignment?: string
}

export interface PanelBarChart {
  width: number
  radius: number
}

/** panelBarChart 解析生效值，缺省与服务端一致：12px 柱宽、直角柱顶。 */
export function panelBarChart(panel: BarChartFields): PanelBarChart {
  return {
    width: validBarWidth(panel.bar_width) ? panel.bar_width as number : defaultBarWidth,
    radius: validBarRadius(panel.bar_radius) ? panel.bar_radius as number : minBarRadius,
  }
}

/** barChartError 返回目录键，null 表示可以保存；规则与服务端一致。 */
export function barChartError(panel: BarChartFields): string | null {
  const styled = panel.bar_width !== undefined || panel.bar_radius !== undefined || panel.bar_alignment !== undefined
  if (styled && panel.type !== "barchart") return "workspace.bar_chart_barchart_only"
  if (panel.bar_width !== undefined && !validBarWidth(panel.bar_width)) return "workspace.bar_width_invalid"
  if (panel.bar_radius !== undefined && !validBarRadius(panel.bar_radius)) return "workspace.bar_radius_invalid"
  if (panel.bar_alignment !== undefined && !validBarAlignment(panel.bar_alignment)) return "workspace.bar_alignment_invalid"
  return null
}

/** barChartPatch 只在目标是柱状图时保留字段；切走时同步清理，避免保存出服务端会拒绝的组合。 */
export function barChartPatch(type: PanelType): Partial<Pick<DashboardPanel, "bar_width" | "bar_radius" | "bar_alignment">> {
  return type === "barchart" ? {} : { bar_width: undefined, bar_radius: undefined, bar_alignment: undefined }
}

/** panelBarAlignment 返回生效的对齐方式：未知取值与缺省都按 auto 处理，渲染器不再判断。 */
export function panelBarAlignment(panel: { bar_alignment?: string }): BarAlignment {
  return barAlignments.find((value) => value === panel.bar_alignment) ?? "auto"
}

/** barRadiusCorners 把圆角换算成渲染器的四角数组：只圆化柱顶，柱脚保持贴住坐标轴。 */
export function barRadiusCorners(radius: number): [number, number, number, number] {
  const corners = validBarRadius(radius) ? radius : minBarRadius
  return [corners, corners, 0, 0]
}

function validBarAlignment(value: string): boolean {
  return barAlignments.some((candidate) => candidate === value)
}

function validBarWidth(value: number | undefined): boolean {
  return value !== undefined && Number.isInteger(value) && value >= minBarWidth && value <= maxBarWidth
}

function validBarRadius(value: number | undefined): boolean {
  return value !== undefined && Number.isInteger(value) && value >= minBarRadius && value <= maxBarRadius
}
