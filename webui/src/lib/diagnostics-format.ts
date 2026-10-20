import type { DiagnosticJob, DiagnosticState } from "@/lib/diagnostics-api"
import { formatBytes as formatIntlBytes, formatInteger, formatNumber as formatIntlNumber } from "@/lib/intl-format"

export const diagnosticStates: DiagnosticState[] = [
  "queued",
  "leased",
  "running",
  "succeeded",
  "partial",
  "failed",
  "timed_out",
  "cancelled",
  "interrupted",
  "unknown",
]

export type DiagnosticBadgeVariant = "default" | "secondary" | "destructive" | "outline"

// 活动状态与终态分开配色：只有失败类终态使用破坏色。
const stateVariants: Record<DiagnosticState, DiagnosticBadgeVariant> = {
  queued: "outline",
  leased: "secondary",
  running: "secondary",
  succeeded: "default",
  partial: "secondary",
  failed: "destructive",
  timed_out: "destructive",
  cancelled: "outline",
  interrupted: "destructive",
  unknown: "destructive",
}

export function diagnosticStateVariant(state: DiagnosticState): DiagnosticBadgeVariant {
  return stateVariants[state] ?? "outline"
}

export function diagnosticActive(state: DiagnosticState): boolean {
  return state === "queued" || state === "leased" || state === "running"
}

export function diagnosticRetryable(state: DiagnosticState): boolean {
  return state !== "succeeded" && state !== "running" && state !== "leased" && state !== "queued"
}

export interface DiagnosticTimelineStep {
  key: string
  at: string | null
  reached: boolean
  current: boolean
}

// diagnosticTimeline 编码真实状态：授权、租约、启动、测量、完成。
export function diagnosticTimeline(job: DiagnosticJob): DiagnosticTimelineStep[] {
  const measured = job.cursor_sequence > 0
  const steps = [
    { key: "authorized", at: job.created_at, reached: true },
    { key: "leased", at: job.leased_at, reached: job.leased_at !== null },
    { key: "started", at: job.started_at, reached: job.started_at !== null },
    { key: "measured", at: measured ? job.updated_at : null, reached: measured },
    { key: "completed", at: job.completed_at, reached: job.completed_at !== null },
  ]
  const last = steps.reduce((index, step, position) => (step.reached ? position : index), 0)
  return steps.map((step, position) => ({
    ...step,
    current: position === last && job.completed_at === null,
  }))
}

// maskDiagnosticAddress 只保留可辨认的网络前缀，跳地址默认不完整暴露。
export function maskDiagnosticAddress(address?: string): string {
  if (!address) return "—"
  if (address.includes(":")) {
    const groups = address.split(":").filter((group) => group.length > 0)
    return `${groups.slice(0, 2).join(":")}:****`
  }
  const parts = address.split(".")
  if (parts.length !== 4) return "****"
  return `${parts[0]}.${parts[1]}.${parts[2]}.***`
}

export function formatDiagnosticValue(metric: string, value: number, locale?: string): string {
  if (!Number.isFinite(value)) return "—"
  switch (metric) {
    case "diagnostic_iperf_bytes_per_second":
      return formatBytes(value, locale, { perSecond: true })
    case "diagnostic_iperf_bytes":
      return formatBytes(value, locale)
    case "diagnostic_mtr_latency_ms":
    case "diagnostic_iperf_jitter_ms":
      return `${formatNumber(value, locale, 2)} ms`
    case "diagnostic_mtr_loss_percent":
    case "diagnostic_iperf_loss_percent":
      return `${formatNumber(value, locale, 1)}%`
    case "diagnostic_iperf_retransmits":
    case "diagnostic_mtr_sent_packets":
    case "diagnostic_mtr_received_packets":
      return formatInteger(locale, value)
    default:
      return formatNumber(value, locale, 2)
  }
}

/** 诊断读数使用两位小数并最多展示到 TiB；速率由调用方追加 /s。 */
export function formatBytes(value: number, locale?: string, options: { perSecond?: boolean } = {}): string {
  return formatIntlBytes(locale, value, { decimals: 2, perSecond: options.perSecond })
}

function formatNumber(value: number, locale: string | undefined, digits: number): string {
  return formatIntlNumber(locale, value, { minimumFractionDigits: 0, maximumFractionDigits: digits })
}

export interface DiagnosticSeriesSpec {
  metric: string
  hop?: number
  stat?: string
  role?: string
  label: string
}

export const diagnosticPreviewInterval = 15_000

// diagnosticSparklineSeries 是详情页固定需要的序列；数量固定，避免每次渲染重新拉取。
export function detailSeriesSpecs(job: DiagnosticJob): DiagnosticSeriesSpec[] {
  if (job.kind === "mtr") {
    return [
      { metric: "diagnostic_mtr_latency_ms", stat: "avg", label: "latency" },
      { metric: "diagnostic_mtr_loss_percent", stat: "last", label: "loss" },
    ]
  }
  const specs: DiagnosticSeriesSpec[] = [
    { metric: "diagnostic_iperf_bytes_per_second", role: "sender", label: "sender" },
    { metric: "diagnostic_iperf_bytes_per_second", role: "receiver", label: "receiver" },
    { metric: "diagnostic_iperf_bytes", role: "sender", label: "sender-bytes" },
    { metric: "diagnostic_iperf_bytes", role: "receiver", label: "receiver-bytes" },
  ]
  if (job.protocol === "udp") {
    specs.push({ metric: "diagnostic_iperf_jitter_ms", role: "receiver", label: "jitter" })
    specs.push({ metric: "diagnostic_iperf_loss_percent", role: "receiver", label: "loss" })
  }
  return specs
}
