import type { NetworkTaskView } from "@/types"
import type { ThemeJSON, ThemeNetworkHistory, ThemeNetworkHistoryInput } from "@/types/theme-runtime"
import { assertCount, finiteNumber, publicID, publicText, ThemeDataError } from "@/lib/theme-json"

export const MAX_THEME_NETWORK_TASKS = 16
export const MAX_THEME_NETWORK_POINTS = 600
const MAX_NETWORK_HISTORY_SECONDS = 31 * 24 * 60 * 60

export function themeNetwork(views: NetworkTaskView[], visible: ReadonlySet<string>): ThemeJSON[] {
  const tasks = views.filter(({ task }) => task.is_public === true && task.enabled === true)
  assertCount(tasks, 200)
  return tasks.map((view) => {
    const nodes = view.nodes.filter(({ node }) => visible.has(node.id))
    assertCount(nodes, 1000)
    return { id: publicID(view.task.id), name: publicText(view.task.name), type: publicText(view.task.type), nodes: nodes.map((state) => {
      if (!state.latest) return { node_id: publicID(state.node.id), latest: null }
      const latest: Record<string, ThemeJSON> = { timestamp: publicText(state.latest.timestamp), success: state.latest.success === true }
      if (finiteNumber(state.latest.latency_ms)) latest.latency_ms = state.latest.latency_ms
      if (finiteNumber(state.latest.status_code)) latest.status_code = state.latest.status_code
      return { node_id: publicID(state.node.id), latest }
    }) }
  })
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
    const points: ThemeNetworkHistory["tasks"][number]["points"] = []
    for (const point of input.points) {
      if (!finiteNumber(point.timestamp) || point.timestamp < 0) continue
      if (point.latency_ms !== null && (!finiteNumber(point.latency_ms) || point.latency_ms < 0)) continue
      points.push({ timestamp: point.timestamp, latency_ms: point.latency_ms })
    }
    seen.add(id)
    tasks.push({ id, points })
  }
  return { from: source.from, to: source.to, tasks }
}
