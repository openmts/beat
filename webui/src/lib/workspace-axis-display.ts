import { softAxisPanelTypes, type AxisLimit } from "@/lib/workspace-axis"
import type { DashboardPanel, PanelType } from "@/types/metric-workspace"

/** Grafana 的 Axis → Placement：left 是缺省值（auto 也落在左侧），right 把 Y 轴挪到绘图区右侧。 */
export const axisPlacements = ["left", "right"] as const
export type AxisPlacement = (typeof axisPlacements)[number]

/** axisGrid 与 Grafana 的 Show grid lines 同义：只有显式 false 才关掉水平网格线。 */
export function axisGrid(panel: { grid?: boolean }): boolean {
  return panel.grid !== false
}

/** axisPlacement 把未声明与非法值都收敛到缺省的 left。 */
export function axisPlacement(panel: { axis_placement?: string }): AxisPlacement {
  return panel.axis_placement === "right" ? "right" : "left"
}

/** 轴宽：百分比刻度更窄；面板轴标签额外留出左内边距。渲染器按这个宽度从绘图区里扣掉一根轴。 */
const compactAxisWidth = 52
const wideAxisWidth = 88
const axisLabelExtraWidth = 18

/** axisWidth 给出某根值轴的宽度：单位决定基础宽度，轴标签只在它挂载的那侧加宽。 */
export function axisWidth(unit: string, labelled: boolean): number {
  return (unit === "percent" ? compactAxisWidth : wideAxisWidth) + (labelled ? axisLabelExtraWidth : 0)
}

/** 逐序列轴宽覆盖的闭区间：0 与缺省同义（自动），上限与服务端 MaxDashboardAxisWidth 一致。 */
export const minimumAxisWidth = 0
export const maximumAxisWidth = 500

/**
 * axisOverrideWidth 解析一根值轴的最终宽度：逐序列覆盖（Grafana 的 Overrides → Axis → Width）优先，
 * 0 与未声明一样回落到按单位与轴标题算出的缺省宽度，越界值同样按未覆盖处理。
 */
export function axisOverrideWidth(width: number | undefined, unit: string, labelled: boolean): number {
  const usable = width !== undefined && Number.isInteger(width) && width > minimumAxisWidth && width <= maximumAxisWidth
  return usable ? width as number : axisWidth(unit, labelled)
}

/** axisDisplayError 返回目录键，null 表示可以保存；规则与服务端 validateDashboardAxisDisplay 一致。 */
export function axisDisplayError(panel: { type: string; grid?: boolean; axis_placement?: string }): string | null {
  const declared = panel.grid !== undefined || panel.axis_placement !== undefined && panel.axis_placement !== ""
  if (!declared) return null
  if (!softAxisPanelTypes.includes(panel.type as PanelType)) return "workspace.axis_display_panels_only"
  const placement = panel.axis_placement
  return placement === undefined || placement === "" || axisPlacements.includes(placement as AxisPlacement) ? null : "workspace.axis_placement_invalid"
}

/** centeredZeroError 返回目录键；固定上下限会直接决定坐标轴，因此与居中互斥。 */
export function centeredZeroError(panel: { type: string; centered_zero?: boolean; min?: number; max?: number }): string | null {
  if (panel.centered_zero !== true) return null
  if (!softAxisPanelTypes.includes(panel.type as PanelType)) return "workspace.centered_zero_panels_only"
  return panel.min === undefined && panel.max === undefined ? null : "workspace.centered_zero_fixed_limits"
}

/**
 * centeredZeroDomain 实现 Grafana 的 Centered zero：坐标轴对称展开到 ±limit，
 * limit 取数据绝对值最大值与柔性上下限的较大者；没有任何可用正数时保持原 domain。
 */
export function centeredZeroDomain(
  panel: { centered_zero?: boolean; soft_min?: number; soft_max?: number }, maximum: number, fallback: [AxisLimit, AxisLimit],
): [AxisLimit, AxisLimit] {
  if (panel.centered_zero !== true) return fallback
  const limit = Math.max(Number.isFinite(maximum) ? maximum : 0, Math.abs(panel.soft_min ?? 0), Math.abs(panel.soft_max ?? 0))
  return limit > 0 ? [-limit, limit] : fallback
}

/** axisDisplayPatch 只在折线与柱状面板保留显示类轴选项；切走时同步清理。 */
export function axisDisplayPatch(type: PanelType): Partial<Pick<DashboardPanel, "grid" | "axis_placement" | "centered_zero">> {
  return softAxisPanelTypes.includes(type) ? {} : { grid: undefined, axis_placement: undefined, centered_zero: undefined }
}
