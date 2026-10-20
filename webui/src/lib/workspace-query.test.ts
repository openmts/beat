import { afterEach, describe, expect, it, vi } from "vitest"
import { dashboardDomain, dashboardQueryPoints, expandPanelSeries, panelMetricQuery, panelQueryPoints, runWorkspaceQueue, workspaceDomain } from "@/lib/workspace-query"
import { workspaceAllSelection } from "@/lib/workspace-variable-options"
import { workspaceDashboard, workspaceNode } from "@/test/metric-workspace-fixtures"

afterEach(() => vi.useRealTimers())

describe("workspace queries", () => {
  it("captures relative time once and preserves absolute ranges", () => {
    const now = Date.parse("2026-09-13T01:00:00Z")
    expect(workspaceDomain({ mode: "relative", seconds: 3600 }, now)).toEqual({ from: "2026-09-13T00:00:00.000Z", to: "2026-09-13T01:00:00.000Z" })
    expect(workspaceDomain({ mode: "absolute", from: "2026-09-13T00:00Z", to: "2026-09-13T01:00Z" }, now + 9000)).toEqual(workspaceDomain({ mode: "relative", seconds: 3600 }, now))
    for (const seconds of [0, -1, 32 * 86400, Infinity]) expect(() => workspaceDomain({ mode: "relative", seconds }, now)).toThrow("workspace.invalid_range")
    expect(() => workspaceDomain({ mode: "absolute", from: "invalid", to: "invalid" })).toThrow("workspace.invalid_range")
  })

  it("shifts the relative now by the dashboard now delay and clamps the point budget", () => {
    const now = Date.parse("2026-09-13T01:00:00Z")
    expect(dashboardDomain({ mode: "relative", seconds: 3600 }, { now_delay_seconds: 60 }, now)).toEqual({ from: "2026-09-12T23:59:00.000Z", to: "2026-09-13T00:59:00.000Z" })
    const absolute = { mode: "absolute" as const, from: "2026-09-13T00:00Z", to: "2026-09-13T01:00Z" }
    expect(dashboardDomain(absolute, { now_delay_seconds: 600 }, now)).toEqual(workspaceDomain(absolute, now))
    expect(dashboardQueryPoints({})).toBe(600)
    expect(dashboardQueryPoints({ max_points: 120 })).toBe(120)
    expect(dashboardQueryPoints({ max_points: 1 })).toBe(600)
    expect(panelQueryPoints({}, { max_points: 300 })).toBe(300)
    expect(panelQueryPoints({ max_points: 90 }, { max_points: 300 })).toBe(90)
    expect(panelQueryPoints({ max_points: 601 }, { max_points: 300 })).toBe(300)
  })

  it("expands visible node and group variables without leaking presentation into the request", () => {
    const definition = workspaceDashboard().definition
    const node = workspaceNode()
    node.alias = "West host"
    const context = { definition, selections: { node: [node.id] }, nodes: [node] }
    const series = expandPanelSeries(definition.panels[0], context)
    expect(series[0].node_name).toBe("West host")
    const query = panelMetricQuery(series, workspaceDomain({ mode: "relative", seconds: 60 }))
    expect(query.series[0]).toEqual({ ref_id: "A", node_id: node.id, metric: "cpu", aggregation: "avg" })
    expect(query.series[0]).not.toHaveProperty("color")
    definition.variables[0].type = "group"
    context.selections.node = ["group-one"]
    context.nodes.push(workspaceNode("node-two"))
    expect(expandPanelSeries(definition.panels[0], context).map((value) => value.ref_id)).toEqual(["A_1", "A_2"])
  })

  it("expands a multi-select node variable into one series per selected node", () => {
    const definition = workspaceDashboard().definition
    definition.variables[0] = { ...definition.variables[0], multi: true }
    definition.panels[0].series[0].node_id = "$node"
    const context = { definition, selections: { node: ["node-one", "node-two"] }, nodes: [workspaceNode(), workspaceNode("node-two")] }
    expect(expandPanelSeries(definition.panels[0], context).map((series) => series.node_id)).toEqual(["node-one", "node-two"])
    expect(expandPanelSeries(definition.panels[0], context).map((series) => series.ref_id)).toEqual(["A_1", "A_2"])
    expect(() => expandPanelSeries(definition.panels[0], { ...context, selections: { node: [] } })).toThrow("workspace.select_node")
  })

  it("expands an All selection to every node visible to the dashboard", () => {
    const definition = workspaceDashboard().definition
    definition.variables[0] = { ...definition.variables[0], multi: true, include_all: true, regex: "^West" }
    definition.panels[0].series[0].node_id = "$node"
    const west = workspaceNode(), east = workspaceNode("node-two")
    west.alias = "West host"
    east.alias = "East host"
    const context = { definition, selections: { node: [workspaceAllSelection] }, nodes: [west, east] }
    expect(expandPanelSeries(definition.panels[0], context).map((series) => series.node_id)).toEqual(["node-one"])
    expect(() => expandPanelSeries(definition.panels[0], { ...context, nodes: [] })).toThrow("workspace.select_node")
  })

  it("rejects missing selections, excessive expansion and duplicate references", () => {
    const definition = workspaceDashboard().definition
    const context = { definition, selections: { node: ["node-one"] }, nodes: [workspaceNode()] }
    expect(() => expandPanelSeries(definition.panels[0], { ...context, nodes: [] })).toThrow("workspace.select_node")
    definition.panels[0].series[0].node_id = "$missing"
    expect(() => expandPanelSeries(definition.panels[0], context)).toThrow("workspace.select_node")
    definition.panels[0].series[0].node_id = "node-one"
    expect(expandPanelSeries(definition.panels[0], context)).toHaveLength(1)
    definition.panels[0].series.push({ ...definition.panels[0].series[0] })
    expect(() => expandPanelSeries(definition.panels[0], context)).toThrow("workspace.duplicate_series")
    definition.panels[0].series = [{ ...definition.panels[0].series[0], node_id: "$node" }]
    definition.variables[0].type = "group"
    expect(() => expandPanelSeries(definition.panels[0], { ...context, selections: { node: ["group-one"] }, nodes: Array.from({ length: 9 }, (_, i) => workspaceNode(String(i))) })).toThrow("workspace.series_limit")
  })

  it("sends the resolved point budget with the panel query and defaults to the server cap", () => {
    const expanded = expandPanelSeries(workspaceDashboard().definition.panels[0], { definition: workspaceDashboard().definition, selections: { node: ["node-one"] }, nodes: [workspaceNode()] })
    const domain = workspaceDomain({ mode: "relative", seconds: 3600 }, Date.parse("2026-09-13T01:00:00Z"))
    expect(panelMetricQuery(expanded, domain).max_points).toBe(600)
    expect(panelMetricQuery(expanded, domain, 120).max_points).toBe(120)
  })

  it("limits concurrent panels and stops scheduling after cancellation", async () => {
    vi.useFakeTimers()
    const controller = new AbortController()
    let active = 0
    let peak = 0
    const visited: number[] = []
    const done = runWorkspaceQueue([1, 2, 3, 4], async (value) => {
      visited.push(value)
      peak = Math.max(peak, ++active)
      await new Promise((resolve) => setTimeout(resolve, 1))
      active--
    }, controller.signal)
    expect(visited).toEqual([1, 2])
    await vi.runAllTimersAsync()
    await done
    expect(peak).toBe(2)
    expect(visited).toEqual([1, 2, 3, 4])
    controller.abort()
    await runWorkspaceQueue([5], async () => { throw new Error("must not run") }, controller.signal)
  })
})
