import { sha256 } from "@noble/hashes/sha2.js"
import { bytesToHex } from "@noble/hashes/utils.js"
import { api } from "@/lib/api-client"
import type { OperationTransfer, OperationTransferInput } from "@/types/operations"
const base = "/admin/operations/transfers"
export const operationChunkBytes = 4 * 1024 * 1024
export async function getOperationTransfers(signal: AbortSignal) { return (await api.get<OperationTransfer[]>(base, { signal })).data }
export async function getOperationTransfer(id: string, signal: AbortSignal) { return (await api.get<OperationTransfer>(`${base}/${encodeURIComponent(id)}`, { signal })).data }
export async function createOperationTransfer(input: OperationTransferInput) { return (await api.post<OperationTransfer>(base, input)).data }
export async function finalizeOperationTransfer(id: string) { return (await api.post<{ task_id: string }>(`${base}/${encodeURIComponent(id)}/finalize`)).data }
export async function cancelOperationTransfer(id: string) { await api.post(`${base}/${encodeURIComponent(id)}/cancel`) }
export async function discardOperationTransferDraft(id: string) { await api.delete(`${base}/${encodeURIComponent(id)}`) }
export const operationDownloadURL = (id: string) => `/api/v1${base}/${encodeURIComponent(id)}/download`
export async function hashOperationFile(file: File, options: { signal: AbortSignal; progress: (bytes: number) => void }) {
 const hash = sha256.create()
 try {
  for (let offset = 0; offset < file.size; offset += operationChunkBytes) {
   options.signal.throwIfAborted()
   const bytes = new Uint8Array(await file.slice(offset, offset + operationChunkBytes).arrayBuffer())
   hash.update(bytes); options.progress(Math.min(offset + bytes.length, file.size)); bytes.fill(0)
  }
  options.signal.throwIfAborted()
  return bytesToHex(hash.digest())
 } finally { hash.destroy() }
}
export async function uploadOperationFile(transfer: OperationTransfer, file: File, options: { signal: AbortSignal; progress: (bytes: number) => void }) {
 if (file.size !== transfer.size || await hashOperationFile(file, { ...options, progress: () => undefined }) !== transfer.digest) throw new Error("The selected file does not match this transfer")
 const missing = transfer.missing_chunks ?? []
 let uploaded = file.size - missing.reduce((sum, index) => sum + Math.min(operationChunkBytes, file.size - index * operationChunkBytes), 0)
 options.progress(uploaded)
 for (const index of missing) {
  options.signal.throwIfAborted()
  const start = index * operationChunkBytes, end = Math.min(start + operationChunkBytes, file.size)
  const body = new Uint8Array(await file.slice(start, end).arrayBuffer())
  try {
   await api.post(`${base}/${encodeURIComponent(transfer.id)}/chunks`, body, { signal: options.signal, headers: { "Content-Type": "application/octet-stream", "X-Beat-Chunk-Index": String(index), "X-Content-SHA256": bytesToHex(sha256(body)), "Content-Range": `bytes ${start}-${end - 1}/${file.size}` } })
   uploaded += body.length; options.progress(uploaded)
  } finally { body.fill(0) }
 }
}
