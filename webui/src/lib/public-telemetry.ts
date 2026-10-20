// 匿名聚合遥测上报器：固定枚举、会话内采样上限、永不重试、永不排队。
import { reportPublicInteraction } from "@/lib/public-telemetry-transport"

export const publicTelemetryEvents = ["page_view", "node_open", "range_change"] as const
export type PublicTelemetryEvent = (typeof publicTelemetryEvents)[number]

export const publicTelemetrySurfaces = ["dashboard", "node", "settings"] as const
export type PublicTelemetrySurface = (typeof publicTelemetrySurfaces)[number]

export interface PublicTelemetryConfig {
  enabled: boolean
  allowedEvents: string[]
  sampleCeiling: number
}

const sessionKey = "beat.public-telemetry.sent"
const maximumSampleCeiling = 100000

export const disabledPublicTelemetry: PublicTelemetryConfig = { enabled: false, allowedEvents: [], sampleCeiling: 0 }

// publicSurfaceForPath 只把路径粗粒度映射成页面种类，不保留路径、查询或标识。
export function publicSurfaceForPath(pathname: string): PublicTelemetrySurface | null {
  if (pathname === "/" || pathname.startsWith("/metrics")) return "dashboard"
  if (pathname.startsWith("/node/")) return "node"
  return null
}

export function publicTelemetryConfig(settings: {
  public_telemetry?: { enabled: boolean; allowed_events: string[]; sample_ceiling: number }
} | undefined): PublicTelemetryConfig {
  const telemetry = settings?.public_telemetry
  if (!telemetry) return disabledPublicTelemetry
  return { enabled: Boolean(telemetry.enabled), allowedEvents: telemetry.allowed_events ?? [], sampleCeiling: telemetry.sample_ceiling }
}

export interface PublicTelemetryReporter {
  configure: (config: PublicTelemetryConfig) => void
  report: (event: PublicTelemetryEvent, surface: PublicTelemetrySurface) => void
}

// createPublicTelemetryReporter 在会话内累计已发送数量；失败从不影响页面。
export function createPublicTelemetryReporter(): PublicTelemetryReporter {
  let config = disabledPublicTelemetry
  let sent = readSessionCount()
  return {
    configure: (next) => {
      config = next
    },
    report: (event, surface) => {
      if (!config.enabled || !publicTelemetryEvents.includes(event) || !publicTelemetrySurfaces.includes(surface)) return
      if (!config.allowedEvents.includes(event)) return
      if (sent >= ceilingOf(config.sampleCeiling)) return
      sent += 1
      writeSessionCount(sent)
      void reportPublicInteraction(event, surface)
    },
  }
}

function ceilingOf(value: number): number {
  if (!Number.isFinite(value) || value < 1) return 1
  return Math.min(Math.trunc(value), maximumSampleCeiling)
}

function readSessionCount(): number {
  try {
    const raw = window.sessionStorage.getItem(sessionKey)
    const value = Number.parseInt(raw ?? "", 10)
    return Number.isFinite(value) && value > 0 ? value : 0
  } catch {
    return 0
  }
}

function writeSessionCount(value: number) {
  try {
    window.sessionStorage.setItem(sessionKey, String(value))
  } catch {
    // 会话存储不可用时只影响采样计数，绝不影响页面。
  }
}
