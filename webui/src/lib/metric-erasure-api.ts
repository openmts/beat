import { api } from "@/lib/api-client"
import { newCalibrationRequestID } from "@/lib/traffic-calibration"
import type { BackupRecord } from "@/types/backup"

/** 擦除范围是服务端封闭枚举；前端永远不下发 measurement、tag、时间范围或谓词。 */
export type MetricErasureScope =
  | "node_history"
  | "network_task_history"
  | "fleet_telemetry"
  | "all_metric_history"
  | "delete_node"
  | "delete_network_task"

export type MetricErasureState =
  | "queued"
  | "claimed"
  | "deleting"
  | "compacting"
  | "finalizing"
  | "succeeded"
  | "failed"
  | "canceled"

export type MetricErasureTargetType = "" | "node" | "network_task"

export interface MetricErasureJob {
  id: string
  scope: MetricErasureScope
  state: MetricErasureState
  target_type: MetricErasureTargetType
  target_id: string
  target_name: string
  cutoff?: string
  progress_cursor?: string
  attempts: number
  error_code?: string
  requested_at: string
  started_at?: string
  completed_at?: string
}

export interface MetricErasureCreateInput {
  scope: MetricErasureScope
  target_id?: string
  request_id: string
  confirmation: string
}

export interface MetricErasureAccepted {
  job: MetricErasureJob
  replayed: boolean
}

/** 舰队级范围要求固定短语；实体级范围由服务端比对精确显示名。 */
export const metricErasurePhrases: Record<MetricErasureScope, string> = {
  node_history: "",
  network_task_history: "",
  fleet_telemetry: "ERASE FLEET TELEMETRY",
  all_metric_history: "ERASE ALL METRIC HISTORY",
  delete_node: "",
  delete_network_task: "",
}

export const metricErasureScopes: MetricErasureScope[] = [
  "node_history",
  "network_task_history",
  "fleet_telemetry",
  "all_metric_history",
  "delete_node",
  "delete_network_task",
]

/** 活动状态：服务端仍在持有耐久资源，界面需要轮询进度。 */
const activeErasureStates: ReadonlySet<MetricErasureState> = new Set([
  "queued",
  "claimed",
  "deleting",
  "compacting",
  "finalizing",
])

export const recentBackupWindowMs = 24 * 60 * 60 * 1000

export function newMetricErasureRequestID(): string {
  return newCalibrationRequestID()
}

export function erasurePhrase(scope: MetricErasureScope): string {
  return metricErasurePhrases[scope]
}

export function erasureErasesEntity(scope: MetricErasureScope): boolean {
  return scope === "delete_node" || scope === "delete_network_task"
}

export function erasureTargetKind(scope: MetricErasureScope): "none" | "node" | "task" {
  switch (scope) {
    case "node_history":
    case "delete_node":
      return "node"
    case "network_task_history":
    case "delete_network_task":
      return "task"
    default:
      return "none"
  }
}

export function erasureActive(state: MetricErasureState): boolean {
  return activeErasureStates.has(state)
}

/** 最近 24 小时内成功完成的备份才是可用的恢复点；其它状态不构成恢复证据。 */
export function latestReadyBackup(
  records: BackupRecord[],
  now: number,
): BackupRecord | undefined {
  return records
    .filter((record) => record.state === "ready" || record.state === "validated")
    .filter((record) => now - Date.parse(record.created_at) <= recentBackupWindowMs)
    .sort((left, right) => Date.parse(right.created_at) - Date.parse(left.created_at))[0]
}

export async function listMetricErasureJobs(
  options: { limit?: number; signal?: AbortSignal } = {},
): Promise<MetricErasureJob[]> {
  const response = await api.get<{ jobs: MetricErasureJob[] }>("/admin/metric-erasure/jobs", {
    params: { limit: options.limit },
    signal: options.signal,
  })
  return response.data.jobs ?? []
}

export async function getMetricErasureJob(
  id: string,
  signal?: AbortSignal,
): Promise<MetricErasureJob> {
  const response = await api.get<MetricErasureJob>(
    `/admin/metric-erasure/jobs/${encodeURIComponent(id)}`,
    { signal },
  )
  return response.data
}

export async function createMetricErasureJob(
  input: MetricErasureCreateInput,
): Promise<MetricErasureAccepted> {
  const response = await api.post<MetricErasureAccepted>("/admin/metric-erasure/jobs", input)
  return response.data
}

export async function retryMetricErasureJob(id: string): Promise<MetricErasureJob> {
  const response = await api.post<MetricErasureJob>(
    `/admin/metric-erasure/jobs/${encodeURIComponent(id)}/retry`,
  )
  return response.data
}

export async function cancelMetricErasureJob(id: string): Promise<MetricErasureJob> {
  const response = await api.post<MetricErasureJob>(
    `/admin/metric-erasure/jobs/${encodeURIComponent(id)}/cancel`,
  )
  return response.data
}

/** 节点删除与擦除任务共用同一个 202 契约：只提交耐久任务，不等待删除完成。 */
export async function deleteNodeWithErasure(
  nodeID: string,
  confirmation: string,
  requestID: string,
): Promise<MetricErasureAccepted> {
  const response = await api.delete<MetricErasureAccepted>(
    `/nodes/${encodeURIComponent(nodeID)}`,
    { data: { request_id: requestID, confirmation } },
  )
  return response.data
}

export async function deleteNetworkTaskWithErasure(
  taskID: string,
  confirmation: string,
  requestID: string,
): Promise<MetricErasureAccepted> {
  const response = await api.delete<MetricErasureAccepted>(
    `/network/tasks/${encodeURIComponent(taskID)}`,
    { data: { request_id: requestID, confirmation } },
  )
  return response.data
}
