import { api } from "@/lib/api-client"
import type { ServiceToken, ServiceTokenCredential, ServiceTokenInput, ServiceTokenRotationInput } from "@/types/service-tokens"

const base = "/admin/service-tokens"
const id = encodeURIComponent

export async function listServiceTokens(signal: AbortSignal) {
  return (await api.get<{ items: ServiceToken[] }>(base, { signal })).data.items
}

export async function createServiceToken(input: ServiceTokenInput) {
  return (await api.post<ServiceTokenCredential>(base, input)).data
}

export async function rotateServiceToken(token: string, input: ServiceTokenRotationInput) {
  return (await api.post<ServiceTokenCredential>(`${base}/${id(token)}/rotate`, input)).data
}

export async function revokeServiceToken(token: string, reason: string) {
  return (await api.post<{ revoked: boolean }>(`${base}/${id(token)}/revoke`, { reason })).data
}
