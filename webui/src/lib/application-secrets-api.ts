import { api } from "@/lib/api-client"

export interface ApplicationSecretStatus {
  healthy: boolean
  root_available: boolean
  registry_valid: boolean
  backfill_complete: boolean
  rotation: "idle" | "running" | "degraded"
  generation: number
  data_keys: number
  rotation_id?: string
}

export async function getApplicationSecretStatus(): Promise<ApplicationSecretStatus> {
  return (await api.get<ApplicationSecretStatus>("/admin/application-secrets")).data
}

export async function rotateApplicationDataKey(): Promise<ApplicationSecretStatus> {
  return (await api.post<ApplicationSecretStatus>("/admin/application-secrets/rotate")).data
}
