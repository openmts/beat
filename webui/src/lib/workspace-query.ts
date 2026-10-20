import { acceleratorSeriesError } from "@/lib/accelerator-metrics"
import { workspaceVariableSelection } from "@/lib/workspace-variable-options"
import type { Group, Node } from "@/types"
import type { DashboardDefinition, DashboardPanel, ExpandedSeries, MetricQuery, WorkspaceRange, WorkspaceSelections } from "@/types/metric-workspace"

const maximumRange = 31 * 24 * 60 * 60 * 1000

// 查询预算与服务端 internal/metricquery.ValidateRequest 一致：每条序列 2..600 个点。
export const minimumQueryPoints = 2
export const maximumQueryPoints = 600
export const defaultQueryPoints = maximumQueryPoints
// 看板 now delay 上限，与服务端 model.MaxDashboardNowDelaySeconds 一致。
export const maximumNowDelaySeconds = 3600

export interface DashboardQueryDefaults {
  max_points?: number
  now_delay_seconds?: number
}

/** 看板默认点数：面板未覆盖时使用，越界值一律回落到上限，避免把非法值发给服务端。 */
export function dashboardQueryPoints(definition: DashboardQueryDefaults): number {
  const value = definition.max_points
  return Number.isInteger(value) && (value as number) >= minimumQueryPoints && (value as number) <= maximumQueryPoints ? (value as number) : defaultQueryPoints
}

/** 面板生效点数：面板覆盖优先，其次看板默认，最后是查询上限。 */
export function panelQueryPoints(panel: { max_points?: number }, definition: DashboardQueryDefaults): number {
  const value = panel.max_points
  return Number.isInteger(value) && (value as number) >= minimumQueryPoints && (value as number) <= maximumQueryPoints ? (value as number) : dashboardQueryPoints(definition)
}

/** 相对范围的 now 会按看板 now delay 整体回退；绝对范围不受影响。 */
export function dashboardDomain(range: WorkspaceRange, definition: DashboardQueryDefaults, now = Date.now()): { from: string; to: string } {
  const delay = range.mode === "relative" ? Math.max(0, definition.now_delay_seconds ?? 0) : 0
  return workspaceDomain(range, now - delay * 1000)
}

export function workspaceDomain(range: WorkspaceRange, now = Date.now()): { from: string; to: string } {
  const from = range.mode === "relative" ? now - range.seconds * 1000 : Date.parse(range.from)
  const to = range.mode === "relative" ? now : Date.parse(range.to)
  if (![from, to].every(Number.isFinite) || from >= to || to - from > maximumRange || from < 0) throw new Error("workspace.invalid_range")
  return { from: new Date(from).toISOString(), to: new Date(to).toISOString() }
}

/** SeriesBinding 把一个重复面板实例固定到某个节点上。 */
export interface SeriesBinding {
  variable: string
  node_id: string
}

export function expandPanelSeries(panel: DashboardPanel, context: {
  definition: DashboardDefinition; selections: WorkspaceSelections; nodes: Node[]; groups?: Group[]; binding?: SeriesBinding
}): ExpandedSeries[] {
  const expanded: ExpandedSeries[] = []
  for (const series of panel.series) {
    const deviceError = acceleratorSeriesError(series)
    if (deviceError) throw new Error(deviceError)
    const selected = selectedSeriesNodes(series.node_id, context)
    if (selected.length === 0) throw new Error("workspace.select_node")
    if (expanded.length + selected.length > 8) throw new Error("workspace.series_limit")
    for (const [index, node] of selected.entries()) {
      expanded.push({ ...series, ref_id: selected.length === 1 ? series.ref_id : `${series.ref_id.slice(0, 56)}_${index + 1}`,
        node_id: node.id, node_name: node.alias || node.name })
    }
  }
  if (new Set(expanded.map((series) => series.ref_id)).size !== expanded.length) throw new Error("workspace.duplicate_series")
  return expanded
}

function selectedSeriesNodes(reference: string, context: { definition: DashboardDefinition; selections: WorkspaceSelections; nodes: Node[]; groups?: Group[]; binding?: SeriesBinding }): Node[] {
  if (context.binding && reference === `$${context.binding.variable}`) return context.nodes.filter((node) => node.id === context.binding!.node_id)
  if (!reference.startsWith("$")) return context.nodes.filter((node) => node.id === reference)
  const name = reference.slice(1)
  const variable = context.definition.variables.find((value) => value.name === name)
  if (!variable) return []
  const selected = workspaceVariableSelection(variable, context.selections[name] ?? [], context.nodes, context.groups ?? [])
  if (selected.size === 0) return []
  return context.nodes.filter((node) => variable.type === "group" ? selected.has(node.group_id ?? "") : selected.has(node.id))
}

export function panelMetricQuery(series: ExpandedSeries[], domain: { from: string; to: string }, maxPoints = defaultQueryPoints): MetricQuery {
  return { ...domain, max_points: maxPoints, series: series.map(({ ref_id, node_id, metric, aggregation, device_id }) => ({ ref_id, node_id, metric, aggregation, ...(device_id ? { device_id } : {}) })) }
}

export async function runWorkspaceQueue<T>(items: T[], execute: (item: T) => Promise<void>, signal: AbortSignal): Promise<void> {
  let next = 0
  async function worker() {
    while (!signal.aborted && next < items.length) {
      const item = items[next++]
      await execute(item)
    }
  }
  await Promise.all(Array.from({ length: Math.min(2, items.length) }, worker))
}
