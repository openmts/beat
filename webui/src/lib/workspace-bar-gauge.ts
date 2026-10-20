import type { BarDisplay, BarOrientation, DashboardPanel, PanelType } from "@/types/metric-workspace"

/** 与服务端 internal/model 同规则：方向 auto/horizontal/vertical、显示 basic/gradient/lcd、轨道缺省绘制。 */
export const barOrientations = ["auto", "horizontal", "vertical"] as const
export const barDisplays = ["basic", "gradient", "lcd"] as const
/** LCD 模式的固定格数：同一数值在任何面板尺寸下点亮相同格数，便于跨面板比较。 */
export const barGaugeSegments = 10
/** 面板类型切换时需要一并清理的条状仪表字段；其它可视化类型不接受这些字段。 */
export const barGaugeFieldNames = ["bar_orientation", "bar_display", "bar_unfilled"] as const

export interface BarGaugeFields {
  type: string
  bar_orientation?: string
  bar_display?: string
  bar_unfilled?: boolean
}

export interface PanelBarGauge {
  orientation: BarOrientation
  display: BarDisplay
  unfilled: boolean
}

/** panelBarGauge 解析生效值，缺省与服务端一致：auto 方向、basic 填充、绘制未填充轨道。 */
export function panelBarGauge(panel: BarGaugeFields): PanelBarGauge {
  return {
    orientation: barOrientations.find((value) => value === panel.bar_orientation) ?? "auto",
    display: barDisplays.find((value) => value === panel.bar_display) ?? "basic",
    unfilled: panel.bar_unfilled !== false,
  }
}

/** barGaugeError 返回目录键，null 表示可以保存；规则与服务端一致。 */
export function barGaugeError(panel: BarGaugeFields): string | null {
  const styled = panel.bar_orientation !== undefined || panel.bar_display !== undefined || panel.bar_unfilled !== undefined
  if (styled && panel.type !== "bargauge") return "workspace.bar_gauge_bargauge_only"
  if (panel.bar_orientation !== undefined && !barOrientations.includes(panel.bar_orientation as BarOrientation)) return "workspace.bar_orientation_invalid"
  if (panel.bar_display !== undefined && !barDisplays.includes(panel.bar_display as BarDisplay)) return "workspace.bar_display_invalid"
  if (panel.bar_unfilled !== undefined && typeof panel.bar_unfilled !== "boolean") return "workspace.bar_unfilled_invalid"
  return null
}

/** resolvedBarOrientation 解析 auto：单序列竖排、多序列横排，与 Grafana 的 auto 一致。 */
export function resolvedBarOrientation(panel: BarGaugeFields, seriesCount: number): BarOrientation {
  const orientation = panelBarGauge(panel).orientation
  if (orientation !== "auto") return orientation
  return seriesCount <= 1 ? "vertical" : "horizontal"
}

/** barGaugeFraction 把数值换算成 0..1 的填充比例；未知上下限或非法数值按 0 处理。 */
export function barGaugeFraction(minimum: number, maximum: number, value: number | null | undefined): number {
  if (value === null || value === undefined || !Number.isFinite(value)) return 0
  if (!Number.isFinite(minimum) || !Number.isFinite(maximum) || maximum <= minimum) return 0
  return Math.max(0, Math.min(1, (value - minimum) / (maximum - minimum)))
}

/** barGaugeLitSegments 返回 LCD 模式点亮的格数；没有填充时返回 0。 */
export function barGaugeLitSegments(fraction: number): number {
  if (!Number.isFinite(fraction) || fraction <= 0) return 0
  return Math.min(barGaugeSegments, Math.ceil(fraction * barGaugeSegments))
}

/** barGaugePatch 只在目标是条状仪表时保留字段；切走时同步清理，避免保存出服务端会拒绝的组合。 */
export function barGaugePatch(type: PanelType): Partial<Pick<DashboardPanel, "bar_orientation" | "bar_display" | "bar_unfilled">> {
  return type === "bargauge" ? {} : { bar_orientation: undefined, bar_display: undefined, bar_unfilled: undefined }
}
