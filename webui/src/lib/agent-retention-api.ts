import { api } from "@/lib/api-client"
import type { AgentRetentionSettings } from "@/types/agent-retention"

const base = "/settings/agent-release-retention"

export async function getAgentRetention(signal?: AbortSignal): Promise<AgentRetentionSettings> {
  return (await api.get<AgentRetentionSettings>(base, { signal })).data
}

export async function updateAgentRetention(
  input: { rollout_days: number; release_days: number },
): Promise<AgentRetentionSettings> {
  return (await api.put<AgentRetentionSettings>(base, input)).data
}

/** 恢复缺省：只清后台配置，保留最近一轮清理记录。 */
export async function resetAgentRetention(): Promise<AgentRetentionSettings> {
  return (await api.delete<AgentRetentionSettings>(base)).data
}

/** 手动触发一轮清理；没有生效保留期时服务端返回 409。 */
export async function runAgentRetention(): Promise<AgentRetentionSettings> {
  return (await api.post<AgentRetentionSettings>(`${base}/run`)).data
}
