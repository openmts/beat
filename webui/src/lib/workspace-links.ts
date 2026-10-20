// 面板数据链接：Grafana 的 Data links。链接本身只存标题与 URL 模板，取值在渲染时按当前面板
// 上下文替换；服务端只校验形态（条数、标题、协议），因此这里与 model.DashboardPanelLink 对齐。

import { expandPanelSeries } from "@/lib/workspace-query"
import { dashboardLinkIcons } from "@/types/metric-workspace"
import type { DashboardDefinition, DashboardLinkIcon, DashboardPanel, DashboardPanelLink, WorkspaceRange, WorkspaceSelections } from "@/types/metric-workspace"
import type { Group, Node } from "@/types"

export const maximumPanelLinks = 5
export const maximumDashboardLinks = 8
export const maximumPanelLinkTitleLength = 40
export const maximumPanelLinkBytes = 1024

/** 占位符清单：编辑器与文档共用，避免文案与实际支持的变量漂移。 */
export const panelLinkPlaceholders = ["node", "node_name", "group", "group_name", "metric", "aggregation", "from", "to", "__url_time_range", "__all_variables"] as const

export function panelLinks(panel: DashboardPanel): DashboardPanelLink[] {
  return Array.isArray(panel.links) ? panel.links : []
}

export function emptyPanelLink(): DashboardPanelLink {
  return { title: "", url: "" }
}

/** linkIcon 把面板配置归一成可渲染的图标名：未知或缺省都退回 external，渲染器不必再判断。 */
export function linkIcon(link: DashboardPanelLink): DashboardLinkIcon {
  return dashboardLinkIcons.includes(link.icon as DashboardLinkIcon) ? link.icon as DashboardLinkIcon : "external"
}

function byteLength(value: string): number {
  return new TextEncoder().encode(value).length
}

/** 单条链接的校验：标题非空且有界，URL 必须是绝对 http(s) 或站内相对路径。 */
export function panelLinkError(link: DashboardPanelLink): string | null {
  if (!link.title.trim() || byteLength(link.title) > maximumPanelLinkTitleLength) return "workspace.link_invalid"
  if (!link.url.trim() || byteLength(link.url) > maximumPanelLinkBytes) return "workspace.link_invalid"
  if (/[\s\u0000-\u001f\u007f]/.test(link.url)) return "workspace.link_invalid"
  const absolute = link.url.startsWith("https://") || link.url.startsWith("http://")
  if (!absolute && !(link.url.startsWith("/") && !link.url.startsWith("//"))) return "workspace.link_invalid"
  if (link.icon !== undefined && !dashboardLinkIcons.includes(link.icon)) return "workspace.link_invalid"
  return null
}

/** 看板级导航链接沿用同一套标题/URL 规则，只放宽条数。 */
export function dashboardLinks(definition: DashboardDefinition): DashboardPanelLink[] {
  return Array.isArray(definition.links) ? definition.links : []
}

export function dashboardLinksError(links: DashboardPanelLink[]): string | null {
  if (links.length > maximumDashboardLinks) return "workspace.link_limit"
  return links.map(panelLinkError).find(Boolean) ?? null
}

/** 只读视图是否隐藏链接：公开节点页面向终端用户，作者可以把运维入口留在后台。 */
export function linksHiddenInReadOnly(definition: DashboardDefinition): boolean {
  return definition.hide_links === true
}

/** 看板级上下文：没有面板，只能提供变量、时间范围与看板标识。 */
export function dashboardLinkValues(context: {
  selections: WorkspaceSelections
  range: WorkspaceRange
  domain?: { from: string; to: string }
  dashboardId?: string
}): Record<string, string> {
  const range = context.domain ?? rangeTokens(context.range)
  const values: Record<string, string> = {
    from: range.from, to: range.to, __dashboard: context.dashboardId ?? "",
    __url_time_range: `from=${encodeURIComponent(range.from)}&to=${encodeURIComponent(range.to)}`,
  }
  Object.assign(values, variableLinkValues(context.selections))
  return values
}

/** 变量取值：每个变量一个 `${var-<name>}`，另加 Grafana 的 `${__all_variables}`（整段查询串）。 */
function variableLinkValues(selections: WorkspaceSelections): Record<string, string> {
  const values: Record<string, string> = {}
  const pairs: string[] = []
  for (const [name, selected] of Object.entries(selections)) {
    const joined = selected.join(",")
    values[`var-${name}`] = joined
    pairs.push(`var-${name}=${joined}`)
  }
  values.__all_variables = pairs.join("&")
  return values
}

export function panelLinksError(links: DashboardPanelLink[]): string | null {
  if (links.length > maximumPanelLinks) return "workspace.link_limit"
  return links.map(panelLinkError).find(Boolean) ?? null
}

function placeholder(name: string): string {
  return "$" + "{" + name + "}"
}

/** 替换已知占位符；未知占位符原样保留，便于作者看出拼写错误而不是拿到一个静默坏链接。 */
export function resolvePanelLinkURL(url: string, values: Record<string, string>): string {
  let resolved = url
  for (const [key, value] of Object.entries(values)) resolved = resolved.split(placeholder(key)).join(value)
  return resolved
}

function rangeTokens(range: WorkspaceRange): { from: string; to: string } {
  if (range.mode === "absolute") return { from: range.from, to: range.to }
  const to = new Date()
  const from = new Date(to.getTime() - range.seconds * 1000)
  return { from: from.toISOString(), to: to.toISOString() }
}

/** 面板实例 → 占位符取值。序列无法展开时仍返回时间与变量，让链接保持可用。 */
export function panelLinkValues(context: {
  panel: DashboardPanel
  definition: DashboardDefinition
  selections: WorkspaceSelections
  nodes: Node[]
  groups?: Group[]
  range: WorkspaceRange
  domain?: { from: string; to: string }
}): Record<string, string> {
  let expanded: ReturnType<typeof expandPanelSeries> = []
  try {
    expanded = expandPanelSeries(context.panel, { definition: context.definition, selections: context.selections, nodes: context.nodes, groups: context.groups })
  } catch {
    expanded = []
  }
  const first = expanded[0]
  const range = context.domain ?? rangeTokens(context.range)
  const node = first ? context.nodes.find((value) => value.id === first.node_id) : undefined
  const group = context.groups?.find((value) => value.id === node?.group_id)
  const values: Record<string, string> = {
    node: first?.node_id ?? "", node_name: first?.node_name ?? "", group: node?.group_id ?? "", group_name: group?.name ?? "",
    metric: first?.metric ?? "", aggregation: first?.aggregation ?? "", from: range.from, to: range.to,
    __url_time_range: `from=${encodeURIComponent(range.from)}&to=${encodeURIComponent(range.to)}`,
  }
  Object.assign(values, variableLinkValues(context.selections))
  return values
}
