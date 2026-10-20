import { visiblePanels } from "@/lib/workspace-row"
import { workspaceVariableSelection } from "@/lib/workspace-variable-options"
import type { DashboardDefinition, DashboardPanel, WorkspaceSelections } from "@/types/metric-workspace"
import type { Group, Node } from "@/types"

/** 单个面板最多渲染的重复实例数；超出的节点只在提示中计数。 */
export const maxPanelInstances = 12

export interface WorkspacePanelBinding {
  variable: string
  node_id: string
  label: string
}

export interface WorkspacePanelInstance {
  /** 查询与渲染的唯一键；非重复面板等于面板 ID。 */
  key: string
  /** 渲染用的面板副本：标题已按重复节点替换。 */
  panel: DashboardPanel
  /** 编辑、拖拽、告警与库操作始终作用于基础面板。 */
  base: DashboardPanel
  binding?: WorkspacePanelBinding
}

export function workspacePanelInstances(
  definition: DashboardDefinition, selections: WorkspaceSelections, nodes: Node[], groups: Group[] = [],
): WorkspacePanelInstance[] {
  const instances: WorkspacePanelInstance[] = []
  // 文本面板也要出实例，否则看板里永远不会渲染；查询侧由 WorkspacePoller 过滤静态面板。
  // 折叠行里的面板同样跳过，避免用户看不到的面板持续拉取数据。
  for (const panel of visiblePanels(definition.panels)) instances.push(...panelInstances(panel, definition, selections, nodes, groups))
  return instances
}

/** repeatedNodes 返回该面板按变量展开后的节点数，用于超限提示。 */
export function repeatedNodes(
  panel: DashboardPanel, definition: DashboardDefinition, selections: WorkspaceSelections, nodes: Node[], groups: Group[] = [],
): number {
  return repeatedNodeList(panel, definition, selections, nodes, groups).length
}

/** repeatedNodeList 返回重复面板当前绑定的节点；非重复面板返回空列表。 */
function repeatedNodeList(
  panel: DashboardPanel, definition: DashboardDefinition, selections: WorkspaceSelections, nodes: Node[], groups: Group[],
): Node[] {
  if (!panel.repeat) return []
  const variable = definition.variables.find((value) => value.name === panel.repeat)
  if (!variable) return []
  const selected = workspaceVariableSelection(variable, selections[variable.name] ?? [], nodes, groups)
  if (selected.size === 0) return []
  return nodes
    .filter((node) => variable.type === "group" ? selected.has(node.group_id ?? "") : selected.has(node.id))
    .sort((left, right) => panelNodeLabel(left).localeCompare(panelNodeLabel(right)))
}

function panelInstances(
  panel: DashboardPanel, definition: DashboardDefinition, selections: WorkspaceSelections, nodes: Node[], groups: Group[],
): WorkspacePanelInstance[] {
  const single = { key: panel.id, panel, base: panel }
  if (!panel.repeat) return [single]
  const matched = repeatedNodeList(panel, definition, selections, nodes, groups)
  if (matched.length === 0) return [single]
  return matched.slice(0, maxPanelInstances).map((node) => {
    const label = panelNodeLabel(node)
    return {
      key: `${panel.id}::${node.id}`,
      panel: { ...panel, title: repeatPanelTitle(panel.title, panel.repeat!, label) },
      base: panel,
      binding: { variable: panel.repeat!, node_id: node.id, label },
    }
  })
}

/** 标题里出现 `$变量` 时直接替换，否则追加节点名，保证每个实例可区分。 */
export function repeatPanelTitle(title: string, variable: string, label: string) {
  const token = `$${variable}`
  if (title.includes(token)) return title.split(token).join(label)
  return `${title} · ${label}`
}

function panelNodeLabel(node: Node) {
  return node.alias || node.name || node.id
}
