import { api } from "@/lib/api-client"
import type { RuntimeCapture, RuntimeCaptureKind, RuntimeCapturePage, RuntimeSummary } from "@/types/runtime-diagnostics"

const base = "/admin/runtime"
const id = encodeURIComponent

export async function getRuntimeDiagnosticsSummary(signal: AbortSignal) {
  return (await api.get<RuntimeSummary>(`${base}/summary`, { signal })).data
}

export async function listRuntimeCaptures(signal: AbortSignal) {
  return (await api.get<RuntimeCapturePage>(`${base}/captures`, { signal })).data
}

export async function startRuntimeCapture(input: { kind: RuntimeCaptureKind; duration_seconds: number }) {
  return (await api.post<RuntimeCapture>(`${base}/captures`, input)).data
}

export async function cancelRuntimeCapture(capture: string) {
  await api.delete(`${base}/captures/${id(capture)}`)
}

export async function downloadRuntimeCapture(capture: string) {
  const response = await api.get<Blob>(`${base}/captures/${id(capture)}/download`, { responseType: "blob" })
  return response.data
}

export function captureFileName(capture: RuntimeCapture) {
  const safe = /^[0-9a-f]{1,64}$/.test(capture.id) && /^[a-z]+$/.test(capture.kind)
  return safe ? `${capture.kind}-${capture.id}.prof` : "capture.prof"
}

export function saveCapturedProfile(capture: RuntimeCapture, blob: Blob) {
  const url = URL.createObjectURL(blob)
  const anchor = document.createElement("a")
  anchor.href = url
  anchor.download = captureFileName(capture)
  anchor.rel = "noopener"
  anchor.click()
  URL.revokeObjectURL(url)
}
