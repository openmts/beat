import { softAxisPanelTypes } from "@/lib/workspace-axis"
import type { DashboardPanel, PanelType } from "@/types/metric-workspace"

/** Grafana 的 Axis → Scale：linear 是缺省值，log 是以 10 为底的对数刻度。 */
export const axisScaleModes = ["linear", "log"] as const
export type AxisScaleMode = (typeof axisScaleModes)[number]

/** 与服务端 internal/model 同规则：刻度类型只对折线与柱状面板生效。 */
export const axisScalePanelTypes: PanelType[] = softAxisPanelTypes

/** axisScale 把未声明与非法值都收敛到缺省的 linear，渲染器因此永远拿到一个已知刻度。 */
export function axisScale(panel: { scale?: string }): AxisScaleMode {
  return panel.scale === "log" ? "log" : "linear"
}

/** axisScaleError 返回目录键，null 表示可以保存；规则与服务端 validateDashboardAxisScale 一致。 */
export function axisScaleError(panel: { type: string; scale?: string; min?: number; soft_min?: number }): string | null {
  const scale = panel.scale
  if (scale === undefined || scale === "") return null
  if (scale !== "linear" && scale !== "log") return "workspace.axis_scale_invalid"
  if (!axisScalePanelTypes.includes(panel.type as PanelType)) return "workspace.axis_scale_panels_only"
  if (scale !== "log") return null
  if (panel.min !== undefined && panel.min <= 0 || panel.soft_min !== undefined && panel.soft_min <= 0) return "workspace.axis_scale_positive_minimum"
  return null
}

/** axisScalePatch 只在折线与柱状面板保留刻度类型；切走时同步清理，避免保存出服务端会拒绝的组合。 */
export function axisScalePatch(type: PanelType): Partial<Pick<DashboardPanel, "scale">> {
  return axisScalePanelTypes.includes(type) ? {} : { scale: undefined }
}

/**
 * maskLogRows 把 0 与负数改成缺失值：它们在对数轴上没有位置，Grafana 同样不画这些样本。
 * timestamp 等非序列键原样保留。
 */
export function maskLogRows<T extends Record<string, number | null>>(rows: T[], keys: string[]): T[] {
  return rows.map((row) => {
    const masked = { ...row }
    for (const key of keys) {
      const value = row[key]
      if (typeof value === "number" && value <= 0) masked[key as keyof T] = null as T[keyof T]
    }
    return masked
  })
}
