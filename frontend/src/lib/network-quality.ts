import type {
  NetworkNode,
  NetworkNodeState,
  NetworkProbeType,
  NetworkTaskView,
} from "@/types"

export const networkHistoryRanges = [
  { label: "1h", hours: 1 },
  { label: "6h", hours: 6 },
  { label: "24h", hours: 24 },
  { label: "7d", hours: 168 },
] as const

export function formatLatency(value: number): string {
  if (!Number.isFinite(value) || value < 0) return "--"
  if (value < 1000) return `${Math.round(value)} ms`
  const seconds = value / 1000
  return `${seconds < 10 ? seconds.toFixed(1) : Math.round(seconds)} s`
}

export function formatNetworkInterval(seconds: number): string {
  if (seconds < 60) return `${seconds}s`
  if (seconds < 3600) return `${Math.round(seconds / 60)}m`
  return `${Math.round(seconds / 3600)}h`
}

export function networkNodeLabel(node: NetworkNode): string {
  const alias = node.alias.trim()
  return alias ? `${alias} (${node.name})` : node.name
}

export function networkTypeKey(type: NetworkProbeType): string {
  return `network.type.${type}`
}

export function latestSuccessPercent(view: NetworkTaskView): number | null {
  const reported = view.nodes.filter((state) => state.latest !== null)
  if (reported.length === 0) return null
  const successful = reported.filter((state) => state.latest?.success).length
  return successful / reported.length * 100
}

export function latencyRailPercent(state: NetworkNodeState): number {
  const latency = state.latest?.latency_ms
  if (latency === undefined) return 0
  return Math.min(100, Math.max(4, latency / 10))
}
