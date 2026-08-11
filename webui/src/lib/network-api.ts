import { api } from "@/lib/api"
import type {
  NetworkHistoryResponse,
  NetworkTask,
  NetworkTaskPayload,
  NetworkTaskView,
} from "@/types"

interface NetworkHistoryQuery {
  nodeId: string
  from: string
  to: string
}

export async function listPublicNetworkQuality(): Promise<NetworkTaskView[]> {
  const response = await api.get<NetworkTaskView[]>("/network/quality")
  return response.data
}

export async function getPublicNetworkHistory(
  taskId: string,
  query: NetworkHistoryQuery,
): Promise<NetworkHistoryResponse> {
  const response = await api.get<NetworkHistoryResponse>(
    `/network/quality/${taskId}/history`,
    { params: historyParams(query) },
  )
  return response.data
}

export async function listNetworkTasks(): Promise<NetworkTaskView[]> {
  const response = await api.get<NetworkTaskView[]>("/network/tasks")
  return response.data
}

export async function createNetworkTask(
  payload: NetworkTaskPayload,
): Promise<NetworkTask> {
  const response = await api.post<NetworkTask>("/network/tasks", payload)
  return response.data
}

export async function updateNetworkTask(
  taskId: string,
  payload: NetworkTaskPayload,
): Promise<NetworkTask> {
  const response = await api.put<NetworkTask>(`/network/tasks/${taskId}`, payload)
  return response.data
}

export async function deleteNetworkTask(taskId: string): Promise<void> {
  await api.delete(`/network/tasks/${taskId}`)
}

export async function sortNetworkTasks(ids: string[]): Promise<void> {
  await api.put("/network/tasks/sort", { ids })
}

export async function getAdminNetworkHistory(
  taskId: string,
  query: NetworkHistoryQuery,
): Promise<NetworkHistoryResponse> {
  const response = await api.get<NetworkHistoryResponse>(
    `/network/tasks/${taskId}/history`,
    { params: historyParams(query) },
  )
  return response.data
}

function historyParams(query: NetworkHistoryQuery) {
  return {
    node_id: query.nodeId,
    from: query.from,
    to: query.to,
  }
}
