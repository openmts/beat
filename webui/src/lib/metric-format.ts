import { formatBytes as formatIntlBytes, formatDecimal, formatInteger, formatPercent } from "@/lib/intl-format"

export type MetricValueFormat = "percent" | "bytes-per-second" | "number"

/** 数值展示统一走 Intl：语言来自应用设置，缺省时退回运行环境语言（测试与无 Provider 场景）。 */
export function formatMetricValue(value: number, format: MetricValueFormat, locale?: string): string {
  if (format === "percent") return formatPercent(locale, value, 1)
  if (format === "number") return formatDecimal(locale, value, 1)
  return formatIntlBytes(locale, value, { decimals: 1, maximumUnit: "MiB", perSecond: true })
}

/** 时长用两位以内的整数单位拼接；数字本身按语言分组。 */
export function formatDuration(totalSeconds: number, locale?: string): string {
  if (!Number.isFinite(totalSeconds) || totalSeconds < 0) return "--"
  const seconds = Math.floor(totalSeconds)
  const days = Math.floor(seconds / 86400)
  const hours = Math.floor((seconds % 86400) / 3600)
  const minutes = Math.floor((seconds % 3600) / 60)
  if (days > 0) return `${formatInteger(locale, days)}d ${formatInteger(locale, hours)}h`
  if (hours > 0) return `${formatInteger(locale, hours)}h ${formatInteger(locale, minutes)}m`
  return `${formatInteger(locale, minutes)}m`
}

/** 容量展示：与既有展示一致，最多到 TiB，保留一位小数且不补零。 */
export function formatBytes(value: number, locale?: string): string {
  return formatIntlBytes(locale, value, { decimals: 1 })
}
