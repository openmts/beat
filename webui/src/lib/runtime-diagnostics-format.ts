import { formatBytes as formatIntlBytes, formatDecimal } from "@/lib/intl-format"

export type RuntimeCaptureKind = "cpu" | "trace" | "heap" | "allocs" | "goroutine" | "mutex" | "block" | "threadcreate"

export const runtimeCaptureKinds: RuntimeCaptureKind[] = ["cpu", "trace", "heap", "allocs", "goroutine", "mutex", "block", "threadcreate"]

/** 目录查询函数，与 useLocale() 的 t 同签名；格式化辅助函数通过它取文案。 */
export type RuntimeTranslate = (key: string, params?: Record<string, string | number>) => string

// 后端返回的是稳定标识，界面文案只在目录里维护；标识缺目录时原样显示，便于发现新增枚举。
export const runtimeKindLabelKeys: Record<string, string> = {
  cpu: "runtime.kind.cpu",
  trace: "runtime.kind.trace",
  heap: "runtime.kind.heap",
  allocs: "runtime.kind.allocs",
  goroutine: "runtime.kind.goroutine",
  mutex: "runtime.kind.mutex",
  block: "runtime.kind.block",
  threadcreate: "runtime.kind.threadcreate",
}

export const runtimeStatusLabelKeys: Record<string, string> = {
  running: "runtime.status.running",
  complete: "runtime.status.complete",
  failed: "runtime.status.failed",
  cancelled: "runtime.status.cancelled",
  expired: "runtime.status.expired",
}

export const runtimeFailureLabelKeys: Record<string, string> = {
  size_limit: "runtime.failure.size_limit",
  writer_failed: "runtime.failure.writer_failed",
  conflict: "runtime.failure.conflict",
  rate_limited: "runtime.failure.rate_limited",
  capacity: "runtime.failure.capacity",
}

export function runtimeLabel(t: RuntimeTranslate, keys: Record<string, string>, value: string): string {
  return t(keys[value] ?? value)
}

export const runtimePollMilliseconds = 5000

// 服务端固定上限：CPU 5–30 秒、trace 1–5 秒；前端只做镜像校验。
const timedBounds: Record<string, { min: number; max: number }> = { cpu: { min: 5, max: 30 }, trace: { min: 1, max: 5 } }

export function runtimeKindIsTimed(kind: string): boolean {
  return kind === "cpu" || kind === "trace"
}

export function runtimeDurationBounds(kind: string): { min: number; max: number } {
  return timedBounds[kind] ?? { min: 0, max: 0 }
}

export function clampRuntimeDuration(kind: string, seconds: number): number {
  const { min, max } = runtimeDurationBounds(kind)
  if (max <= 0) return 0
  if (!Number.isFinite(seconds)) return min
  return Math.min(max, Math.max(min, Math.round(seconds)))
}

export function formatRuntimeBytes(bytes: number, locale?: string): string {
  if (!Number.isFinite(bytes) || bytes <= 0) return "0 B"
  return formatIntlBytes(locale, bytes, { decimals: 1, maximumUnit: "GiB" })
}

export function formatRuntimeUptime(seconds: number, t: RuntimeTranslate): string {
  if (!Number.isFinite(seconds) || seconds <= 0) return t("runtime.uptime_just_started")
  const days = Math.floor(seconds / 86400)
  const hours = Math.floor((seconds % 86400) / 3600)
  const minutes = Math.floor((seconds % 3600) / 60)
  if (days > 0) return t("runtime.uptime_days", { days, hours })
  if (hours > 0) return t("runtime.uptime_hours", { hours, minutes })
  return t("runtime.uptime_minutes", { minutes, seconds: Math.floor(seconds % 60) })
}

export function formatRuntimePause(seconds: number, locale?: string): string {
  if (!Number.isFinite(seconds) || seconds <= 0) return "0 ms"
  return `${formatDecimal(locale, seconds * 1000, 1)} ms`
}
