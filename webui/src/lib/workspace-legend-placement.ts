import type { DashboardPanel } from "@/types/metric-workspace"

/** 与服务端 dashboardLegendPlacements 一致：bottom 是缺省，right 把图例放到绘图区右侧。 */
export const legendPlacements = ["bottom", "right"] as const
export type LegendPlacement = typeof legendPlacements[number]

/** 只有会画图例的面板才提供图例位置，与服务端校验同一份白名单。 */
export const legendPlacementTypes = ["timeseries", "barchart"] as const

export type LegendPlacementFields = Pick<DashboardPanel, "type" | "legend" | "legend_placement">

/** legendPlacement 返回生效的位置：未知取值与缺省都按 bottom 处理，渲染器不需要再判断。 */
export function legendPlacement(panel: Pick<DashboardPanel, "legend_placement">): LegendPlacement {
  return legendPlacements.find((value) => value === panel.legend_placement) ?? "bottom"
}

/** legendPlacementError 返回目录键，null 表示可以保存；规则与服务端一致。 */
export function legendPlacementError(panel: LegendPlacementFields): string | null {
  if (panel.legend_placement === undefined) return null
  if (!panel.legend) return "workspace.legend_placement_requires_legend"
  if (!legendPlacementTypes.some((type) => type === panel.type)) return "workspace.legend_placement_panels_only"
  return legendPlacements.some((value) => value === panel.legend_placement) ? null : "workspace.legend_placement_invalid"
}

/** legendPlacementPatch 选回 bottom 时省略字段：它与服务端缺省等价，留着只会制造无意义的差异。 */
export function legendPlacementPatch(placement: string): Pick<DashboardPanel, "legend_placement"> {
  const value = legendPlacements.find((candidate) => candidate === placement)
  return value === undefined || value === "bottom" ? { legend_placement: undefined } : { legend_placement: value }
}

/** Grafana 的 Legend → Width：右侧图例列宽，缺省沿用渲染器自带宽度（列表 160px、表格 224px）。 */
export const minLegendWidth = 120
export const maxLegendWidth = 480

export type LegendWidthFields = Pick<DashboardPanel, "type" | "legend" | "legend_placement" | "legend_width">

/** legendWidth 返回显式声明的列宽；缺省或非法值返回 undefined，渲染器回落到自带宽度。 */
export function legendWidth(panel: Pick<DashboardPanel, "legend_width">): number | undefined {
  return validLegendWidth(panel.legend_width) ? panel.legend_width as number : undefined
}

/** legendWidthError 返回目录键，null 表示可以保存；规则与服务端 validateDashboardLegendWidth 一致。 */
export function legendWidthError(panel: LegendWidthFields): string | null {
  if (panel.legend_width === undefined) return null
  if (!panel.legend) return "workspace.legend_width_requires_legend"
  if (!legendPlacementTypes.some((type) => type === panel.type)) return "workspace.legend_width_panels_only"
  if (panel.legend_placement !== "right") return "workspace.legend_width_requires_right"
  return validLegendWidth(panel.legend_width) ? null : "workspace.legend_width_invalid"
}

/** legendWidthPatch 只在折线与柱状面板保留列宽；切到别的可视化类型时同步清理。 */
export function legendWidthPatch(type: string): Pick<DashboardPanel, "legend_width"> {
  return legendPlacementTypes.some((value) => value === type) ? {} : { legend_width: undefined }
}

function validLegendWidth(value: number | undefined): boolean {
  return value !== undefined && Number.isInteger(value) && value >= minLegendWidth && value <= maxLegendWidth
}
