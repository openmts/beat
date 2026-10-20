import type { Node, TrafficSummary } from "@/types"
import type { ThemeJSON, ThemeNode } from "@/types/theme-runtime"
import { assertCount, finiteNumber, publicID, publicText } from "@/lib/theme-json"
import { themeTelemetry } from "@/lib/theme-telemetry"

export const THEME_METRICS = [
  "cpu", "cpu_used", "cpu_total", "memory", "memory_used", "memory_total", "disk", "disk_used", "disk_total",
  "disk_read", "disk_write", "net_recv", "net_sent", "net_recv_total", "net_sent_total", "swap", "swap_used", "swap_total",
  "load1", "load5", "load15", "uptime", "processes", "tcp_connections", "udp_connections",
] as const
const SYSTEM_FIELDS = ["cpu_model", "os", "platform", "os_version", "kernel", "arch", "virtualization", "agent_version"] as const
const TRAFFIC_NUMBERS = ["sent", "received", "used", "limit", "remaining", "percentage", "reset_day"] as const
const TRAFFIC_TEXT = ["limit_type", "period_start", "next_reset", "tracked_since", "status"] as const

export function themeNode(node: Node, showIP: boolean): ThemeNode {
  const tags = node.tags ?? []
  assertCount(tags, 128)
  const result: ThemeNode = {
    id: publicID(node.id), name: publicText(node.name), alias: publicText(node.alias), group_id: publicText(node.group_id),
    status: node.status === "online" ? "online" : "offline", tags: tags.map(publicText), public_remark: publicText(node.public_remark),
    last_seen: publicText(node.last_seen), system: {}, metrics: {}, ...themeTelemetry(node),
  }
  if (showIP) result.host = publicText(node.host)
  for (const key of SYSTEM_FIELDS) result.system[key] = publicText(node[key])
  for (const key of THEME_METRICS) {
    const value = node.metrics?.[key]
    if (finiteNumber(value)) result.metrics[key] = value
  }
  if (node.traffic) result.traffic = themeTraffic(node.traffic)
  return result
}

function themeTraffic(source: TrafficSummary): Record<string, ThemeJSON> {
  const traffic: Record<string, ThemeJSON> = {}
  for (const key of TRAFFIC_NUMBERS) {
    const value = source[key]
    if (value === null || finiteNumber(value)) traffic[key] = value
  }
  for (const key of TRAFFIC_TEXT) {
    const value = source[key]
    if (value === null || typeof value === "string") traffic[key] = value === null ? null : publicText(value)
  }
  return traffic
}
