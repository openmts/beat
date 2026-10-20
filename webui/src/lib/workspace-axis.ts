import type { DashboardPanel, PanelType } from "@/types/metric-workspace"

/** 与服务端 internal/model 同规则：柔性上下限只对折线与柱状面板生效，同一侧已有固定上下限时拒绝。 */
export const softAxisPanelTypes: PanelType[] = ["timeseries", "barchart"]
/** 面板类型切换时需要一并清理的柔性轴字段；其它可视化类型不接受这些字段。 */
export const softAxisFieldNames = ["soft_min", "soft_max"] as const

export interface AxisFields {
  type: string
  min?: number
  max?: number
  soft_min?: number
  soft_max?: number
  scale?: string
}

/** 坐标轴端点：固定值、跟随数据的函数，或交给渲染器自动决定。 */
export type AxisLimit = number | ((value: number) => number) | "auto"

/** axisError 返回目录键，null 表示可以保存；规则与服务端一致。 */
export function axisError(panel: AxisFields): string | null {
  const styled = panel.soft_min !== undefined || panel.soft_max !== undefined
  if (panel.soft_min !== undefined && !Number.isFinite(panel.soft_min)) return "workspace.soft_axis_invalid"
  if (panel.soft_max !== undefined && !Number.isFinite(panel.soft_max)) return "workspace.soft_axis_invalid"
  if (panel.soft_min !== undefined && panel.soft_max !== undefined && panel.soft_min >= panel.soft_max) return "workspace.soft_axis_order"
  if (!styled) return null
  if (!softAxisPanelTypes.includes(panel.type as PanelType)) return "workspace.soft_axis_timeseries_only"
  if (panel.soft_min !== undefined && panel.min !== undefined || panel.soft_max !== undefined && panel.max !== undefined) return "workspace.soft_axis_conflict"
  return null
}

/** softAxisPatch 只在折线与柱状面板保留柔性轴字段；切走时同步清理，避免保存出服务端会拒绝的组合。 */
export function softAxisPatch(type: PanelType): Partial<Pick<DashboardPanel, "soft_min" | "soft_max">> {
  return softAxisPanelTypes.includes(type) ? {} : { soft_min: undefined, soft_max: undefined }
}

/**
 * axisDomain 生成渲染器的坐标轴 domain。固定上下限优先；柔性上下限只在数据超出时撑开坐标轴，
 * 数据没超出时轴就从柔性值开始（Grafana 的 Soft min / Soft max 语义）；对数刻度（Grafana 的
 * Axis → Scale: Logarithmic）交给渲染器按正样本取值，因为 0 与负数在对数轴上没有位置。
 */
export function axisDomain(panel: AxisFields, unit: string): [AxisLimit, AxisLimit] {
  const logarithmic = panel.scale === "log"
  // 百分比单位缺省 0..100 在对数轴上不可用，0 没有对数位置。
  const percent = unit === "percent" && !logarithmic
  return [axisLower(panel, percent, logarithmic), axisUpper(panel, percent, logarithmic)]
}

function axisLower(panel: AxisFields, percent: boolean, logarithmic: boolean): AxisLimit {
  // 对数轴上的非正固定下限由服务端拒绝；这里仍然兜底回自动刻度，渲染器不会拿到非法值。
  if (panel.min !== undefined && (!logarithmic || panel.min > 0)) return panel.min
  if (panel.soft_min !== undefined && (!logarithmic || panel.soft_min > 0)) return (dataMin: number) => Math.min(panel.soft_min as number, dataMin)
  if (logarithmic) return "auto"
  return percent ? 0 : "auto"
}

function axisUpper(panel: AxisFields, percent: boolean, logarithmic: boolean): AxisLimit {
  if (panel.max !== undefined) return panel.max
  if (panel.soft_max !== undefined) return (dataMax: number) => Math.max(panel.soft_max as number, dataMax)
  if (logarithmic) return "auto"
  return percent ? 100 : "auto"
}
