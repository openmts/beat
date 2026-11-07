import type {
  NetworkDirection,
  NetworkNode,
  NetworkNodeState,
  NetworkProbeType,
  NetworkTaskView,
  NodeNetworkSummary,
} from "@/types"

export const networkHistoryRanges = [
  { label: "1h", hours: 1 },
  { label: "6h", hours: 6 },
  { label: "24h", hours: 24 },
  { label: "7d", hours: 168 },
] as const

export function formatLatency(value: number | null | undefined): string {
  if (value === null || value === undefined || !Number.isFinite(value) || value < 0) return "--"
  if (value < 1000) return `${Math.round(value)} ms`
  const seconds = value / 1000
  return `${seconds < 10 ? seconds.toFixed(1) : Math.round(seconds)} s`
}

export function formatPacketLoss(value: number | null | undefined): string {
  if (value === null || value === undefined || !Number.isFinite(value) || value < 0) return "--"
  return `${value.toFixed(1)}%`
}

// networkSummaryByNode 为首页节点卡片选择一条最有参考价值的公开任务。
// 优先 ICMP（有丢包率），同类型下优先 Server→Agent，因为它是服务端视角的端到端采样。
export function networkSummaryByNode(
  views: NetworkTaskView[] | null,
): Record<string, NodeNetworkSummary> {
  const selected = new Map<string, { summary: NodeNetworkSummary; score: number }>()
  for (const view of views ?? []) {
    const direction = view.task.direction ?? "agent_to_target"
    for (const state of view.nodes) {
      const latest = state.latest
      if (latest !== null && !Number.isFinite(latest.latency_ms)) continue
      const summary: NodeNetworkSummary = {
        task_id: view.task.id,
        task_name: view.task.name,
        direction,
        type: view.task.type,
        latency_ms: latest?.latency_ms ?? null,
        loss_percent: view.task.type === "icmp" ? latest?.loss_percent ?? null : null,
        success: latest?.success ?? false,
        timestamp: latest?.timestamp ?? "",
      }
      const score = summaryScore(summary, view.task.sort_order)
      const current = selected.get(state.node.id)
      if (current === undefined || score < current.score) {
        selected.set(state.node.id, { summary, score })
      }
    }
  }
  return Object.fromEntries([...selected].map(([nodeID, value]) => [nodeID, value.summary]))
}

function summaryScore(summary: NodeNetworkSummary, sortOrder: number): number {
  const sampleRank = summary.latency_ms === null ? 1_000_000_000 : 0
  const directionRank = summary.direction === "server_to_agent" ? 0 : 1
  const typeRank = summary.type === "icmp" ? 0 : summary.type === "tcp" ? 1 : 2
  return sampleRank + typeRank * 1_000_000 + directionRank * 100_000 + Math.max(0, sortOrder)
}

export function networkDirectionKey(direction: NetworkDirection): string {
  return `network.direction.${direction}`
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
