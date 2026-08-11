export type MetricValueFormat = "percent" | "bytes-per-second" | "number"

const byteUnits = ["B", "KiB", "MiB", "GiB", "TiB"] as const

function formatDecimal(value: number): string {
  return value.toFixed(1).replace(/\.0$/, "")
}

export function formatMetricValue(value: number, format: MetricValueFormat): string {
  if (format === "percent") return `${formatDecimal(value)}%`
	  if (format === "number") return formatDecimal(value)
  const absoluteValue = Math.abs(value)
  if (absoluteValue >= 1024 * 1024) {
    return `${formatDecimal(value / (1024 * 1024))} MiB/s`
  }
  if (absoluteValue >= 1024) return `${formatDecimal(value / 1024)} KiB/s`
  return `${formatDecimal(value)} B/s`
}

export function formatDuration(totalSeconds: number): string {
	  if (!Number.isFinite(totalSeconds) || totalSeconds < 0) return "--"
	  const seconds = Math.floor(totalSeconds)
	  const days = Math.floor(seconds / 86400)
	  const hours = Math.floor((seconds % 86400) / 3600)
	  const minutes = Math.floor((seconds % 3600) / 60)
	  if (days > 0) return `${days}d ${hours}h`
	  if (hours > 0) return `${hours}h ${minutes}m`
	  return `${minutes}m`
}

export function formatBytes(value: number): string {
  const absoluteValue = Math.abs(value)
  let unitIndex = 0
  let scaledValue = value

  while (absoluteValue >= 1024 ** (unitIndex + 1) && unitIndex < byteUnits.length - 1) {
    unitIndex++
    scaledValue = value / 1024 ** unitIndex
  }

  return `${formatDecimal(scaledValue)} ${byteUnits[unitIndex]}`
}
