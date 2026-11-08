import { MAX_THEME_SNAPSHOT_BYTES, THEME_RELATIVE_RANGE_SECONDS, type ThemeHistoryPoint, type ThemeHistorySeriesInfo, type ThemeRange, type ThemeSnapshot, type ThemeSnapshotInput } from "@/types/theme-runtime"
import { assertCount, boundedJSON, finiteNumber, publicConfiguration, publicID, publicText, ThemeDataError } from "@/lib/theme-json"
import { THEME_METRICS, themeNode } from "@/lib/theme-node-snapshot"
import { themeNetwork, themeNetworkHistory } from "@/lib/theme-network-snapshot"
import type { WorkspaceRange } from "@/types/metric-workspace"

const MAX_RANGE_SECONDS = 31 * 24 * 60 * 60

export function buildThemeSnapshot(source: ThemeSnapshotInput): ThemeSnapshot {
  // 单页主题需要在任意路由同时展示公开节点列表；route.node_id 只决定当前详情与历史数据。
  const publicNodes = source.nodes.filter((node) => node.is_public === true)
  assertCount(publicNodes, 1000)
  const nodes = publicNodes.map((node) => themeNode(node, source.site.public_address_mode !== "hidden"))
  const visible = new Set(nodes.map((node) => node.id))
  if (visible.size !== nodes.length) throw new ThemeDataError("invalid_data")
  const selected = source.route.kind === "node" && source.route.node_id && visible.has(source.route.node_id) ? source.route.node_id : undefined
  const groupIDs = new Set(nodes.map((node) => node.group_id))
  const groups = source.groups.filter((group) => groupIDs.has(group.id))
  assertCount(groups, 200)
  const snapshot: ThemeSnapshot = {
    timestamp: new Date().toISOString(), route: { kind: source.route.kind, ...(selected ? { node_id: selected } : {}) },
    site: publicSite(source), locale: publicText(source.locale), theme: source.theme, configuration: publicConfiguration(source.configuration),
    nodes, groups: groups.map((group) => ({ id: publicID(group.id), name: publicText(group.name), sort_order: finiteNumber(group.sort_order) ? group.sort_order : 0 })),
    network: { visible: source.site.show_network_quality === true, tasks: [] },
    state: { nodes: source.state?.nodes ?? "ready", metrics: source.state?.metrics ?? "ready", network: source.state?.network ?? "ready" },
  }
  if (selected) snapshot.range_options = [...THEME_RELATIVE_RANGE_SECONDS]
  if (selected && source.range) snapshot.range = publicRange(source.range)
  if (snapshot.network.visible) snapshot.network.tasks = themeNetwork(source.network ?? [], visible)
  if (selected && snapshot.network.visible) {
    const taskIDs = new Set(snapshot.network.tasks.flatMap((task) => typeof task === "object" && task !== null && !Array.isArray(task) && typeof task.id === "string" ? [task.id] : []))
    const networkHistory = themeNetworkHistory(source.networkHistory, taskIDs)
    if (networkHistory) snapshot.network_history = networkHistory
  }
  if (selected && source.history) snapshot.history = themeHistory(source)
  boundedJSON(snapshot, { depth: 12, keys: 100000, bytes: MAX_THEME_SNAPSHOT_BYTES })
  return snapshot
}

function publicRange(source: WorkspaceRange): ThemeRange {
  if (source.mode === "relative") {
    if (!Number.isInteger(source.seconds) || source.seconds <= 0 || source.seconds > MAX_RANGE_SECONDS) throw new ThemeDataError("invalid_data")
    return { mode: "relative", seconds: source.seconds }
  }
  const from = Date.parse(source.from)
  const to = Date.parse(source.to)
  if (![from, to].every(Number.isFinite) || from < 0 || from >= to || to - from > MAX_RANGE_SECONDS * 1000) throw new ThemeDataError("invalid_data")
  return { mode: "absolute", from: new Date(from).toISOString(), to: new Date(to).toISOString() }
}

function publicSite(source: ThemeSnapshotInput): ThemeSnapshot["site"] {
  return { site_title: publicText(source.site.site_title), site_description: publicText(source.site.site_description),
    logo_url: publicText(source.site.logo_url), favicon_url: publicText(source.site.favicon_url),
    show_ip_addresses: source.site.public_address_mode !== "hidden", show_network_quality: source.site.show_network_quality === true }
}

function themeHistory(source: ThemeSnapshotInput): ThemeSnapshot["history"] {
  const history = source.history!
  if (!finiteNumber(history.from) || !finiteNumber(history.to) || history.from > history.to) throw new ThemeDataError("invalid_data")
  const allowed = new Set<string>(THEME_METRICS)
  const names = source.customMetricNames ?? []
  assertCount(names, 64)
  for (const name of names) if (name.startsWith("custom_") && publicText(name).length <= 128) allowed.add(name)
  const publicMetrics = source.historyMetricNames ?? []
  assertCount(publicMetrics, 64)
  for (const name of publicMetrics) if (name && publicText(name).length <= 128) allowed.add(name)
  const series: Record<string, ThemeHistoryPoint[]> = {}
  const information: Record<string, ThemeHistorySeriesInfo> = {}
  const entries = Object.entries(history.series ?? {}).filter(([key]) => allowed.has(key))
  assertCount(entries, 64)
  for (const [name, points] of entries) {
    if (!Array.isArray(points)) throw new ThemeDataError("invalid_data")
    assertCount(points, 1024)
    series[name] = points.filter((point) => finiteNumber(point.timestamp) && (point.value === null || finiteNumber(point.value))).map((point) => ({ timestamp: point.timestamp, value: point.value }))
    if (history.series_info?.[name]) information[name] = historySeriesInfo(history.series_info[name])
  }
  return { from: history.from, to: history.to, series, ...(Object.keys(information).length ? { series_info: information } : {}) }
}

function historySeriesInfo(source: ThemeHistorySeriesInfo): ThemeHistorySeriesInfo {
  if (!["avg", "min", "max", "sum", "last", "rate"].includes(source.aggregation) || typeof source.unit !== "string" || !source.unit || source.unit.length > 128) throw new ThemeDataError("invalid_data")
  return { aggregation: source.aggregation, unit: publicText(source.unit) }
}
