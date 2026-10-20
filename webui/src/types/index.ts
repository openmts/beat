import type { AcceleratorDevicePublic } from "./accelerator"
import type { LifecyclePublic, LocationPublic } from "@/types/node-lifecycle"
export interface Node {
  location?: LocationPublic
  lifecycle?: LifecyclePublic
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
  telemetry_at?: string | null
  telemetry_age_seconds?: number | null
  telemetry_state?: TelemetryState
  created_at: string
  updated_at: string
  metrics?: Record<string, number>
  accelerators?: AcceleratorDevicePublic[]
  accelerator_visible?: boolean
  traffic?: TrafficSummary
}

export type TelemetryState = "fresh" | "delayed" | "stale" | "unavailable"

export type SiteTheme = "system" | "light" | "dark"

// 公共节点地址展示模式：hidden 清空，masked 只保留网络前缀，full 原样展示。
export type PublicAddressMode = "hidden" | "masked" | "full"

// 公共遥测的展示视图：只含开关、事件白名单与会话采样上限，绝不含来源或样本。
export interface PublicSiteTelemetry {
  enabled: boolean
  allowed_events: string[]
  sample_ceiling: number
}

// owner 读写的匿名聚合遥测策略；修订号用于并发冲突检查。
export interface PublicTelemetryPolicy {
  enabled: boolean
  allowed_events: string[]
  sample_ceiling: number
  retention_days: number
  revision: number
  updated_by: string
  updated_at: string
}

export interface SiteSettings {
  site_title: string
  site_description: string
  logo_url: string
  favicon_url: string
  default_theme: SiteTheme
  public_address_mode: PublicAddressMode
  show_network_quality: boolean
  show_accelerators: boolean
  updated_at: string
  public_telemetry?: PublicSiteTelemetry
}

// 部署访问策略来自环境配置，管理端只读展示模式与数量，永远不下发来源列表。
export interface DeploymentAccessStatus {
  valid: boolean
  public_base_url: string
  api_origin_mode: "all" | "configured" | "same_origin"
  api_origin_count: number
  ws_origin_mode: "all" | "configured" | "same_origin"
  ws_origin_count: number
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
  node: ManagedNodeMetadata
  agent_token: string
  agent_config: AgentConfig
}

export type ManagedNodeMetadata = Omit<ManagedNode,
  "metrics" | "traffic" | "telemetry_at" | "telemetry_age_seconds" | "telemetry_state">

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
  accounting_mode?: "raw" | "calibrated"
  calibrated_at?: string | null
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

export interface GeneratedSSHKey extends SSHKey {
  private_key: string
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

export type { NotificationChannel as AlertChannel, NotificationChannelType as AlertChannelType } from "@/types/notification"

export interface AlertDeliveryStatus {
  state: "success" | "failed"
  message: string
  delivered_at: string
}

export interface AlertEvent {
  device_id?: string
  device_name?: string
  resolution_reason?: string
  id: string
  rule_id: string
  node_id: string
  message: string
  value: number
  status: string
  triggered_at: string
  acknowledged_at: string | null
  resolved_at: string | null
  /** 事件创建时的规则显示名快照；空串表示规则已删除，界面绝不回退成 ID。 */
  rule_name_snapshot?: string
  /** 事件创建时的节点显示名快照；空串表示节点已删除，界面绝不回退成 ID。 */
  node_name_snapshot?: string
}

/** 历史列表一律服务端筛选与游标分页，绝不整表下发。 */
export interface AlertEventPage {
  items: AlertEvent[]
  next_cursor?: string
}

export type TrafficReportCadence = "daily" | "weekly" | "monthly"
export type TrafficReportDeliveryState = "success" | "partial" | "failed" | "queued" | "not_routed" | "unknown"

export interface TrafficReportDeliveryStatus {
  state: TrafficReportDeliveryState
  message: string
  delivered: number
  total: number
  delivered_at?: string
  queued_at?: string
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

export interface CustomMetric {
  id: string
  name: string
  shell_command: string
  interval_seconds: number
  description: string
  enabled: boolean
  created_at: string
  updated_at: string
}

export type CustomMetricHealthState = "ok" | "stale" | "missing" | "unverified" | "disabled"

export interface CustomMetricHealthNode {
  node_id: string
  state: CustomMetricHealthState
  value?: number
  captured_at?: string
  report_at?: string
  age_seconds: number
  sample_id?: string
  cadence_ms?: number
  definition_version?: string
}

export interface CustomMetricHealth {
  metric: {
    id: string
    name: string
    enabled: boolean
    interval_seconds: number
    stale_after_seconds: number
  }
  nodes: CustomMetricHealthNode[]
}

export type CustomMetricProbeStatus = "pending" | "done" | "failed"

export interface CustomMetricProbe {
  task_id: string
  node_id: string
  status: CustomMetricProbeStatus
  success?: boolean
  output?: string
  failure?: string
  completed_at?: string
}

export interface CustomMetricPayload {
  name: string
  shell_command: string
  interval_seconds: number
  description?: string
  enabled?: boolean
}
