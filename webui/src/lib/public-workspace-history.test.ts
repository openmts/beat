import { describe, expect, it } from "vitest"
import { buildPublicWorkspaceHistory } from "@/lib/public-workspace-history"
import { workspaceDashboard, workspaceMetric, workspaceQuery, workspaceResult } from "@/test/metric-workspace-fixtures"
import type { PanelQueryState } from "@/types/metric-workspace"

function input() {
  return { nodeId: "node-one", definition: workspaceDashboard().definition, catalog: { version: 1, revision: 1, metrics: [workspaceMetric(), workspaceMetric("memory")] },
    panels: { cpu: { loading: false, result: workspaceResult(), resultQuery: workspaceQuery(), updatedAt: 10 } } as Record<string, PanelQueryState> }
}

describe("theme history from successful public panel queries", () => {
  it("preserves null gaps and converts milliseconds to seconds with actual range and aggregation", () => {
    const source = input()
    const history = buildPublicWorkspaceHistory(source)
    expect(history.state).toBe("ready")
    expect(history.history).toEqual({ from: Date.parse(workspaceQuery().from) / 1000, to: Date.parse(workspaceQuery().to) / 1000,
      series: { cpu: [{ timestamp: Date.parse(workspaceQuery().from) / 1000, value: 40 }, { timestamp: Date.parse(workspaceQuery().from) / 1000 + 6, value: null }] },
      series_info: { cpu: { aggregation: "avg", unit: "percent" } } })
    expect(history.metricNames).toEqual(["cpu"])
  })
  it("never invents successful empty history before a query and ignores other nodes/private series", () => {
    const source = input()
    source.panels.cpu = { loading: true, query: workspaceQuery() }
    expect(buildPublicWorkspaceHistory(source)).toMatchObject({ state: "loading", history: undefined, metricNames: [] })
    source.panels.cpu = { loading: false, result: workspaceResult(), resultQuery: workspaceQuery() }
    source.panels.cpu.result!.series[0].node_id = "other-node"
    expect(buildPublicWorkspaceHistory(source).history).toBeUndefined()
    source.panels.cpu.result!.series[0].node_id = "node-one"
    source.catalog.metrics[0].public = false
    expect(buildPublicWorkspaceHistory(source).history).toBeUndefined()
  })
  it("retains actual successful range on failure and never mixes refresh domains", () => {
    const source = input()
    source.definition.panels.push({ ...source.definition.panels[0], id: "memory", series: [{ ...source.definition.panels[0].series[0], metric: "memory" }] })
    const old = workspaceResult()
    old.series[0].metric = "memory"
    source.panels.memory = { loading: false, error: "failed", result: old, resultQuery: { ...workspaceQuery(), series: [{ ...workspaceQuery().series[0], metric: "memory" }] }, updatedAt: 9 }
    source.panels.cpu.result = { ...workspaceResult(), from: "2026-09-13T02:00:00.000Z", to: "2026-09-13T03:00:00.000Z" }
    source.panels.cpu.updatedAt = 20
    const history = buildPublicWorkspaceHistory(source)
    expect(history.state).toBe("error")
    expect(history.history?.from).toBe(Date.parse("2026-09-13T02:00:00Z") / 1000)
    expect(history.history?.series).not.toHaveProperty("memory")
    source.panels.cpu.loading = true
    source.panels.cpu.query = { ...workspaceQuery(), from: "2026-09-13T04:00:00.000Z", to: "2026-09-13T05:00:00.000Z" }
    expect(buildPublicWorkspaceHistory(source).history?.to).toBe(Date.parse("2026-09-13T03:00:00Z") / 1000)
  })
  it("uses template order for duplicate metrics and only accepts actually requested series", () => {
    const source = input()
    const duplicate = { ...source.definition.panels[0], id: "cpu-max", series: [{ ...source.definition.panels[0].series[0], aggregation: "max" as const }] }
    source.definition.panels.push(duplicate)
    source.panels[duplicate.id] = { loading: false, updatedAt: 20, result: { ...workspaceResult(), series: [{ ...workspaceResult().series[0], aggregation: "max", points: [{ timestamp: Date.parse(workspaceQuery().from), value: 90 }] }] },
      resultQuery: { ...workspaceQuery(), series: [{ ...workspaceQuery().series[0], aggregation: "max" }] } }
    expect(buildPublicWorkspaceHistory(source).history?.series.cpu[0].value).toBe(40)
    source.panels.cpu.result!.series.push({ ...workspaceResult().series[0], ref_id: "unexpected", metric: "memory" })
    expect(buildPublicWorkspaceHistory(source).history?.series).not.toHaveProperty("memory")
  })
})
