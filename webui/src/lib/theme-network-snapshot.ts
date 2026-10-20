import type { NetworkTaskView } from "@/types"
import type { ThemeJSON } from "@/types/theme-runtime"
import { assertCount, finiteNumber, publicID, publicText } from "@/lib/theme-json"

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
