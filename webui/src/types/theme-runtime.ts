import type { Group, NetworkTaskView, Node, SiteSettings, TelemetryState } from "@/types"
import type { MetricAggregation, WorkspaceRange } from "@/types/metric-workspace"

export const THEME_PROTOCOL = 3
export const MAX_THEME_MESSAGE_BYTES = 16 * 1024
export const MAX_THEME_SNAPSHOT_BYTES = 1024 * 1024
export const THEME_RELATIVE_RANGE_SECONDS = [300, 900, 3600, 21600, 86400, 604800, 2592000] as const
export type ThemeJSON = null | boolean | number | string | ThemeJSON[] | { [key: string]: ThemeJSON }
export type ThemeDataState = "loading" | "ready" | "error"
export type ThemeFailure = "timeout" | "protocol_error" | "runtime_error" | "snapshot_budget" | "invalid_data"
export type ThemeRange = WorkspaceRange
export interface ThemeRoute { kind: "dashboard" | "node"; node_id?: string }
export interface ThemeHistoryPoint { timestamp: number; value: number | null }
export interface ThemeHistorySeriesInfo { aggregation: MetricAggregation; unit: string }
export interface ThemeHistory {
  from: number
  to: number
  series: Record<string, ThemeHistoryPoint[]>
  series_info?: Record<string, ThemeHistorySeriesInfo>
}
export interface ThemeNetworkHistoryPoint { timestamp: number; latency_ms: number | null }
export interface ThemeNetworkHistoryTask { id: string; points: ThemeNetworkHistoryPoint[] }
export interface ThemeNetworkHistory { from: number; to: number; tasks: ThemeNetworkHistoryTask[] }
export interface ThemeNetworkHistoryInputPoint { timestamp: number; latency_ms: number | null }
export interface ThemeNetworkHistoryInputTask { task_id: string; points: ThemeNetworkHistoryInputPoint[] }
export interface ThemeNetworkHistoryInput { from: number; to: number; tasks: ThemeNetworkHistoryInputTask[] }

export interface ThemeNode {
  id: string
  name: string
  alias: string
  country_code?: string
  group_id: string
  status: "online" | "offline"
  tags: string[]
  public_remark: string
  host?: string
  system: Record<string, string>
  last_seen: string
  telemetry_at?: string | null
  telemetry_age_seconds?: number | null
  telemetry_state?: TelemetryState
  metrics: Record<string, number>
  traffic?: Record<string, ThemeJSON>
}
export interface ThemeSnapshot {
  timestamp: string
  route: ThemeRoute
  site: Pick<SiteSettings, "site_title" | "site_description" | "logo_url" | "favicon_url" | "show_network_quality"> & { show_ip_addresses: boolean }
  locale: string
  theme: "light" | "dark"
  configuration: ThemeJSON
  groups: { id: string; name: string; sort_order: number }[]
  nodes: ThemeNode[]
  network: { visible: boolean; tasks: ThemeJSON[] }
  network_history?: ThemeNetworkHistory
  history?: ThemeHistory
  range?: ThemeRange
  range_options?: number[]
  state: { nodes: ThemeDataState; metrics: ThemeDataState; network: ThemeDataState }
}
export interface ThemeSnapshotInput {
  route: ThemeRoute
  site: SiteSettings
  nodes: Node[]
  groups: Group[]
  locale: string
  theme: "light" | "dark"
  configuration?: string
  network?: NetworkTaskView[] | null
  networkHistory?: ThemeNetworkHistoryInput
  history?: { from: number; to: number; series: Record<string, ThemeHistoryPoint[]> | null; series_info?: Record<string, ThemeHistorySeriesInfo> }
  range?: WorkspaceRange
  customMetricNames?: string[]
  historyMetricNames?: string[]
  state?: Partial<ThemeSnapshot["state"]>
}
export type ThemeFrameMessage =
  | { type: "ready" }
  | { type: "navigate"; payload: { path: string } }
  | { type: "setRange"; payload: ThemeRange }
  | { type: "error"; payload: { code: "render_failed" | "unsupported_data" | "runtime_error" } }
