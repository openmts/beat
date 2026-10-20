// 面板 → 告警规则的预填种子。Grafana 允许从面板直接创建告警规则，这里把面板首条序列的
// 指标、聚合与已选节点翻译成规则草稿，并编码进规则页 URL；规则页再把它解码回草稿。
// 规则本身仍由既有 /alerts/rules 校验与保存，本模块不引入新的持久化契约。

import { expandPanelSeries } from "@/lib/workspace-query"
import type { DashboardDefinition, DashboardPanel, WorkspaceSelections } from "@/types/metric-workspace"
import type { Group, Node } from "@/types"

export interface WorkspaceAlertSeed {
  metric: string
  aggregation: string
  scope: { kind: "all" } | { kind: "nodes"; node_ids: string[]; device_id?: string }
  /** 规则按窗口聚合；沿用看板时间范围，让 avg/max 之类的面板聚合仍然成立。 */
  window_seconds: number
  threshold?: number
  name?: string
}

const maximumName = 128
const maximumWindowSeconds = 86400
const minimumWindowSeconds = 10
const metricPattern = /^[a-z][a-z0-9_]{0,63}$/
const aggregationPattern = /^[a-z][a-z0-9_]{0,15}$/
const nodePattern = /^[A-Za-z0-9._-]{1,64}$/
const devicePattern = /^[a-f0-9]{32}$/

/** 面板首条序列 → 规则草稿。组变量展开成多个节点时退回全量范围，避免替用户任选一台。 */
export function workspaceAlertSeed(panel: DashboardPanel, context: {
  definition: DashboardDefinition; selections: WorkspaceSelections; nodes: Node[]; groups?: Group[]
}): WorkspaceAlertSeed | null {
  const series = panel.series[0]
  if (!series) return null
  let expanded
  try { expanded = expandPanelSeries(panel, context) } catch { return null }
  const only = expanded.length === 1 ? expanded[0] : null
  const thresholds = panel.thresholds.map((threshold) => threshold.value)
  const range = Math.floor(context.definition.time_range_seconds)
  const window = Number.isFinite(range) ? Math.min(maximumWindowSeconds, Math.max(minimumWindowSeconds, range)) : minimumWindowSeconds
  return { metric: series.metric, aggregation: series.aggregation, window_seconds: window,
    scope: only ? { kind: "nodes", node_ids: [only.node_id], ...(only.device_id ? { device_id: only.device_id } : {}) } : { kind: "all" },
    threshold: thresholds.length ? Math.min(...thresholds) : undefined,
    name: panel.title.slice(0, maximumName) }
}

/** 生成规则页深链；标签与阈值只在存在时携带。 */
export function workspaceAlertLink(seed: WorkspaceAlertSeed): string {
  const parameters = new URLSearchParams({ tab: "rules", metric: seed.metric, aggregation: seed.aggregation, scope: seed.scope.kind, window: String(seed.window_seconds) })
  if (seed.scope.kind === "nodes") {
    parameters.set("node_id", seed.scope.node_ids[0])
    if (seed.scope.device_id) parameters.set("device_id", seed.scope.device_id)
  }
  if (seed.threshold !== undefined && Number.isFinite(seed.threshold)) parameters.set("threshold", String(seed.threshold))
  if (seed.name) parameters.set("name", seed.name.slice(0, maximumName))
  return `/admin/alerts?${parameters.toString()}`
}

/** 解析深链；任何越界或不合法的字段都被丢弃，最坏退回默认规则草稿。 */
export function workspaceAlertSeedFromQuery(query: URLSearchParams): WorkspaceAlertSeed | null {
  const metric = query.get("metric")?.trim() ?? ""
  if (!metricPattern.test(metric)) return null
  const aggregation = query.get("aggregation")?.trim() ?? ""
  const node = query.get("node_id")?.trim() ?? ""
  const device = query.get("device_id")?.trim() ?? ""
  const rawThreshold = query.get("threshold")
  const threshold = rawThreshold === null || rawThreshold.trim() === "" ? Number.NaN : Number(rawThreshold)
  const name = query.get("name")?.trim() ?? ""
  const window = Number(query.get("window"))
  const window_seconds = Number.isInteger(window) && window >= minimumWindowSeconds && window <= maximumWindowSeconds ? window : 0
  return { metric, aggregation: aggregationPattern.test(aggregation) ? aggregation : "last", window_seconds,
    scope: query.get("scope") === "nodes" && nodePattern.test(node)
      ? { kind: "nodes", node_ids: [node], ...(devicePattern.test(device) ? { device_id: device } : {}) }
      : { kind: "all" },
    threshold: Number.isFinite(threshold) ? threshold : undefined,
    name: name ? name.slice(0, maximumName) : undefined }
}
