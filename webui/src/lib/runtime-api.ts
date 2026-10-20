import { api } from "@/lib/api-client"
import type { RuntimeStatus } from "@/types/runtime"

export async function getRuntimeStatus(signal: AbortSignal) {
  return (await api.get<RuntimeStatus>("/admin/runtime", { signal })).data
}
