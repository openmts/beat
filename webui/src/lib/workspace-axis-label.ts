import { softAxisPanelTypes } from "@/lib/workspace-axis"
import type { DashboardPanel, PanelType } from "@/types/metric-workspace"

/** 与服务端 MaxDashboardAxisLabelBytes 一致：Y 轴标题只是坐标轴旁的一行短文本。 */
export const maximumAxisLabelBytes = 40

/** axisLabelError 返回目录键，null 表示可以保存；规则与服务端 validateDashboardAxisLabel 一致。 */
export function axisLabelError(panel: { type: string; axis_label?: string }): string | null {
  const value = panel.axis_label
  if (value === undefined || value === "") return null
  if (!softAxisPanelTypes.includes(panel.type as PanelType)) return "workspace.axis_label_panels_only"
  return validAxisLabel(value) ? null : "workspace.axis_label_invalid"
}

/** validAxisLabel 与服务端同口径：UTF-8 有界，且不含控制字符（标题只有一行，换行也按非法处理）。 */
export function validAxisLabel(value: string): boolean {
  if (new TextEncoder().encode(value).length > maximumAxisLabelBytes) return false
  for (const character of value) {
    const code = character.codePointAt(0) ?? 0
    if (code < 0x20 || code === 0x7f) return false
  }
  return true
}

/** axisLabelPatch 只在折线与柱状面板保留标题；切走时同步清理，避免保存出服务端会拒绝的组合。 */
export function axisLabelPatch(type: PanelType): Partial<Pick<DashboardPanel, "axis_label">> {
  return softAxisPanelTypes.includes(type) ? {} : { axis_label: undefined }
}
