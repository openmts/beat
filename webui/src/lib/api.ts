import { api } from "@/lib/api-client"
import type {
  Node,
  Group,
  SSHKey,
  AlertRule,
  AlertChannel,
  AlertEvent,
  MetricDataPoint,
  AgentConfig,
  ManagedNode,
  ManagedNodePayload,
  NodeCredential,
  TrafficReportRunResult,
  TrafficReportSchedule,
  TrafficReportSchedulePayload,
  SiteSettings,
  MaintenanceOverview,
  MaintenanceSettings,
} from "@/types"
export { api }

export async function listNodes(groupId?: string): Promise<Node[]> {
  const params = groupId ? { group_id: groupId } : {}
  const res = await api.get<Node[]>("/nodes", { params })
  return res.data
}

export async function getSiteSettings(): Promise<SiteSettings> {
  const res = await api.get<SiteSettings>("/settings/site")
  return res.data
}

export async function updateSiteSettings(settings: SiteSettings): Promise<SiteSettings> {
  const res = await api.put<SiteSettings>("/settings/site", settings)
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

export async function deleteNode(id: string): Promise<void> {
  await api.delete(`/nodes/${id}`)
}

export async function listManagedNodes(): Promise<ManagedNode[]> {
  const res = await api.get<ManagedNode[]>("/nodes/manage")
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

export async function revokeAgentToken(id: string): Promise<ManagedNode> {
  const res = await api.post<ManagedNode>(`/nodes/${id}/token/revoke`)
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

export async function listGroups(): Promise<Group[]> {
  const res = await api.get<Group[]>("/groups")
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

export async function listSSHKeys(): Promise<SSHKey[]> {
  const res = await api.get<SSHKey[]>("/ssh-keys")
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
): Promise<SSHKey> {
  const res = await api.post<SSHKey>("/ssh-keys/generate", { name, key_type: keyType })
  return res.data
}

export async function deleteSSHKey(id: string): Promise<void> {
  await api.delete(`/ssh-keys/${id}`)
}

export async function listAlertRules(): Promise<AlertRule[]> {
  const res = await api.get<AlertRule[]>("/alerts/rules")
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

export async function listAlertChannels(): Promise<AlertChannel[]> {
  const res = await api.get<AlertChannel[]>("/alerts/channels")
  return res.data
}

export async function createAlertChannel(
  data: Omit<AlertChannel, "id" | "created_at" | "updated_at">,
): Promise<AlertChannel> {
  const res = await api.post<AlertChannel>("/alerts/channels", data)
  return res.data
}

export async function updateAlertChannel(
  id: string,
  data: Partial<AlertChannel>,
): Promise<AlertChannel> {
  const res = await api.put<AlertChannel>(`/alerts/channels/${id}`, data)
  return res.data
}

export async function deleteAlertChannel(id: string): Promise<void> {
  await api.delete(`/alerts/channels/${id}`)
}

export async function testAlertChannel(id: string): Promise<AlertChannel["last_delivery"]> {
  const res = await api.post<NonNullable<AlertChannel["last_delivery"]>>(`/alerts/channels/${id}/test`)
  return res.data
}

export async function listAlertEvents(): Promise<AlertEvent[]> {
  const res = await api.get<AlertEvent[]>("/alerts/events")
  return res.data
}

export async function listTrafficReportSchedules(): Promise<TrafficReportSchedule[]> {
  const res = await api.get<TrafficReportSchedule[]>("/alerts/traffic-reports")
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
