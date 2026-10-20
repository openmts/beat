import { api } from "@/lib/api-client"
import type {
  EnrollmentApprovalWrite,
  EnrollmentGrant,
  EnrollmentGrantSecret,
  EnrollmentGrantWrite,
  EnrollmentRequest,
  EnrollmentRequestPage,
  EnrollmentRequestQuery,
} from "@/types/enrollment"

const grantsBase = "/admin/agent-enrollment/grants"
const requestsBase = "/admin/agent-enrollment/requests"
const enrollmentTimeout = 15_000

function grantPath(id: string): string {
  return `${grantsBase}/${encodeURIComponent(id)}`
}

/** 授权列表只返回展示前缀，秘密明文永不重复下发。 */
export async function listEnrollmentGrants(signal?: AbortSignal): Promise<EnrollmentGrant[]> {
  return (await api.get<{ grants: EnrollmentGrant[] }>(grantsBase, { signal })).data.grants
}

export async function createEnrollmentGrant(input: EnrollmentGrantWrite): Promise<EnrollmentGrantSecret> {
  const response = await api.post<EnrollmentGrantSecret>(grantsBase, input, { timeout: enrollmentTimeout })
  return response.data
}

export async function updateEnrollmentGrant(id: string, input: EnrollmentGrantWrite): Promise<EnrollmentGrant> {
  return (await api.put<{ grant: EnrollmentGrant }>(grantPath(id), input, { timeout: enrollmentTimeout })).data.grant
}

export async function rotateEnrollmentGrantSecret(id: string): Promise<EnrollmentGrantSecret> {
  const response = await api.post<EnrollmentGrantSecret>(`${grantPath(id)}/rotate`, undefined, {
    timeout: enrollmentTimeout,
  })
  return response.data
}

export async function revokeEnrollmentGrant(id: string): Promise<EnrollmentGrant> {
  return (await api.post<{ grant: EnrollmentGrant }>(`${grantPath(id)}/revoke`)).data.grant
}

/** 请求列表使用不透明游标分页；空字符串状态表示不过滤。 */
export async function listEnrollmentRequests(
  query: EnrollmentRequestQuery,
  signal?: AbortSignal,
): Promise<EnrollmentRequestPage> {
  const params: Record<string, string | number> = {}
  if (query.status) params.status = query.status
  if (query.grant_id) params.grant_id = query.grant_id
  if (query.cursor) params.cursor = query.cursor
  if (query.limit) params.limit = query.limit
  return (await api.get<EnrollmentRequestPage>(requestsBase, { params, signal })).data
}

export async function approveEnrollmentRequest(id: string, input: EnrollmentApprovalWrite): Promise<EnrollmentRequest> {
  const response = await api.post<{ request: EnrollmentRequest }>(
    `${requestsBase}/${encodeURIComponent(id)}/approve`, input, { timeout: enrollmentTimeout },
  )
  return response.data.request
}

export async function rejectEnrollmentRequest(id: string, reason: string): Promise<EnrollmentRequest> {
  const response = await api.post<{ request: EnrollmentRequest }>(
    `${requestsBase}/${encodeURIComponent(id)}/reject`, { reason }, { timeout: enrollmentTimeout },
  )
  return response.data.request
}
