import type { Node } from "@/types"
import { formatBytes, formatMetricValue } from "@/lib/metric-format"
import { formatDateTime, formatInteger } from "@/lib/intl-format"

export function formatSystem(node: Node): string {
  const name = [node.platform || node.os, node.os_version].filter(Boolean).join(" ")
  return [name, node.arch].filter(Boolean).join(" / ") || "--"
}

/** 最近在线时间按应用语言渲染：切换语言后无需刷新即可看到本地化格式。 */
export function formatLastSeen(value: string, locale?: string): string {
  if (!value) return "--"
  return formatDateTime(locale, value)
}

export function valueOr(value: number | undefined, formatter: (value: number) => string): string {
  return value === undefined ? "--" : formatter(value)
}

export function formatLoad(metrics: Record<string, number>, locale?: string): string {
  if (metrics.load1 === undefined) return "--"
  return [metrics.load1, metrics.load5, metrics.load15].map((value) => formatMetricValue(value, "number", locale)).join(" / ")
}

export function formatCount(value: number | undefined, locale?: string): string {
  return value === undefined ? "--" : formatInteger(locale, value)
}

export function formatConnections(metrics: Record<string, number>, locale?: string): string {
  if (metrics.tcp_connections === undefined || metrics.udp_connections === undefined) return "--"
  return `${formatCount(metrics.tcp_connections, locale)} TCP / ${formatCount(metrics.udp_connections, locale)} UDP`
}

export function formatTraffic(metrics: Record<string, number>, locale?: string): string {
  if (metrics.net_sent_total === undefined || metrics.net_recv_total === undefined) return "--"
  return `UP ${formatBytes(metrics.net_sent_total, locale)} / DOWN ${formatBytes(metrics.net_recv_total, locale)}`
}

export function formatNetwork(metrics: Record<string, number>, locale?: string): string {
  if (metrics.net_sent === undefined || metrics.net_recv === undefined) return "--"
  return `UP ${formatMetricValue(metrics.net_sent, "bytes-per-second", locale)} / DOWN ${formatMetricValue(metrics.net_recv, "bytes-per-second", locale)}`
}

/** 容量读数：带单位时按个数展示，否则按字节缩放；两个数字都跟随应用语言。 */
export function formatCapacity(used: number | undefined, total: number | undefined, options: { unit?: string; locale?: string } = {}): string {
  if (used === undefined || total === undefined || total <= 0) return "--"
  if (options.unit) return `${formatDecimalValue(used, options.locale)} / ${formatDecimalValue(total, options.locale)} ${options.unit}`
  return `${formatBytes(used, options.locale)} / ${formatBytes(total, options.locale)}`
}

function formatDecimalValue(value: number, locale?: string): string {
  return formatMetricValue(value, "number", locale)
}
