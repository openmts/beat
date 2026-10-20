import { api } from "@/lib/api-client"
import { APIError } from "@/lib/api-error"
import { dashboardDomain } from "@/lib/workspace-query"
import type { Group, Node } from "@/types"
import type { MetricCatalog, MetricDashboard } from "@/types/metric-workspace"

export interface PublicWorkspaceSources { dashboard: MetricDashboard; catalog: MetricCatalog; nodes: Node[]; groups: Group[] }
interface PublicRequest { nodeId: string; signal: AbortSignal; attempt?: number }
const publicReadTimeout = 10_000

// 首次失败后带 _beat_retry 重试，绕开浏览器或中间层对旧 GET 响应的复用。
function retryParams(attempt: number | undefined): Record<string, number> | undefined {
  return attempt && attempt > 0 ? { _beat_retry: attempt } : undefined
}

export async function getPublicWorkspaceSources(options: PublicRequest): Promise<PublicWorkspaceSources> {
  const request = { signal: options.signal, timeout: publicReadTimeout }
  const retry = retryParams(options.attempt)
  const [template, catalog, nodes, groups] = await Promise.all([
    api.get<MetricDashboard>("/metrics/dashboards/default", { ...request, params: { node_id: options.nodeId, ...retry } }), api.get<MetricCatalog>("/metrics/catalog", { ...request, params: retry }),
    api.get<Node[]>("/nodes", { ...request, params: retry }), api.get<Group[]>("/groups", { ...request, params: retry }),
  ])
  const dashboard = template.data
  // 服务端未配置公共看板时 data 可能为 null；这里显式失败，避免渲染期抛 TypeError。
  if (!dashboard) throw new Error("workspace.load_failed")
  if (dashboard.scope !== "public" && (dashboard.scope !== "node" || dashboard.node_id !== options.nodeId)) throw new Error("workspace.load_failed")
  if (!catalog.data) throw new Error("workspace.load_failed")
  dashboardDomain({ mode: "relative", seconds: dashboard.definition.time_range_seconds }, dashboard.definition)
  const visible = (nodes.data ?? []).filter((node) => node.is_public === true)
  const groupIDs = new Set(visible.map((node) => node.group_id))
  const metrics = (catalog.data.metrics ?? []).filter((metric) => metric.public && metric.enabled)
  return { dashboard, catalog: { ...catalog.data, metrics },
    nodes: visible, groups: (groups.data ?? []).filter((group) => groupIDs.has(group.id)) }
}

export async function getPublicNodeSummary(options: PublicRequest): Promise<Node> {
  const response = await api.get<Node | null>(`/nodes/${encodeURIComponent(options.nodeId)}`, { signal: options.signal, timeout: publicReadTimeout })
  if (!response.data || response.data.id !== options.nodeId || response.data.is_public !== true) throw new APIError("workspace.node_unavailable", 404)
  return response.data
}
