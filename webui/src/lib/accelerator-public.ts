import { acceleratorMetricKeys, isAcceleratorMetric, validAcceleratorValue } from "@/lib/accelerator-metrics"
import type { AcceleratorDevicePublic } from "@/types/accelerator"
export function publicAccelerators(devices: AcceleratorDevicePublic[] | undefined): AcceleratorDevicePublic[] | undefined {
 if (devices === undefined) return undefined
 if (!Array.isArray(devices) || devices.length > 32) return []
 return devices.filter(device => device && /^[a-f0-9]{32}$/.test(device.device_id) && Number.isInteger(device.ordinal) && device.ordinal > 0).map(device => {
  const capabilities = Array.isArray(device.capabilities) ? device.capabilities.filter(isAcceleratorMetric) : []
  const metrics = Object.fromEntries(acceleratorMetricKeys.flatMap(key => capabilities.includes(key) && validAcceleratorValue(key, device.metrics?.[key]) ? [[key, device.metrics[key]]] : []))
  if ((metrics.gpu_memory_used_bytes ?? 0) > (metrics.gpu_memory_total_bytes ?? Infinity)) { delete metrics.gpu_memory_used_bytes; delete metrics.gpu_memory_total_bytes }
  return { device_id: device.device_id, ordinal: device.ordinal, vendor: typeof device.vendor === "string" ? device.vendor.slice(0, 32) : "", model: typeof device.model === "string" ? device.model.slice(0, 256) : "", capabilities, metrics, captured_at: device.captured_at, cadence_ms: device.cadence_ms, stale: acceleratorStale(device) }
 })
}
export function acceleratorDeviceName(device: Pick<AcceleratorDevicePublic, "ordinal" | "model" | "vendor">): string { return `GPU ${device.ordinal} · ${device.model || device.vendor || "GPU"}` }
export function highestUtilization(devices: AcceleratorDevicePublic[]): AcceleratorDevicePublic | undefined {
 return devices.reduce<AcceleratorDevicePublic | undefined>((current, next) => {
  const value = next.metrics.gpu_utilization_percent, previous = current?.metrics.gpu_utilization_percent
  return validAcceleratorValue("gpu_utilization_percent", value) && (previous === undefined || value > previous) ? next : current
 }, undefined)
}
export function mergeAcceleratorSamples(metadata: AcceleratorDevicePublic[] | undefined, incoming: AcceleratorDevicePublic[] | undefined): AcceleratorDevicePublic[] | undefined {
 if (incoming === undefined || metadata === undefined) return metadata
 const current = new Map((publicAccelerators(incoming) ?? []).map(device => [device.device_id, device]))
 return metadata.map(device => {
  const update = current.get(device.device_id)
  if (!update) return { ...device, stale: true }
  const at = Date.parse(update.captured_at ?? ""), previous = Date.parse(device.captured_at ?? "")
  if (!Number.isFinite(at) || Number.isFinite(previous) && at < previous) return device
  return publicAccelerators([{ ...device, metrics: update.metrics, captured_at: update.captured_at, cadence_ms: update.cadence_ms, stale: update.stale }])![0] ?? { ...device, metrics: {}, stale: true }
 })
}
export function acceleratorStale(device: Pick<AcceleratorDevicePublic, "captured_at" | "cadence_ms" | "stale">, now = Date.now()): boolean {
 const captured = Date.parse(device.captured_at ?? "")
 return device.stale || !Number.isFinite(captured) || !Number.isFinite(device.cadence_ms) || device.cadence_ms <= 0 || now - captured > Math.max(3 * device.cadence_ms, 30000)
}
