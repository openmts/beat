import type { DashboardDefinition, DashboardPanel, MetricCatalog, MetricQueryResult, MetricResultSeries, PanelQueryState } from "@/types/metric-workspace"
import type { ThemeDataState, ThemeHistory } from "@/types/theme-runtime"

interface HistorySource {
  nodeId: string
  definition: DashboardDefinition
  catalog: MetricCatalog
  panels: Record<string, PanelQueryState>
}
interface HistoryCandidate { from: number; to: number; updatedAt: number; series: MetricResultSeries[] }

export function buildPublicWorkspaceHistory(source: HistorySource): { state: ThemeDataState; history?: ThemeHistory; metricNames: string[] } {
  const states = source.definition.panels.map((panel) => source.panels[panel.id])
  const state = states.some((panel) => panel?.error || panel?.result && !historyDomain(panel.result)) ? "error" :
    states.some((panel) => !panel || panel.loading || !panel.result) ? "loading" : "ready"
  const candidates = source.definition.panels.flatMap((panel) => historyCandidate(source, panel))
  const latest = candidates.reduce<HistoryCandidate | undefined>((current, value) => !current || value.updatedAt > current.updatedAt ? value : current, undefined)
  if (!latest) return { state, history: undefined, metricNames: [] }
  const history: ThemeHistory = { from: latest.from, to: latest.to, series: {}, series_info: {} }
  for (const candidate of candidates) {
    if (candidate.from !== latest.from || candidate.to !== latest.to) continue
    for (const series of candidate.series) {
      if (Object.hasOwn(history.series, series.metric)) continue
      history.series[series.metric] = series.points.map((point) => ({ timestamp: point.timestamp / 1000, value: point.value }))
      history.series_info![series.metric] = { aggregation: series.aggregation, unit: series.unit }
    }
  }
  return { state, history, metricNames: Object.keys(history.series) }
}

function historyCandidate(source: HistorySource, panel: DashboardPanel): HistoryCandidate[] {
  const state = source.panels[panel.id]
  if (!state?.result || !state.resultQuery) return []
  const domain = historyDomain(state.result)
  if (!domain) return []
  const metrics = new Map(source.catalog.metrics.filter((metric) => metric.enabled && metric.public).map((metric) => [metric.key, metric]))
  const series = state.resultQuery.series.flatMap((query) => {
    const definition = metrics.get(query.metric)
    // 旧主题历史协议没有设备维度，不能把某块 GPU 的值冒充整机指标。
    if (query.device_id || query.node_id !== source.nodeId || !definition?.aggregations.includes(query.aggregation) ||
      !panel.series.some((series) => series.metric === query.metric && series.aggregation === query.aggregation)) return []
    const result = state.result!.series.find((series) => series.ref_id === query.ref_id && series.node_id === query.node_id && series.metric === query.metric && series.aggregation === query.aggregation)
    return result ? [result] : []
  })
  return series.length ? [{ ...domain, updatedAt: state.updatedAt ?? 0, series }] : []
}

function historyDomain(result: MetricQueryResult) {
  const from = Date.parse(result.from) / 1000, to = Date.parse(result.to) / 1000
  return Number.isFinite(from) && Number.isFinite(to) && from >= 0 && from < to ? { from, to } : null
}
