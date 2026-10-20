import type { NotificationChannelWrite, NotificationReceipt } from "@/types/notification"
import { api } from "@/lib/api-client"
import {
  deleteNodeWithErasure,
  type MetricErasureJob,
} from "@/lib/metric-erasure-api"
import type {
  Node,
  Group,
  SSHKey,
  GeneratedSSHKey,
  AlertRule,
  AlertChannel,
  AlertEvent,
  AlertEventPage,
  MetricDataPoint,
  AgentConfig,
  ManagedNode,
  ManagedNodeMetadata,
  ManagedNodePayload,
  NodeCredential,
  TrafficReportRunResult,
  TrafficReportSchedule,
  TrafficReportSchedulePayload,
  SiteSettings,
  PublicTelemetryPolicy,
  DeploymentAccessStatus,
  MaintenanceOverview,
  MaintenanceSettings,
  CustomMetric,
  CustomMetricHealth,
  CustomMetricPayload,
  CustomMetricProbe,
} from "@/types"
export { api }

export async function listNodes(groupId?: string, signal?: AbortSignal): Promise<Node[]> {
  const params = groupId ? { group_id: groupId } : {}
  const res = await api.get<Node[]>("/nodes", { params, signal })
  return res.data
}

export async function getSiteSettings(): Promise<SiteSettings> {
  const res = await api.get<SiteSettings>("/settings/site")
  return res.data
}

export async function updateSiteSettings(settings: SiteSettings): Promise<SiteSettings> {
  const res = await api.put<SiteSettings>("/settings/site", siteSettingsPayload(settings))
  return res.data
}

// public_telemetry 是 GET 公共响应的只读扩展；后端按严格 JSON 解析 PUT，只接受可编辑字段。
function siteSettingsPayload(settings: SiteSettings): Omit<SiteSettings, "public_telemetry"> {
  return {
    site_title: settings.site_title,
    site_description: settings.site_description,
    logo_url: settings.logo_url,
    favicon_url: settings.favicon_url,
    default_theme: settings.default_theme,
    public_address_mode: settings.public_address_mode,
    show_network_quality: settings.show_network_quality,
    show_accelerators: settings.show_accelerators,
    updated_at: settings.updated_at,
  }
}

export async function getPublicTelemetryPolicy(): Promise<PublicTelemetryPolicy> {
  const res = await api.get<PublicTelemetryPolicy>("/admin/settings/public-telemetry")
  return res.data
}

export async function updatePublicTelemetryPolicy(policy: PublicTelemetryPolicy): Promise<PublicTelemetryPolicy> {
  const res = await api.put<PublicTelemetryPolicy>("/admin/settings/public-telemetry", policy)
  return res.data
}

export async function getDeploymentAccess(): Promise<DeploymentAccessStatus> {
  const res = await api.get<DeploymentAccessStatus>("/admin/settings/deployment")
  return res.data
}

export async function getMaintenanceOverview(): Promise<MaintenanceOverview> {
  const res = await api.get<MaintenanceOverview>("/settings/maintenance")
  return res.data
}

export async function updateMaintenanceSettings(
  settings: MaintenanceSettings,
): Promise<MaintenanceSettings> {
  const res = await api.put<MaintenanceSettings>("/settings/maintenance", settings)
  return res.data
}

export async function startMaintenance(): Promise<void> {
  await api.post("/settings/maintenance/run")
}

export async function getNode(id: string): Promise<Node> {
  const res = await api.get<Node>(`/nodes/${id}`)
  return res.data
}

export async function updateNode(
  id: string,
  data: Partial<Node>,
): Promise<Node> {
  const res = await api.put<Node>(`/nodes/${id}`, data)
  return res.data
}

export async function updateNodeSort(ids: string[]): Promise<void> {
  await api.put("/nodes/sort", { ids })
}

/** 删除节点是异步 202 契约：确认值必须是节点显示名，服务端返回耐久擦除任务。 */
export async function deleteNode(
  id: string,
  confirmation: string,
  requestId: string,
): Promise<MetricErasureJob> {
  const accepted = await deleteNodeWithErasure(id, confirmation, requestId)
  return accepted.job
}

export async function listManagedNodes(signal?: AbortSignal): Promise<ManagedNode[]> {
  const res = await api.get<ManagedNode[]>("/nodes/manage", { signal })
  return res.data
}

export async function createManagedNode(data: ManagedNodePayload): Promise<NodeCredential> {
  const res = await api.post<NodeCredential>("/nodes", data)
  return res.data
}

export async function rotateAgentToken(
  id: string,
  serverUrl: string,
): Promise<NodeCredential> {
  const res = await api.post<NodeCredential>(`/nodes/${id}/token/rotate`, {
    server_url: serverUrl,
  })
  return res.data
}

export async function revokeAgentToken(id: string): Promise<ManagedNodeMetadata> {
  const res = await api.post<ManagedNodeMetadata>(`/nodes/${id}/token/revoke`)
  return res.data
}

export async function getAgentInstallConfig(
  id: string,
  serverUrl: string,
): Promise<AgentConfig> {
  const res = await api.get<AgentConfig>(`/nodes/${id}/install`, {
    params: { server_url: serverUrl },
  })
  return res.data
}

export async function getNodeMetrics(
  id: string,
  metrics?: string[],
  from?: string,
  to?: string,
): Promise<Record<string, MetricDataPoint[]>> {
  const params: Record<string, string> = {}
  if (metrics?.length) params.metric = metrics.join(",")
  if (from) params.from = from
  if (to) params.to = to
  const res = await api.get<Record<string, MetricDataPoint[]>>(
    `/nodes/${id}/metrics`,
    { params },
  )
  return res.data
}

export async function nodeReport(data: {
  name: string
  host: string
  port: number
  metrics?: {
    cpu: number
    cpu_used: number
    cpu_total: number
    memory: number
    memory_used: number
    memory_total: number
    disk_used: number
    disk_total: number
    disk_read: number
    disk_write: number
    net_recv: number
    net_sent: number
  }
}): Promise<void> {
  await api.post("/nodes/report", data)
}

export async function listGroups(signal?: AbortSignal): Promise<Group[]> {
  const res = await api.get<Group[]>("/groups", { signal })
  return res.data
}

export async function createGroup(name: string): Promise<Group> {
  const res = await api.post<Group>("/groups", { name })
  return res.data
}

export async function updateGroup(id: string, name: string): Promise<Group> {
  const res = await api.put<Group>(`/groups/${id}`, { name })
  return res.data
}

export async function deleteGroup(id: string): Promise<void> {
  await api.delete(`/groups/${id}`)
}

export async function setDefaultGroup(id: string): Promise<void> {
  await api.put(`/groups/${id}/default`)
}

export async function updateGroupSort(ids: string[]): Promise<void> {
  await api.put("/groups/sort", { ids })
}

export async function listSSHKeys(signal?: AbortSignal): Promise<SSHKey[]> {
  const res = await api.get<SSHKey[]>("/ssh-keys", { signal })
  return res.data
}

export async function createSSHKey(data: {
  name: string
  public_key: string
  private_key?: string
  key_type?: string
}): Promise<SSHKey> {
  const res = await api.post<SSHKey>("/ssh-keys", data)
  return res.data
}

export async function generateSSHKey(
  name: string,
  keyType: string,
): Promise<GeneratedSSHKey> {
  const res = await api.post<GeneratedSSHKey>("/ssh-keys/generate", { name, key_type: keyType })
  return res.data
}

export async function getSSHKey(id: string): Promise<GeneratedSSHKey> {
  const res = await api.get<GeneratedSSHKey>(`/ssh-keys/${id}`)
  return res.data
}

export async function deleteSSHKey(id: string): Promise<void> {
  await api.delete(`/ssh-keys/${id}`)
}

export async function listAlertRules(signal?: AbortSignal): Promise<AlertRule[]> {
  const res = await api.get<AlertRule[]>("/alerts/rules", { signal })
  return res.data
}

export async function createAlertRule(
  data: Omit<AlertRule, "id" | "created_at" | "updated_at">,
): Promise<AlertRule> {
  const res = await api.post<AlertRule>("/alerts/rules", data)
  return res.data
}

export async function updateAlertRule(
  id: string,
  data: Partial<AlertRule>,
): Promise<AlertRule> {
  const res = await api.put<AlertRule>(`/alerts/rules/${id}`, data)
  return res.data
}

export async function deleteAlertRule(id: string): Promise<void> {
  await api.delete(`/alerts/rules/${id}`)
}

export async function listAlertChannels(signal?: AbortSignal): Promise<AlertChannel[]> {
  const res = await api.get<AlertChannel[]>("/alerts/channels", { signal })
  return res.data
}

export async function createAlertChannel(
  data: NotificationChannelWrite,
): Promise<AlertChannel> {
  const res = await api.post<AlertChannel>("/alerts/channels", data)
  return res.data
}

export async function updateAlertChannel(
  id: string,
  data: NotificationChannelWrite,
): Promise<AlertChannel> {
  const res = await api.put<AlertChannel>(`/alerts/channels/${id}`, data)
  return res.data
}

export async function deleteAlertChannel(id: string): Promise<void> {
  await api.delete(`/alerts/channels/${id}`)
}

export async function testAlertChannel(id: string): Promise<NotificationReceipt> {
  const res = await api.post<NotificationReceipt>(`/alerts/channels/${id}/test`)
  return res.data
}

/** 历史告警事件的服务端筛选与游标分页输入；过滤条件由服务端归一化并绑定到游标。 */
export interface AlertEventListQuery {
  status?: string
  ruleId?: string
  nodeId?: string
  from?: string
  to?: string
  cursor?: string
  limit?: number
  signal?: AbortSignal
}

export async function listAlertEvents(query: AlertEventListQuery = {}): Promise<AlertEventPage> {
  const res = await api.get<AlertEventPage>("/alerts/events", {
    params: {
      status: query.status, rule_id: query.ruleId, node_id: query.nodeId,
      from: query.from, to: query.to, cursor: query.cursor, limit: query.limit,
    },
    signal: query.signal,
  })
  return res.data
}

export async function acknowledgeAlertEvent(id: string): Promise<AlertEvent> {
  const res = await api.put<AlertEvent>(`/alerts/events/${id}/acknowledge`)
  return res.data
}

export async function resolveAlertEvent(id: string): Promise<AlertEvent> {
  const res = await api.put<AlertEvent>(`/alerts/events/${id}/resolve`)
  return res.data
}

export async function listTrafficReportSchedules(signal?: AbortSignal): Promise<TrafficReportSchedule[]> {
  const res = await api.get<TrafficReportSchedule[]>("/alerts/traffic-reports", { signal })
  return res.data
}

export async function createTrafficReportSchedule(
  data: TrafficReportSchedulePayload,
): Promise<TrafficReportSchedule> {
  const res = await api.post<TrafficReportSchedule>("/alerts/traffic-reports", data)
  return res.data
}

export async function updateTrafficReportSchedule(
  id: string,
  data: TrafficReportSchedulePayload,
): Promise<TrafficReportSchedule> {
  const res = await api.put<TrafficReportSchedule>(`/alerts/traffic-reports/${id}`, data)
  return res.data
}

export async function deleteTrafficReportSchedule(id: string): Promise<void> {
  await api.delete(`/alerts/traffic-reports/${id}`)
}

export async function testTrafficReportSchedule(id: string): Promise<TrafficReportRunResult> {
  const res = await api.post<TrafficReportRunResult>(`/alerts/traffic-reports/${id}/test`)
  return res.data
}

export interface BatchCommandResult {
  node_id: string
  node_name?: string
  output?: string
  error?: string
}

export async function executeBatchCommand(
  nodeIds: string[],
  command: string,
): Promise<BatchCommandResult[]> {
  const res = await api.post<BatchCommandResult[]>("/terminal/execute", {
    node_ids: nodeIds,
    command,
  })
  return res.data
}

export async function listCustomMetrics(signal?: AbortSignal): Promise<CustomMetric[]> {
  const res = await api.get<CustomMetric[] | null>("/custom-metrics", { signal })
  return res.data ?? []
}

export async function createCustomMetric(data: CustomMetricPayload): Promise<CustomMetric> {
  const res = await api.post<CustomMetric>("/custom-metrics", data)
  return res.data
}

export async function updateCustomMetric(id: string, data: CustomMetricPayload): Promise<CustomMetric> {
  const res = await api.put<CustomMetric>(`/custom-metrics/${id}`, data)
  return res.data
}

export async function deleteCustomMetric(id: string): Promise<void> {
  await api.delete(`/custom-metrics/${id}`)
}

export async function listCustomMetricNodes(metricId: string): Promise<string[]> {
  const res = await api.get<string[] | null>(`/custom-metrics/${metricId}/nodes`)
  return res.data ?? []
}

export interface NodeCustomMetric {
  id: string
  name: string
  interval_seconds: number
}

export async function listNodeCustomMetrics(nodeId: string): Promise<NodeCustomMetric[]> {
  const res = await api.get<NodeCustomMetric[] | null>(`/nodes/${nodeId}/custom-metrics`)
  return res.data ?? []
}

export async function getCustomMetricHealth(metricId: string, signal?: AbortSignal): Promise<CustomMetricHealth> {
  const res = await api.get<CustomMetricHealth>(`/custom-metrics/${metricId}/health`, { signal })
  return res.data
}

export async function setCustomMetricNodes(metricId: string, nodeIds: string[]): Promise<string[]> {
  const res = await api.put<string[] | null>(`/custom-metrics/${metricId}/nodes`, { node_ids: nodeIds })
  return res.data ?? []
}

export async function probeCustomMetric(metricId: string, nodeId: string): Promise<CustomMetricProbe> {
  const res = await api.post<CustomMetricProbe>(`/custom-metrics/${metricId}/probe`, { node_id: nodeId })
  return res.data
}

export async function getCustomMetricProbe(metricId: string, taskId: string): Promise<CustomMetricProbe> {
  const res = await api.get<CustomMetricProbe>(`/custom-metrics/${metricId}/probe/${taskId}`)
  return res.data
}
