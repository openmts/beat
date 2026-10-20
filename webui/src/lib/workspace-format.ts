import { dateTimeFormatter, numberFormatter } from "@/lib/intl-format"
import { defaultNoValue } from "@/lib/workspace-override-contract"

interface UnitScale { divisor: number; suffix: string }

const byteUnits = ["B", "KiB", "MiB", "GiB", "TiB", "PiB"]
const simpleUnits: Record<string, string> = { percent: "%", celsius: "°C", watts: "W", cores: "cores", count: "", number: "", count_per_second: "/s", number_per_second: "/s" }

export function workspaceScale(unit: string, maximum: number): UnitScale {
  const absolute = Math.abs(Number.isFinite(maximum) ? maximum : 0)
  if (unit === "bytes" || unit === "bytes_per_second") {
    const index = absolute > 0 ? Math.min(byteUnits.length - 1, Math.max(0, Math.floor(Math.log(absolute) / Math.log(1024)))) : 0
    return { divisor: 1024 ** index, suffix: byteUnits[index] + (unit.endsWith("_per_second") ? "/s" : "") }
  }
  if (unit === "seconds") {
    if (absolute > 0 && absolute < 1) return { divisor: 0.001, suffix: "ms" }
    for (const [divisor, suffix] of [[86400, "d"], [3600, "h"], [60, "min"]] as const) {
      if (absolute >= divisor) return { divisor, suffix }
    }
    return { divisor: 1, suffix: "s" }
  }
  return { divisor: 1, suffix: simpleUnits[unit] ?? unit }
}

export function formatWorkspaceValue(value: number | null | undefined, unit: string, options: {
  locale?: string; decimals?: number; scale?: UnitScale; noValue?: string
} = {}): string {
  // 面板级 No value 覆盖默认破折号；没有声明时沿用渲染器口径。
  if (value == null || !Number.isFinite(value)) return options.noValue ?? defaultNoValue
  const scale = options.scale ?? workspaceScale(unit, value)
  const formatted = numberFormatter(options.locale, { maximumFractionDigits: options.decimals ?? 2 }).format(value / scale.divisor)
  return scale.suffix ? `${formatted}${scale.suffix === "%" ? "" : " "}${scale.suffix}` : formatted
}

export function workspaceAxisTime(timestamp: number, duration: number, locale?: string): string {
  const options: Intl.DateTimeFormatOptions = { hour: "2-digit", minute: "2-digit", hour12: false, timeZone: "UTC" }
  if (duration >= 86400000) { options.month = "short"; options.day = "numeric" }
  return dateTimeFormatter(locale, options).format(timestamp)
}
