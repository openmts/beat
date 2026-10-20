import { api } from "@/lib/api"

export type DiagnosticKind = "mtr" | "iperf3"
export type DiagnosticProtocol = "icmp" | "tcp" | "udp"
export type DiagnosticDirection = "forward" | "reverse"
export type DiagnosticIPFamily = "ipv4" | "ipv6"
export type DiagnosticState =
  | "queued"
  | "leased"
  | "running"
  | "succeeded"
  | "partial"
  | "failed"
  | "timed_out"
  | "cancelled"
  | "interrupted"
  | "unknown"

export interface DiagnosticPolicy {
  id: string
  enabled: boolean
  allowed_kinds: DiagnosticKind[]
  allow_udp: boolean
  allow_reverse: boolean
  allow_private_targets: boolean
  maximum_active_jobs: number
  maximum_active_jobs_per_node: number
  daily_byte_budget: number
  maximum_iperf_duration_seconds: number
  maximum_iperf_bitrate_mbps: number
  revision: number
  updated_by: string
  updated_at: string
}

export interface DiagnosticTarget {
  id: string
  name: string
  purpose: string
  kinds: DiagnosticKind[]
  address: string
  port: number
  ip_family: DiagnosticIPFamily
  protocol: DiagnosticProtocol
  direction: DiagnosticDirection
  private_address: boolean
  allowed_addresses: string[]
  resolved_addresses: string[]
  resolved_at: string | null
  maximum_bitrate_mbps: number
  maximum_duration_seconds: number
  maximum_parallel_streams: number
  daily_byte_budget: number
  enabled: boolean
  expires_at: string | null
  revision: number
  created_by: string
  updated_by: string
  created_at: string
  updated_at: string
}

export interface DiagnosticCapability {
  node_id: string
  node_name: string
  digest: string
  seen_at: string
  state: "ready" | "stale" | "partial" | "unavailable" | "invalid"
  kinds: DiagnosticKind[]
}

export interface DiagnosticJob {
  id: string
  source_node_id: string
  target_id: string
  kind: DiagnosticKind
  state: DiagnosticState
  actor_user_id: string
  actor_username: string
  confirmation: string
  source_node_name: string
  target_name: string
  target_revision: number
  policy_revision: number
  protocol: DiagnosticProtocol
  direction: DiagnosticDirection
  ip_family: DiagnosticIPFamily
  probes: number
  hop_limit: number
  duration_seconds: number
  parallel_streams: number
  bitrate_mbps: number
  policy_snapshot: string
  target_snapshot: string
  capability_digest: string
  reserved_bytes: number
  actual_bytes: number
  attempt: number
  cursor_sequence: number
  cancel_requested: boolean
  error_code: string
  created_at: string
  updated_at: string
  leased_at: string | null
  started_at: string | null
  completed_at: string | null
}

export interface DiagnosticHop {
  hostname?: string
  address?: string
  index: number
  reached: boolean
}

export interface DiagnosticJobDetail {
  job: DiagnosticJob
  hops: DiagnosticHop[] | null
}

export interface DiagnosticPoint {
  Timestamp: string
  Value: number
}

export interface DiagnosticSeries {
  metric: string
  unit: string
  hop?: number
  stat?: string
  points: DiagnosticPoint[] | null
}

export interface DiagnosticJobInput {
  source_node_id: string
  target_id: string
  kind: DiagnosticKind
  protocol: DiagnosticProtocol
  direction: DiagnosticDirection
  ip_family: DiagnosticIPFamily
  probes: number
  hop_limit: number
  duration_seconds: number
  parallel_streams: number
  bitrate_mbps: number
  confirmation?: string
}

export interface DiagnosticJobFilter {
  state?: DiagnosticState | ""
  nodeId?: string
  targetId?: string
  limit?: number
}

export interface DiagnosticSeriesQuery {
  metric: string
  hop?: number
  stat?: string
  direction?: string
  protocol?: string
  role?: string
  limit?: number
}

export async function getDiagnosticPolicy(): Promise<DiagnosticPolicy> {
  const response = await api.get<DiagnosticPolicy>("/admin/diagnostics/policy")
  return response.data
}

export async function listDiagnosticTargets(): Promise<DiagnosticTarget[]> {
  const response = await api.get<DiagnosticTarget[]>("/admin/diagnostics/targets")
  return response.data
}

export async function listDiagnosticCapabilities(): Promise<DiagnosticCapability[]> {
  const response = await api.get<DiagnosticCapability[]>("/admin/diagnostics/capabilities")
  return response.data
}

export async function listDiagnosticJobs(filter: DiagnosticJobFilter = {}): Promise<DiagnosticJob[]> {
  const response = await api.get<DiagnosticJob[]>("/admin/diagnostics/jobs", { params: jobParams(filter) })
  return response.data
}

export async function createDiagnosticJob(input: DiagnosticJobInput): Promise<DiagnosticJob> {
  const response = await api.post<DiagnosticJob>("/admin/diagnostics/jobs", input)
  return response.data
}

export async function getDiagnosticJob(jobId: string): Promise<DiagnosticJobDetail> {
  const response = await api.get<DiagnosticJobDetail>(`/admin/diagnostics/jobs/${jobId}`)
  return response.data
}

export async function cancelDiagnosticJob(jobId: string): Promise<void> {
  await api.post(`/admin/diagnostics/jobs/${jobId}/cancel`)
}

export async function retryDiagnosticJob(jobId: string): Promise<DiagnosticJob> {
  const response = await api.post<DiagnosticJob>(`/admin/diagnostics/jobs/${jobId}/retry`)
  return response.data
}

export async function deleteDiagnosticJob(jobId: string): Promise<void> {
  await api.delete(`/admin/diagnostics/jobs/${jobId}`)
}

export async function getDiagnosticSeries(jobId: string, query: DiagnosticSeriesQuery): Promise<DiagnosticSeries> {
  const response = await api.get<DiagnosticSeries>(`/admin/diagnostics/jobs/${jobId}/results`, {
    params: seriesParams(query),
  })
  return response.data
}

function jobParams(filter: DiagnosticJobFilter) {
  return {
    state: filter.state || undefined,
    node_id: filter.nodeId || undefined,
    target_id: filter.targetId || undefined,
    limit: filter.limit,
  }
}

function seriesParams(query: DiagnosticSeriesQuery) {
  return {
    metric: query.metric,
    hop: query.hop || undefined,
    stat: query.stat || undefined,
    direction: query.direction || undefined,
    protocol: query.protocol || undefined,
    role: query.role || undefined,
    limit: query.limit,
  }
}
