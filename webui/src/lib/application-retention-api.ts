import { api } from "@/lib/api-client"
import type {
  ApplicationRetentionOverview,
  ApplicationRetentionSettings,
} from "@/types/application-retention"

const base = "/settings/application-retention"

export async function getApplicationRetention(
  signal?: AbortSignal,
): Promise<ApplicationRetentionOverview> {
  return (await api.get<ApplicationRetentionOverview>(base, { signal })).data
}

export async function updateApplicationRetention(
  settings: ApplicationRetentionSettings,
): Promise<ApplicationRetentionSettings> {
  return (await api.put<ApplicationRetentionSettings>(base, settings)).data
}

/** 手动清理只提交后台任务；重复触发由服务端返回 409。 */
export async function runApplicationRetention(): Promise<void> {
  await api.post(`${base}/run`)
}
