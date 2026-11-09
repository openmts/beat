import type { NetworkTaskView } from "@/types"
import type {
  ThemeJSON,
  ThemeNetworkHistory,
  ThemeNetworkHistoryInput,
  ThemeNetworkHistoryInputPoint,
} from "@/types/theme-runtime"
import { assertCount, finiteNumber, publicID, publicText, ThemeDataError } from "@/lib/theme-json"

export const MAX_THEME_NETWORK_TASKS = 16
export const MAX_THEME_NETWORK_POINTS = 600
export const MAX_THEME_NETWORK_SERIES = 20
const MAX_NETWORK_HISTORY_SECONDS = 31 * 24 * 60 * 60

export function themeNetwork(views: NetworkTaskView[], visible: ReadonlySet<string>): ThemeJSON[] {
  const tasks = views.filter(({ task }) => task.is_public === true && task.enabled === true)
  assertCount(tasks, 200)
  return tasks.map((view) => {
    const nodes = view.nodes.filter(({ node }) => visible.has(node.id))
    assertCount(nodes, 1000)
    const targets = (view.task.targets ?? []).map((target) => ({
      id: publicID(target.id), label: publicText(target.label), carrier: publicText(target.carrier),
      region: publicText(target.region), visible_address: target.visible_address === true,
    }))
    return { id: publicID(view.task.id), name: publicText(view.task.name), type: publicText(view.task.type), targets, nodes: nodes.map((state) => {
      const targetStates = (state.targets ?? []).flatMap((target) => target.target_id
        ? [{ target_id: publicID(target.target_id), latest: themeNetworkLatest(target.latest) }]
        : [])
      return { node_id: publicID(state.node.id), latest: themeNetworkLatest(state.latest), targets: targetStates }
    }) }
  })
}

function themeNetworkLatest(latest: NetworkTaskView["nodes"][number]["latest"]): ThemeJSON {
  if (!latest) return null
  const value: Record<string, ThemeJSON> = { timestamp: publicText(latest.timestamp), success: latest.success === true }
  if (finiteNumber(latest.latency_ms)) value.latency_ms = latest.latency_ms
  if (finiteNumber(latest.status_code)) value.status_code = latest.status_code
  if (finiteNumber(latest.loss_percent)) value.loss_percent = latest.loss_percent
  if (finiteNumber(latest.success_percent)) value.success_percent = latest.success_percent
  if (finiteNumber(latest.p95_latency_ms)) value.p95_latency_ms = latest.p95_latency_ms
  return value
}

export function themeNetworkHistory(source: ThemeNetworkHistoryInput | undefined, allowedTasks: ReadonlySet<string>): ThemeNetworkHistory | undefined {
  if (!source) return undefined
  if (!finiteNumber(source.from) || !finiteNumber(source.to) || source.from < 0 || source.from >= source.to || source.to - source.from > MAX_NETWORK_HISTORY_SECONDS) {
    throw new ThemeDataError("invalid_data")
  }
  assertCount(source.tasks, MAX_THEME_NETWORK_TASKS)
  const seen = new Set<string>()
  const tasks: ThemeNetworkHistory["tasks"] = []
  for (const input of source.tasks) {
    const id = publicID(input.task_id)
    if (!allowedTasks.has(id) || seen.has(id)) continue
    if (!Array.isArray(input.points)) throw new ThemeDataError("invalid_data")
    assertCount(input.points, MAX_THEME_NETWORK_POINTS)
    const points = sanitizeNetworkPoints(input.points)
    const series: NonNullable<ThemeNetworkHistory["tasks"][number]["series"]> = []
    assertCount(input.series ?? [], MAX_THEME_NETWORK_SERIES)
    for (const entry of input.series ?? []) {
      if (!entry.target_id) continue
      if (!Array.isArray(entry.points)) throw new ThemeDataError("invalid_data")
      assertCount(entry.points, MAX_THEME_NETWORK_POINTS)
      series.push({
        id: publicID(entry.target_id),
        label: publicText(entry.label),
        carrier: publicText(entry.carrier),
        region: publicText(entry.region),
        visible_address: entry.visible_address === true,
        points: sanitizeNetworkPoints(entry.points),
      })
    }
    seen.add(id)
    tasks.push({ id, points, ...(series.length ? { series } : {}) })
  }
  return { from: source.from, to: source.to, tasks }
}

function sanitizeNetworkPoints(points: ThemeNetworkHistoryInputPoint[]) {
  const sanitized: ThemeNetworkHistory["tasks"][number]["points"] = []
  for (const point of points) {
    if (!finiteNumber(point.timestamp) || point.timestamp < 0) continue
    if (point.latency_ms !== null && (!finiteNumber(point.latency_ms) || point.latency_ms < 0)) continue
    const loss = finiteNumber(point.loss_percent) && point.loss_percent >= 0 && point.loss_percent <= 100
      ? point.loss_percent
      : null
    const success = finiteNumber(point.success_percent) && point.success_percent >= 0 && point.success_percent <= 100
      ? point.success_percent
      : null
    sanitized.push({
      timestamp: point.timestamp,
      latency_ms: point.latency_ms,
      ...(loss !== null ? { loss_percent: loss } : {}),
      ...(success !== null ? { success_percent: success } : {}),
    })
  }
  return sanitized
}
