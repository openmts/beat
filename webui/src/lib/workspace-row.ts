import type { DashboardDefinition, DashboardPanel } from "@/types/metric-workspace"

/** 行面板（Grafana Row）不查询数据，只把其后的面板归入同一组并记录默认折叠状态。 */
export function isRowPanel(panel: DashboardPanel): boolean {
  return panel.type === "row"
}

/** 文本面板（Grafana Text）不查询数据，只渲染 Markdown 与链接。 */
export function isTextPanel(panel: DashboardPanel): boolean {
  return panel.type === "text"
}

/** 不查询数据的面板：行面板负责分组，文本面板负责说明，两者都不该进入查询队列。 */
export function isStaticPanel(panel: DashboardPanel): boolean {
  return isRowPanel(panel) || isTextPanel(panel)
}

export function rowCollapsed(panel: DashboardPanel): boolean {
  return panel.row_collapsed === true
}

export interface WorkspaceRowGroup {
  /** 组首的行面板；看板开头、任何行之前的面板没有行。 */
  row?: DashboardPanel
  panels: DashboardPanel[]
}

/** rowGroups 按顺序把看板面板切成若干组：每个 row 面板开启新的一组，其后的面板归入该组。 */
export function rowGroups(panels: DashboardPanel[]): WorkspaceRowGroup[] {
  const groups: WorkspaceRowGroup[] = [{ panels: [] }]
  for (const panel of panels) {
    if (isRowPanel(panel)) groups.push({ row: panel, panels: [] })
    else groups.at(-1)!.panels.push(panel)
  }
  return groups
}

/** rowPanelCount 行内的面板数量，用于行标题上的计数提示。 */
export function rowPanelCount(definition: DashboardDefinition, row: DashboardPanel): number {
  return rowGroups(definition.panels).find((group) => group.row?.id === row.id)?.panels.length ?? 0
}

/** visiblePanels 返回当前可见的普通面板：折叠行内的面板不在其中。
 *  折叠行不参与查询是 Grafana 的行为，也是长期打开看板时最直接的省流手段。 */
export function visiblePanels(panels: DashboardPanel[]): DashboardPanel[] {
  return rowGroups(panels).flatMap((group) => group.row && rowCollapsed(group.row) ? [] : group.panels)
}

/** queryablePanels 返回真正会查询数据的面板：行面板与文本面板都不查询。 */
export function queryablePanels(panels: DashboardPanel[]): DashboardPanel[] {
  return visiblePanels(panels).filter((panel) => !isStaticPanel(panel))
}

export interface RowGroupItems<T> extends WorkspaceRowGroup {
  /** 该组面板对应的展开实例；折叠行为空数组，与 queryablePanels 保持一致。 */
  items: T[]
}

/** groupRowItems 把面板实例按所属行分组，供后台与公共节点页共用同一套分组渲染。 */
export function groupRowItems<T extends { base: DashboardPanel }>(panels: DashboardPanel[], items: T[]): RowGroupItems<T>[] {
  const byPanel = new Map<string, T[]>()
  for (const item of items) {
    const bucket = byPanel.get(item.base.id)
    if (bucket) bucket.push(item)
    else byPanel.set(item.base.id, [item])
  }
  return rowGroups(panels).map((group) => ({ ...group, items: group.panels.flatMap((panel) => byPanel.get(panel.id) ?? []) }))
}

/** toggleRowCollapse 只改行面板的折叠位，其余字段保持不变。 */
export function toggleRowCollapse(panel: DashboardPanel): DashboardPanel {
  const collapsed = rowCollapsed(panel)
  return collapsed ? rowFields(panel) : { ...panel, row_collapsed: true }
}

function rowFields(panel: DashboardPanel): DashboardPanel {
  return { id: panel.id, title: panel.title, type: "row", size: panel.size, legend: false, thresholds: [], series: [] }
}

/** applyRowCollapse 覆盖行的折叠状态：公共节点页是只读的，折叠交互只保留在本地状态里。 */
export function applyRowCollapse(panels: DashboardPanel[], overrides: Record<string, boolean>): DashboardPanel[] {
  return panels.map((panel) => {
    const collapsed = isRowPanel(panel) ? overrides[panel.id] : undefined
    if (collapsed === undefined) return panel
    return collapsed ? { ...panel, row_collapsed: true } : rowFields(panel)
  })
}

/** rowPatch 在面板类型切换时清理行专属字段，避免保存出服务端会拒绝的组合。 */
export function rowPatch(type: string): Pick<DashboardPanel, "row_collapsed"> {
  if (type === "row") return {}
  return { row_collapsed: undefined }
}

/** 行面板只保留标题、尺寸与折叠开关，其余字段必须清空，服务端的行校验同样会拒绝这些组合。 */
export function emptyRowPanel(panel: DashboardPanel): DashboardPanel {
  const row = rowFields(panel)
  return panel.row_collapsed === undefined ? row : { ...row, row_collapsed: panel.row_collapsed }
}
