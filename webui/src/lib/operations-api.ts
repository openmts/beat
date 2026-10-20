import { api } from "@/lib/api-client"
import type { OperationOutput, OperationPolicyRecord, OperationSnippet, OperationSnippetInput, OperationTask, OperationTaskInput, OperationTaskPage } from "@/types/operations"
const base = "/admin/operations"
const id = encodeURIComponent
export async function getOperationTasks(cursor: string, signal: AbortSignal) {
 return (await api.get<OperationTaskPage>(`${base}/tasks`, { params: { cursor, limit: 50 }, signal })).data
}
export async function getOperationTask(task: string, signal: AbortSignal) { return (await api.get<OperationTask>(`${base}/tasks/${id(task)}`, { signal })).data }
export async function createOperationTask(input: OperationTaskInput) { return (await api.post<{ id: string }>(`${base}/tasks`, input)).data }
export async function cancelOperationTask(task: string) { await api.post(`${base}/tasks/${id(task)}/cancel`) }
export async function retryOperationTarget(task: string, target: string, input: { reason: string; acknowledge_unknown_risk: boolean }) { await api.post(`${base}/tasks/${id(task)}/targets/${id(target)}/retry`, input) }
export async function getOperationOutput(task: string, target: string) { return (await api.get<OperationOutput>(`${base}/tasks/${id(task)}/targets/${id(target)}/output`)).data }
export async function getExecutablePolicies(signal: AbortSignal) { return (await api.get<OperationPolicyRecord[]>(`${base}/executable-policies`, { signal })).data }
export async function getOperationPolicies(signal: AbortSignal) { return (await api.get<OperationPolicyRecord[]>(`${base}/policies`, { signal })).data }
export async function saveOperationPolicy(input: OperationPolicyRecord) { return (await api.request<{ id: string }>({ url: `${base}/policies${input.id ? `/${id(input.id)}` : ""}`, method: input.id ? "PUT" : "POST", data: input })).data }
export async function disableOperationPolicy(policy: string) { await api.post(`${base}/policies/${id(policy)}/disable`) }
export async function getOperationSnippets(signal: AbortSignal) { return (await api.get<OperationSnippet[]>(`${base}/snippets`, { signal })).data }
export async function saveOperationSnippet(input: OperationSnippetInput) { return (await api.request<OperationSnippet>({ url: `${base}/snippets${input.id ? `/${id(input.id)}` : ""}`, method: input.id ? "PUT" : "POST", data: input })).data }
export async function approveOperationSnippet(snippet: OperationSnippet) { await api.post(`${base}/snippets/${id(snippet.id)}/approve`, { version: snippet.version }) }
export function operationEncode(text: string) {
 const bytes = new TextEncoder().encode(text)
 let binary = ""
 for (let i = 0; i < bytes.length; i += 8192) binary += String.fromCharCode(...bytes.subarray(i, i + 8192))
 return btoa(binary)
}
export function operationDecode(text: string | null) { return text ? new TextDecoder().decode(Uint8Array.from(atob(text), value => value.charCodeAt(0))) : "" }

export async function getOperationNodes(signal: AbortSignal) { return (await api.get<import("@/types").Node[]>("/nodes/manage", { signal })).data }
export async function getOperationSnippetContent(snippet: OperationSnippet, signal?: AbortSignal) { return (await api.get<{ content: string }>(`${base}/snippets/${id(snippet.id)}`, { params: { version: snippet.version }, signal })).data.content }
export async function deleteOperationSnippet(snippet: string) { await api.delete(`${base}/snippets/${id(snippet)}`) }

export async function getOperationGroups(signal: AbortSignal) { return (await api.get<import("@/types").Group[]>("/groups", { signal })).data }

export function operationRequestID() {
 if (typeof crypto.randomUUID === "function") return crypto.randomUUID()
 const bytes = crypto.getRandomValues(new Uint8Array(16))
 bytes[6] = (bytes[6] & 15) | 64; bytes[8] = (bytes[8] & 63) | 128
 const hex = Array.from(bytes, value => value.toString(16).padStart(2, "0")).join("")
 return `${hex.slice(0, 8)}-${hex.slice(8, 12)}-${hex.slice(12, 16)}-${hex.slice(16, 20)}-${hex.slice(20)}`
}

export async function getOperationServerStatus(signal: AbortSignal) { return (await api.get<{ enabled: boolean; spool_configured: boolean; scanner_configured: boolean; maximum_file_bytes: number; chunk_bytes: number }>(`${base}/status`, { signal })).data }
export async function getOperationTaskContent(task: string) { return (await api.get<{ id: string; payload: OperationTaskInput }>(`${base}/tasks/${id(task)}/content`)).data }
