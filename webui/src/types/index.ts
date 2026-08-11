export interface Node {
  id: string
  name: string
  alias: string
  group_id: string
  host: string
  port: number
  status: "online" | "offline"
  ssh_public_key: string
  cpu_model: string
  os: string
  platform: string
  os_version: string
  kernel: string
  arch: string
  virtualization: string
  agent_version: string
  sort_order: number
  tags: string[]
  is_public: boolean
  public_remark: string
  traffic_limit?: number
  traffic_limit_type?: TrafficLimitType
  traffic_reset_day?: number
  last_seen: string
  created_at: string
  updated_at: string
  metrics?: Record<string, number>
  traffic?: TrafficSummary
}

export type SiteTheme = "system" | "light" | "dark"

export interface SiteSettings {
  site_title: string
  site_description: string
  logo_url: string
  favicon_url: string
  default_theme: SiteTheme
  show_ip_addresses: boolean
  show_network_quality: boolean
  updated_at: string
}

export interface MaintenanceSettings {
  retention_days: number
  auto_cleanup_enabled: boolean
  cleanup_hour_utc: number
  updated_at: string
}

export type MaintenanceRunState = "never" | "running" | "success" | "failed"
export type MaintenanceTrigger = "" | "manual" | "automatic"

export interface MaintenanceRunStatus {
  running: boolean
  last_started_at: string | null
  last_completed_at: string | null
  last_status: MaintenanceRunState
  last_error: string
  last_cutoff_at: string | null
  last_duration_ms: number
  last_trigger: MaintenanceTrigger
  sqlite_integrity: string
}

export interface MaintenanceStorageUsage {
  mts_bytes: number
  sqlite_bytes: number
  total_bytes: number
  mts_healthy: boolean
  mts_health_reasons: string[]
}

export interface MaintenanceOverview {
  settings: MaintenanceSettings
  status: MaintenanceRunStatus
  storage: MaintenanceStorageUsage
}

export type AgentCredentialStatus = "legacy" | "active" | "revoked"

export interface ManagedNode extends Node {
  private_remark: string
  agent_credential_status: AgentCredentialStatus
  agent_token_prefix: string
  agent_token_created_at: string | null
  agent_token_last_used_at: string | null
  agent_token_revoked_at: string | null
}

export interface AgentConfig {
  server_url: string
  agent_token?: string
  node_name: string
  advertised_host: string
  ssh_port: number
  report_interval: string
}

export interface NodeCredential {
  node: ManagedNode
  agent_token: string
  agent_config: AgentConfig
}

export interface ManagedNodePayload {
  name: string
  alias?: string
  group_id?: string
  host: string
  port: number
  ssh_public_key?: string
  server_url: string
  report_interval?: string
}

export type TrafficLimitType = "up" | "down" | "sum" | "min" | "max"
export type TrafficStatus = "unlimited" | "normal" | "warning" | "critical" | "exceeded"

export interface TrafficSummary {
  sent: number
  received: number
  used: number
  limit: number
  remaining: number | null
  percentage: number | null
  limit_type: TrafficLimitType
  reset_day: number
  period_start: string
  next_reset: string
  tracked_since: string | null
  status: TrafficStatus
}

export interface Group {
  id: string
  name: string
  sort_order: number
  is_default: boolean
  created_at: string
  updated_at: string
}

export interface SSHKey {
  id: string
  name: string
  key_type: string
  public_key: string
  fingerprint: string
  created_at: string
}

export interface AlertRule {
  id: string
  name: string
  description: string
  metric: string
  operator: string
  threshold: number
  duration: number
  severity: string
  enabled: boolean
  created_at: string
  updated_at: string
}

export interface AlertChannel {
  id: string
  name: string
  channel_type: AlertChannelType
  config: string
  enabled: boolean
  last_delivery?: AlertDeliveryStatus
  created_at: string
  updated_at: string
}

export type AlertChannelType = "webhook" | "telegram" | "email"

export interface AlertDeliveryStatus {
  state: "success" | "failed"
  message: string
  delivered_at: string
}

export interface AlertEvent {
  id: string
  rule_id: string
  node_id: string
  message: string
  value: number
  status: string
  triggered_at: string
  resolved_at: string | null
}

export type TrafficReportCadence = "daily" | "weekly" | "monthly"
export type TrafficReportDeliveryState = "success" | "partial" | "failed"

export interface TrafficReportDeliveryStatus {
  state: TrafficReportDeliveryState
  message: string
  delivered: number
  total: number
  delivered_at: string
}

export interface TrafficReportSchedule {
  id: string
  name: string
  cadence: TrafficReportCadence
  timezone: string
  send_hour: number
  send_minute: number
  weekday: number
  month_day: number
  all_nodes: boolean
  node_ids: string[]
  all_channels: boolean
  channel_ids: string[]
  enabled: boolean
  last_run_at: string | null
  next_run_at: string
  last_delivery?: TrafficReportDeliveryStatus
  created_at: string
  updated_at: string
}

export type TrafficReportSchedulePayload = Omit<
  TrafficReportSchedule,
  "id" | "last_run_at" | "next_run_at" | "last_delivery" | "created_at" | "updated_at"
>

export interface TrafficReportRunResult {
  delivery: TrafficReportDeliveryStatus
  report: {
    schedule_id: string
    schedule_name: string
    cadence: TrafficReportCadence
    timezone: string
  }
}

export interface NodeMetric {
  timestamp: string
  cpu: number
  cpu_used: number
  cpu_total: number
  memory: number
  memory_used: number
  memory_total: number
	  disk: number
  disk_used: number
  disk_total: number
  disk_read: number
  disk_write: number
  net_recv: number
  net_sent: number
	  net_recv_total: number
	  net_sent_total: number
	  swap: number
	  swap_used: number
	  swap_total: number
	  load1: number
	  load5: number
	  load15: number
	  uptime: number
	  processes: number
	  tcp_connections: number
	  udp_connections: number
}

export interface MetricsSnapshot {
	  timestamp: string
	  nodes: Node[]
}

export interface MetricDataPoint {
  timestamp: number
  value: number
}

export type NetworkProbeType = "icmp" | "tcp" | "http"
export type NetworkIPFamily = "auto" | "ipv4" | "ipv6"

export interface NetworkNode {
  id: string
  name: string
  alias: string
}

export interface NetworkProbeLatest {
  timestamp: string
  latency_ms: number
  success: boolean
  status_code: number
  error_code: string
}

export interface NetworkNodeState {
  node: NetworkNode
  latest: NetworkProbeLatest | null
}

export interface NetworkTask {
  id: string
  name: string
  type: NetworkProbeType
  target: string
  ip_family: NetworkIPFamily
  interval_seconds: number
  timeout_milliseconds: number
  all_nodes: boolean
  enabled: boolean
  is_public: boolean
  sort_order: number
  nodes: NetworkNode[]
  created_at: string
  updated_at: string
}

export interface NetworkTaskView {
  task: NetworkTask
  nodes: NetworkNodeState[]
}

export interface NetworkHistoryPoint {
  timestamp: string
  average_latency_ms: number
  success_percent: number
  sample_count: number
}

export interface NetworkHistoryResponse {
  task_id: string
  node_id: string
  from: string
  to: string
  points: NetworkHistoryPoint[]
}

export interface NetworkTaskPayload {
  name: string
  type: NetworkProbeType
  target: string
  ip_family: NetworkIPFamily
  interval_seconds: number
  timeout_milliseconds: number
  all_nodes: boolean
  enabled: boolean
  is_public: boolean
  sort_order: number
  node_ids: string[]
}
export type {
  AdminAuditEvent,
  AdminPrincipal,
  AdminRole,
  AdminSession,
  AdminUser,
  AuditPage,
  AuthState,
  TOTPSetup,
} from "@/types/security"
