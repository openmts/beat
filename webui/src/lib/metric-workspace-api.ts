import { api } from "@/lib/api-client"
import { withWorkspaceReadSlot } from "@/lib/workspace-read-gate"
import type { Group, Node } from "@/types"
import type { DashboardDefinition, MetricCatalog, MetricDashboard, MetricDashboardVersion, MetricDefinition, MetricLatestQuery, MetricLatestResult, MetricQuery, MetricQueryResult } from "@/types/metric-workspace"

export interface WorkspaceSources {
  catalog: MetricCatalog
  dashboards: MetricDashboard[]
  nodes: Node[]
  groups: Group[]
}

export async function getWorkspaceSources(signal: AbortSignal, attempt = 0): Promise<WorkspaceSources> {
  // 失败重试时附加一次性参数，避免浏览器复用上一轮的失败响应。
  const params = attempt > 0 ? { _beat_retry: attempt } : undefined
  const [catalog, dashboards, nodes, groups] = await Promise.all([
    withWorkspaceReadSlot(() => api.get<MetricCatalog | null>("/admin/metrics/catalog", { signal, params })),
    withWorkspaceReadSlot(() => api.get<MetricDashboard[] | null>("/admin/metrics/dashboards", { signal, params })),
    withWorkspaceReadSlot(() => api.get<Node[] | null>("/nodes/manage", { signal, params })),
    withWorkspaceReadSlot(() => api.get<Group[] | null>("/groups", { signal, params })),
  ])
  // 目录缺失时工作区无法渲染，显式失败并给出可诊断的错误码。
  if (!catalog.data) throw new Error("workspace.load_failed")
  return { catalog: catalog.data, dashboards: dashboards.data ?? [], nodes: nodes.data ?? [], groups: groups.data ?? [] }
}

export async function queryWorkspaceMetrics(query: MetricQuery, options: { signal: AbortSignal; public?: boolean }): Promise<MetricQueryResult> {
  const prefix = options.public ? "/metrics" : "/admin/metrics"
  const result = (await withWorkspaceReadSlot(() => api.post<MetricQueryResult>(`${prefix}/query`, query, { signal: options.signal }))).data
  if (!result) throw new Error("workspace.query_failed")
  const produced = result.series ?? []
  if (query.series.some(requested => requested.device_id && !produced.some(series => series.ref_id === requested.ref_id && series.node_id === requested.node_id && series.metric === requested.metric && series.device_id === requested.device_id))) throw new Error("workspace.query_failed")
  return { ...result, series: produced }
}

/** 即时查询：一次最多 200 条序列，服务端复用区间查询的准入与可见性规则。 */
export async function queryWorkspaceLatest(query: MetricLatestQuery, options: { signal: AbortSignal; public?: boolean }): Promise<MetricLatestResult> {
  const prefix = options.public ? "/metrics" : "/admin/metrics"
  const result = (await withWorkspaceReadSlot(() => api.post<MetricLatestResult>(`${prefix}/query/latest`, query, { signal: options.signal }))).data
  if (!result) throw new Error("workspace.query_failed")
  return { series: result.series ?? [] }
}

export async function createMetricDashboard(definition: DashboardDefinition): Promise<MetricDashboard> {
  return (await api.post<MetricDashboard>("/admin/metrics/dashboards", { definition, revision: 0 })).data
}

export async function listMetricDashboards(): Promise<MetricDashboard[]> {
  return (await api.get<MetricDashboard[] | null>("/admin/metrics/dashboards")).data ?? []
}

export async function saveMetricDashboard(dashboard: MetricDashboard): Promise<MetricDashboard> {
  return (await api.put<MetricDashboard>(`/admin/metrics/dashboards/${encodeURIComponent(dashboard.id)}`, {
    definition: dashboard.definition, revision: dashboard.revision,
  })).data
}

export async function rollbackMetricDashboard(dashboard: MetricDashboard): Promise<MetricDashboard> {
  return (await api.post<MetricDashboard>(`/admin/metrics/dashboards/${encodeURIComponent(dashboard.id)}/rollback`, { revision: dashboard.revision })).data
}

export async function deleteMetricDashboard(dashboard: MetricDashboard): Promise<void> {
  await api.delete(`/admin/metrics/dashboards/${encodeURIComponent(dashboard.id)}`, { params: { revision: dashboard.revision } })
}

export async function listDashboardVersions(id: string): Promise<MetricDashboardVersion[]> {
  return (await api.get<MetricDashboardVersion[] | null>(`/admin/metrics/dashboards/${encodeURIComponent(id)}/versions`)).data ?? []
}

export async function dashboardVersionDefinition(id: string, revision: number): Promise<DashboardDefinition> {
  const value = (await api.get<DashboardDefinition | null>(`/admin/metrics/dashboards/${encodeURIComponent(id)}/versions/${revision}`)).data
  if (!value) throw new Error("workspace.load_failed")
  return value
}

/** 恢复历史版本：服务端把它写成新版本，因此历史不会被覆盖。 */
export async function restoreDashboardVersion(dashboard: MetricDashboard, revision: number, summary = ""): Promise<MetricDashboard> {
  return (await api.post<MetricDashboard>(`/admin/metrics/dashboards/${encodeURIComponent(dashboard.id)}/versions/${revision}/restore`, {
    revision: dashboard.revision, summary,
  })).data
}

export async function getPublicDashboard(options: { nodeId?: string; signal: AbortSignal }): Promise<MetricDashboard> {
  const dashboard = (await api.get<MetricDashboard | null>("/metrics/dashboards/default", { params: { node_id: options.nodeId }, signal: options.signal })).data
  if (!dashboard) throw new Error("workspace.load_failed")
  return dashboard
}

export async function updateMetricPolicy(definition: MetricDefinition): Promise<MetricDefinition> {
  return (await api.put<MetricDefinition>(`/admin/metrics/catalog/${encodeURIComponent(definition.key)}`, {
    revision: definition.revision, label: definition.label, description: definition.description,
    default_aggregation: definition.default_aggregation, public: definition.public, enabled: definition.enabled,
    retention_days: definition.retention_override_days,
  })).data
}
