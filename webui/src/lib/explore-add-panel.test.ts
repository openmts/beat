import { describe, expect, it } from "vitest"
import { appendExplorePanels, buildExplorePanels, maximumExplorePanels } from "@/lib/explore-add-panel"
import type { ExploreQuery, ExploreState } from "@/lib/metric-explore"
import type { DashboardDefinition, DashboardPanel } from "@/types/metric-workspace"

const row = (index: number): ExploreQuery => ({ ref_id: `Q${index}`, metric: "cpu", aggregation: "avg", node_id: `node-${index}`, device_id: "", color: "blue", hidden: false })

const state = (count: number): ExploreState => ({ queries: Array.from({ length: count }, (_, index) => row(index + 1)), range: { mode: "relative", seconds: 3600 }, max_points: 600, query_type: "range" })

const definition = (panels: DashboardPanel[]): DashboardDefinition => ({ version: 1, title: "Fleet", description: "", time_range_seconds: 3600, refresh_seconds: 0, variables: [], panels })

const emptyPanel = (id: string): DashboardPanel => ({ id, title: id, type: "timeseries", size: "medium", legend: false, thresholds: [], series: [] })

describe("explore panels", () => {
  it("splits the visible rows into panels of at most eight series", () => {
    const panels = buildExplorePanels({ ...state(9), queries: [...state(9).queries, { ...row(10), hidden: true }] }, "Explore query")
    expect(panels.map((panel) => [panel.title, panel.series.length])).toEqual([["Explore query", 8], ["Explore query 2", 1]])
    expect(panels[0]!.series[0]).toEqual({ ref_id: "Q1", node_id: "node-1", metric: "cpu", aggregation: "avg", color: "blue", label: "" })
  })

  it("carries the device dimension into the panel series", () => {
    const panels = buildExplorePanels({ ...state(1), queries: [{ ...row(1), device_id: "gpu-0" }] }, "Explore query")
    expect(panels[0]!.series[0]!.device_id).toBe("gpu-0")
  })

  it("appends panels with unused ids and refuses to exceed the panel limit", () => {
    const drafts = buildExplorePanels(state(9), "Explore query")
    const appended = appendExplorePanels(definition([emptyPanel("explore-1")]), drafts)
    expect(appended?.panels.map((panel) => panel.id)).toEqual(["explore-1", "explore-2", "explore-3"])
    const full = definition(Array.from({ length: maximumExplorePanels }, (_, index) => emptyPanel(`panel-${index}`)))
    expect(appendExplorePanels(full, drafts)).toBeNull()
  })
})
