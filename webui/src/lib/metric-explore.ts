// 指标探索（Grafana Explore 等价物）：不落库的临时查询状态，可编码进 URL 分享。
import { workspaceDomain } from "@/lib/workspace-query"
import type { Node } from "@/types"
import type { DashboardPanel, ExpandedSeries, MetricAggregation, MetricCatalog, MetricDefinition, MetricLatestQuery, MetricQuery, PanelColor, WorkspaceRange } from "@/types/metric-workspace"

export const maximumExploreQueries = 4
export const exploreRefIds = ["A", "B", "C", "D"] as const
export const exploreColors: readonly PanelColor[] = ["blue", "green", "amber", "rose"]
export const exploreAggregations: readonly MetricAggregation[] = ["avg", "min", "max", "sum", "last", "rate"]
export const explorePointOptions = [60, 300, 600] as const
export const exploreDefaultPoints = 300
// 与服务端 internal/metricquery.ValidateRequest 的点数预算一致。
const minimumPoints = 2
const maximumPoints = 600
const maximumSearchLength = 4096
const relativeRanges = [300, 900, 3600, 21600, 86400, 604800, 2592000]

export const emptyMetricCatalog: MetricCatalog = { version: 0, revision: 0, metrics: [] }

export interface ExploreQuery {
  ref_id: string
  metric: string
  aggregation: MetricAggregation
  node_id: string
  device_id: string
  color: PanelColor
  hidden: boolean
}

/** Grafana Explore 的查询类型：range 走区间分桶，instant 取每条序列的最新原始样本。 */
export type ExploreQueryType = "range" | "instant"

export interface ExploreState {
  queries: ExploreQuery[]
  range: WorkspaceRange
  max_points: number
  query_type: ExploreQueryType
}

/** 只有启用中的指标可以被探索，禁用指标仍留在目录里供管理端查看。 */
export function exploreMetrics(catalog: MetricCatalog): MetricDefinition[] {
  return catalog.metrics.filter((metric) => metric.enabled)
}

export function exploreMetric(catalog: MetricCatalog, key: string): MetricDefinition | undefined {
  return exploreMetrics(catalog).find((metric) => metric.key === key)
}

export function exploreNode(nodes: Node[], id: string): Node | undefined {
  return nodes.find((node) => node.id === id)
}

/** 新查询行：优先第一个启用指标与第一个节点，缺任何一项时返回空选择让页面提示。 */
export function exploreQueryFor(catalog: MetricCatalog, nodes: Node[], index: number): ExploreQuery {
  const metric = exploreMetrics(catalog)[0]
  return {
    ref_id: exploreRefIds[Math.min(index, exploreRefIds.length - 1)],
    metric: metric?.key ?? "",
    aggregation: metric?.default_aggregation ?? "avg",
    node_id: nodes[0]?.id ?? "",
    device_id: "",
    color: exploreColors[index % exploreColors.length],
    hidden: false,
  }
}

export function exploreDefaultState(catalog: MetricCatalog, nodes: Node[]): ExploreState {
  return { queries: [exploreQueryFor(catalog, nodes, 0)], range: { mode: "relative", seconds: 3600 }, max_points: exploreDefaultPoints, query_type: "range" }
}

/** 新增一行时保持既有行的引用标识与颜色，超出四行上限直接返回原状态。 */
export function exploreAddQuery(state: ExploreState, catalog: MetricCatalog, nodes: Node[]): ExploreState {
  if (state.queries.length >= maximumExploreQueries) return state
  return { ...state, queries: [...state.queries, exploreQueryFor(catalog, nodes, state.queries.length)] }
}

export function exploreRemoveQuery(state: ExploreState, index: number): ExploreState {
  return { ...state, queries: state.queries.filter((_, current) => current !== index) }
}

/** 查询行校验：返回错误键而不是抛出，页面据此逐行提示并禁用执行。 */
export function exploreQueryErrorKey(query: ExploreQuery, catalog: MetricCatalog, nodes: Node[]): string | null {
  if (!exploreMetric(catalog, query.metric)) return "explore.invalid_metric"
  if (!exploreNode(nodes, query.node_id)) return "explore.invalid_node"
  if (!exploreAggregations.includes(query.aggregation)) return "explore.invalid_aggregation"
  return null
}

export function exploreRunnable(state: ExploreState): ExploreQuery[] {
  return state.queries.filter((query) => !query.hidden && query.metric !== "" && query.node_id !== "")
}

/** 查询载荷：范围沿用看板的相对/绝对语义，点数被夹到服务端预算内。 */
export function exploreRequest(state: ExploreState, now = Date.now()): MetricQuery {
  const rows = exploreRunnable(state)
  if (rows.length === 0) throw new Error("explore.no_queries")
  const points = Number.isInteger(state.max_points) ? Math.min(maximumPoints, Math.max(minimumPoints, state.max_points)) : exploreDefaultPoints
  return { ...workspaceDomain(state.range, now), max_points: points, series: rows.map((row) => ({ ref_id: row.ref_id, node_id: row.node_id, metric: row.metric, aggregation: row.aggregation, ...(row.device_id ? { device_id: row.device_id } : {}) })) }
}

/** 即时查询只覆盖节点级指标：带设备维度的行无法寻址，页面据此禁用执行并提示。 */
export function exploreInstantSupported(state: ExploreState): boolean {
  return exploreRunnable(state).every((row) => row.device_id === "")
}

/**
 * 即时查询载荷：不带时间范围与点数，按「节点 + 指标」去重后逐条取最新原始样本。
 * 与服务端 internal/metricquery.ValidateLatestRequest 一样要求非空且引用唯一。
 */
export function exploreLatestRequest(state: ExploreState): MetricLatestQuery {
  const rows = exploreRunnable(state)
  if (rows.length === 0) throw new Error("explore.no_queries")
  const seen = new Set<string>()
  const series = rows.flatMap((row) => {
    const key = `${row.node_id}\u0000${row.metric}`
    if (seen.has(key)) return []
    seen.add(key)
    return [{ ref_id: row.ref_id, node_id: row.node_id, metric: row.metric }]
  })
  return { series }
}

/** 探索只渲染时间序列：图例与悬停保持与看板面板同一套行为。 */
export function explorePanel(t: (key: string) => string): DashboardPanel {
  return { id: "explore", title: t("explore.title"), type: "timeseries", size: "large", legend: true, tooltip: "all", unit: "auto", thresholds: [], series: [] }
}

export function exploreExpanded(state: ExploreState, nodes: Node[], catalog: MetricCatalog): ExpandedSeries[] {
  return exploreRunnable(state).map((row) => {
    const node = exploreNode(nodes, row.node_id)
    const metric = exploreMetric(catalog, row.metric)
    const name = node?.alias || node?.name || row.node_id
    return { ref_id: row.ref_id, node_id: row.node_id, metric: row.metric, aggregation: row.aggregation,
      color: row.color, label: `${metric?.label ?? row.metric} · ${name}`, node_name: name,
      ...(row.device_id ? { device_id: row.device_id } : {}) }
  })
}

function encodeQuery(query: ExploreQuery): string {
  return [query.metric, query.aggregation, query.node_id, query.device_id, query.color, query.hidden ? "1" : "0"].map((value) => encodeURIComponent(value)).join("|")
}

export function exploreSearch(state: ExploreState): string {
  const params = new URLSearchParams()
  for (const query of state.queries) params.append("q", encodeQuery(query))
  if (state.range.mode === "relative") params.set("range", String(state.range.seconds))
  else { params.set("from", state.range.from); params.set("to", state.range.to) }
  if (state.max_points !== exploreDefaultPoints) params.set("points", String(state.max_points))
  if (state.query_type === "instant") params.set("type", "instant")
  return params.toString()
}

function decodeQuery(value: string, index: number, catalog: MetricCatalog, nodes: Node[]): ExploreQuery | null {
  const [metric, aggregation, node_id, device_id = "", color, hidden] = value.split("|").map((part) => { try { return decodeURIComponent(part) } catch { return "" } })
  const definition = exploreMetric(catalog, metric)
  if (!definition || !exploreNode(nodes, node_id)) return null
  return { ref_id: exploreRefIds[Math.min(index, exploreRefIds.length - 1)], metric, node_id, device_id,
    aggregation: exploreAggregations.includes(aggregation as MetricAggregation) ? aggregation as MetricAggregation : definition.default_aggregation,
    color: exploreColors.includes(color as PanelColor) ? color as PanelColor : exploreColors[index % exploreColors.length], hidden: hidden === "1" }
}

function decodeRange(params: URLSearchParams): WorkspaceRange | null {
  const seconds = Number(params.get("range"))
  if (relativeRanges.includes(seconds)) return { mode: "relative", seconds }
  const from = params.get("from") ?? "", to = params.get("to") ?? ""
  if (![from, to].every((value) => Number.isFinite(Date.parse(value)))) return null
  return { mode: "absolute", from, to }
}

/** URL 解析绝不抛错：非法行、非法指标或未知节点都被丢弃，完全不可用时回落到默认状态。 */
export function parseExploreState(search: string, catalog: MetricCatalog, nodes: Node[]): ExploreState {
  const params = new URLSearchParams(search.length <= maximumSearchLength ? search : "")
  const queries = params.getAll("q").map((value, index) => decodeQuery(value, index, catalog, nodes))
    .filter((value): value is ExploreQuery => value !== null).slice(0, maximumExploreQueries)
  const points = Number(params.get("points"))
  const defaults = exploreDefaultState(catalog, nodes)
  return {
    queries: queries.length > 0 ? queries : defaults.queries,
    range: decodeRange(params) ?? defaults.range,
    max_points: (explorePointOptions as readonly number[]).includes(points) ? points : exploreDefaultPoints,
    query_type: params.get("type") === "instant" ? "instant" : "range",
  }
}
