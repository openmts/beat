import { record, text, importBoundedInteger } from "@/lib/workspace-import-core"
import { gradientModes } from "@/lib/workspace-line-style"
import { dashboardLinksError, maximumDashboardLinks, maximumPanelLinkBytes, maximumPanelLinkTitleLength, maximumPanelLinks, panelLinksError } from "@/lib/workspace-links"
import { cellDisplayModes, dashboardLinkIcons, tableOrientations } from "@/types/metric-workspace"
import { maxSparklineFillOpacity, maxSparklineLineWidth, minSparklineLineWidth, sparklineError } from "@/lib/workspace-sparkline"
import { validPanelDescription } from "@/lib/workspace-panel-description"
import { statColorModes, statOrientations, statStyleError, statTextModes, statTextSizes, type StatStyleKey, type StatStylePatch } from "@/lib/workspace-stat"
import { gaugeThresholdError } from "@/lib/workspace-gauge"
import { validWorkspaceText } from "@/lib/workspace-text"
import { noValueError } from "@/lib/workspace-overrides"
import type { DashboardCellDisplayMode, DashboardDefinition, DashboardTableOrientation, DashboardLinkIcon, DashboardPanel, DashboardPanelLink, PanelType } from "@/types/metric-workspace"

/** importRowCollapse 只接受行面板上的布尔 row_collapsed；其它类型出现该字段一律按无效文件处理。 */
export function importRowCollapse(source: Record<string, unknown>, type: PanelType): Pick<DashboardPanel, "row_collapsed"> {
  const collapsed = source.row_collapsed
  if (collapsed === undefined || collapsed === false) return {}
  if (collapsed !== true || type !== "row") throw new Error("workspace.import_invalid")
  return { row_collapsed: true }
}

/** 看板级导航链接随看板导入导出；非法协议或超出条数按整份文件无效处理。 */
/** importHideLinks 只接受布尔值：其它类型说明文件被改坏，按整份文件无效处理。 */
export function importHideLinks(value: unknown): Partial<Pick<DashboardDefinition, "hide_links">> {
  if (value === undefined || value === false) return {}
  if (value !== true) throw new Error("workspace.import_invalid")
  return { hide_links: true }
}

export function importDashboardLinks(value: unknown): DashboardPanelLink[] {
  if (value === undefined) return []
  if (!Array.isArray(value) || value.length > maximumDashboardLinks) throw new Error("workspace.import_invalid")
  const links = value.map(importPanelLink)
  if (dashboardLinksError(links)) throw new Error("workspace.import_invalid")
  return links
}

/** importStatStyles 只接受 stat 面板上的 Color mode、Text mode、Text size 与 Orientation；
 *  其它类型上的任何一项都按无效文件处理。 */
export function importStatStyles(source: Record<string, unknown>, type: PanelType): StatStylePatch {
  const fields: StatStylePatch = {}
  const sources = { stat_color_mode: statColorModes, stat_text_mode: statTextModes, stat_text_size: statTextSizes,
    stat_orientation: statOrientations }
  for (const key of Object.keys(sources) as StatStyleKey[]) {
    const raw = source[key]
    if (raw === undefined || raw === "") continue
    const value = sources[key].find((candidate) => candidate === raw)
    if (value === undefined) throw new Error("workspace.import_invalid")
    ;(fields as Record<string, string>)[key] = value
  }
  if (type !== "stat" && Object.keys(fields).length > 0) throw new Error("workspace.import_invalid")
  if (statStyleError({ type, ...fields })) throw new Error("workspace.import_invalid")
  return fields
}

/** importGaugeThresholds 只接受 gauge 面板上的阈值刻度与标签开关；其它类型出现任一字段按无效文件处理。 */
export function importGaugeThresholds(source: Record<string, unknown>, type: PanelType, thresholds: DashboardPanel["thresholds"]): Pick<DashboardPanel, "gauge_threshold_markers" | "gauge_threshold_labels"> {
  const fields: Pick<DashboardPanel, "gauge_threshold_markers" | "gauge_threshold_labels"> = {}
  for (const key of ["gauge_threshold_markers", "gauge_threshold_labels"] as const) {
    const value = source[key]
    if (value === undefined) continue
    if (typeof value !== "boolean") throw new Error("workspace.import_invalid")
    fields[key] = value
  }
  if (gaugeThresholdError({ type, thresholds, ...fields })) throw new Error("workspace.import_invalid")
  return fields
}

/** importPanelDescription 接受任意面板类型上的可选描述；超长或含控制字符按整份文件无效处理。 */
export function importPanelDescription(source: Record<string, unknown>): Pick<DashboardPanel, "description"> {
  const value = source.description
  if (value === undefined || value === "") return {}
  if (typeof value !== "string" || !validPanelDescription(value)) throw new Error("workspace.import_invalid")
  return { description: value }
}

/** importPanelText 只接受文本面板上的 Markdown 内容；其余类型出现 text 一律按无效文件处理。 */
export function importPanelText(source: Record<string, unknown>, type: PanelType): Pick<DashboardPanel, "text"> {
  const value = source.text
  if (value === undefined || value === "") return {}
  if (typeof value !== "string" || type !== "text" || !validWorkspaceText(value)) throw new Error("workspace.import_invalid")
  return { text: value }
}

/** importSparkline 导入 stat 面板的迷你趋势图开关、线宽、填充与渐变；其它类型上的任何一项都按整份文件无效处理。 */
export function importSparkline(source: Record<string, unknown>, type: PanelType): Pick<DashboardPanel, "sparkline" | "sparkline_line_width" | "sparkline_fill_opacity" | "sparkline_gradient_mode"> {
  const fields: Pick<DashboardPanel, "sparkline" | "sparkline_line_width" | "sparkline_fill_opacity" | "sparkline_gradient_mode"> = {}
  if (source.sparkline !== undefined) {
    if (typeof source.sparkline !== "boolean") throw new Error("workspace.import_invalid")
    fields.sparkline = source.sparkline
  }
  if (source.sparkline_line_width !== undefined) fields.sparkline_line_width = importBoundedInteger(source.sparkline_line_width, minSparklineLineWidth, maxSparklineLineWidth)
  if (source.sparkline_fill_opacity !== undefined) fields.sparkline_fill_opacity = importBoundedInteger(source.sparkline_fill_opacity, 0, maxSparklineFillOpacity)
  if (source.sparkline_gradient_mode !== undefined && source.sparkline_gradient_mode !== "") {
    const gradient = gradientModes.find((mode) => mode === source.sparkline_gradient_mode)
    if (gradient === undefined) throw new Error("workspace.import_invalid")
    fields.sparkline_gradient_mode = gradient
  }
  if (type !== "stat" && Object.keys(fields).length > 0) throw new Error("workspace.import_invalid")
  if (sparklineError({ type, ...fields })) throw new Error("workspace.import_invalid")
  return fields
}

/** importPanelLinks 只接受最多 5 条合法数据链接；行面板上出现 links 一律按无效文件处理。 */
export function importPanelLinks(source: Record<string, unknown>, type: PanelType): Pick<DashboardPanel, "links"> {
  if (source.links === undefined) return {}
  if (!Array.isArray(source.links) || source.links.length > maximumPanelLinks || type === "row") throw new Error("workspace.import_invalid")
  const links = source.links.map(importPanelLink)
  if (panelLinksError(links)) throw new Error("workspace.import_invalid")
  return { links }
}

export function importPanelLink(value: unknown): DashboardPanelLink {
  const source = record(value)
  if (!source) throw new Error("workspace.import_invalid")
  const title = text(source.title, maximumPanelLinkTitleLength), url = text(source.url, maximumPanelLinkBytes)
  if (!title || !url || source.new_tab !== undefined && typeof source.new_tab !== "boolean") throw new Error("workspace.import_invalid")
  const icon = source.icon === undefined ? undefined : text(source.icon, 32)
  if (source.icon !== undefined && (icon === "" || !dashboardLinkIcons.includes(icon as DashboardLinkIcon))) throw new Error("workspace.import_invalid")
  return { title, url, ...(source.new_tab === true ? { new_tab: true } : {}), ...(icon ? { icon: icon as DashboardLinkIcon } : {}) }
}

/** importCellDisplayMode 只接受表格面板上的四种合法模式；其它面板出现该字段按无效文件处理。 */
export function importCellDisplayMode(source: Record<string, unknown>, type: PanelType): Pick<DashboardPanel, "cell_display_mode"> {
  if (source.cell_display_mode === undefined) return {}
  if (type !== "table" || !cellDisplayModes.includes(source.cell_display_mode as DashboardCellDisplayMode)) throw new Error("workspace.import_invalid")
  return { cell_display_mode: source.cell_display_mode as DashboardCellDisplayMode }
}

/** importTableOrientation 只接受表格面板上的两种合法形态；long 是缺省值，不作为字段保留。 */
export function importTableOrientation(source: Record<string, unknown>, type: PanelType): Pick<DashboardPanel, "table_orientation"> {
  if (source.table_orientation === undefined || source.table_orientation === "long") return {}
  if (type !== "table" || !tableOrientations.includes(source.table_orientation as DashboardTableOrientation)) throw new Error("workspace.import_invalid")
  return { table_orientation: source.table_orientation as DashboardTableOrientation }
}

/** importPanelNoValue 只接受会显示占位文本的面板上的短文本（Grafana 的 Standard options → No value）。 */
export function importPanelNoValue(source: Record<string, unknown>, type: PanelType): Pick<DashboardPanel, "no_value"> {
  if (source.no_value === undefined || source.no_value === "") return {}
  if (typeof source.no_value !== "string" || noValueError({ type, no_value: source.no_value })) throw new Error("workspace.import_invalid")
  return { no_value: source.no_value }
}
