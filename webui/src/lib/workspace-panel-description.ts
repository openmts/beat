import type { DashboardPanel } from "@/types/metric-workspace"

/** 与服务端 MaxPanelDescriptionBytes 一致；描述只出现在信息图标里，512 字节足够写清口径。 */
export const maximumPanelDescriptionBytes = 512

export type PanelDescriptionFields = Pick<DashboardPanel, "description">

/** panelDescriptionError 返回目录键，null 表示可以保存；规则与服务端 validateDashboardPanelDescription 一致。 */
export function panelDescriptionError(panel: PanelDescriptionFields): string | null {
  const value = panel.description
  if (value === undefined || value === "") return null
  return validPanelDescription(value) ? null : "workspace.panel_description_invalid"
}

/** validPanelDescription 与文本面板共用同一套文本口径：UTF-8、有界，且除换行、回车与制表符外无控制字符。 */
export function validPanelDescription(value: string): boolean {
  if (new TextEncoder().encode(value).length > maximumPanelDescriptionBytes) return false
  for (const character of value) {
    const code = character.codePointAt(0) ?? 0
    if (code < 0x20 && character !== "\n" && character !== "\r" && character !== "\t") return false
  }
  return true
}
