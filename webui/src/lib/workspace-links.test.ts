import { describe, expect, it } from "vitest"
import { dashboardLinkValues, linksHiddenInReadOnly, panelLinkValues, resolvePanelLinkURL } from "@/lib/workspace-links"
import type { DashboardDefinition, DashboardPanel } from "@/types/metric-workspace"
import type { Node } from "@/types"

const range = { mode: "absolute" as const, from: "2026-10-17T00:00:00.000Z", to: "2026-10-17T01:00:00.000Z" }
const definition: DashboardDefinition = {
  version: 1, title: "Probe", description: "", time_range_seconds: 3600, refresh_seconds: 0,
  variables: [
    { name: "node", type: "node", label: "Node" },
    { name: "env", type: "group", label: "Env" },
  ],
  panels: [],
}
const node = { id: "n1", name: "host-1", alias: "主机 1", group_id: "g1" } as unknown as Node
const panel: DashboardPanel = { id: "p1", title: "CPU", type: "timeseries", size: "medium", legend: true, thresholds: [], series: [{ ref_id: "A", node_id: "n1", metric: "cpu", aggregation: "avg", color: "blue", label: "CPU" }] }

describe("link variable placeholders", () => {
  it("joins every selection into `${__all_variables}`", () => {
    const values = dashboardLinkValues({ selections: { node: ["n1", "n2"], env: ["prod"] }, range })
    expect(values["var-node"]).toBe("n1,n2")
    expect(values["var-env"]).toBe("prod")
    expect(values.__all_variables).toBe("var-node=n1,n2&var-env=prod")
    expect(values.__url_time_range).toBe("from=2026-10-17T00%3A00%3A00.000Z&to=2026-10-17T01%3A00%3A00.000Z")
  })

  it("offers the pooled placeholder on panel links too, next to the series context", () => {
    const values = panelLinkValues({ panel, definition, selections: { node: ["n1"] }, nodes: [node], range })
    expect(values.node).toBe("n1")
    expect(values.__all_variables).toBe("var-node=n1")
    expect(resolvePanelLinkURL("/admin/metrics?${__all_variables}&node=${node}", values)).toBe("/admin/metrics?var-node=n1&node=n1")
  })

  it("leaves unknown placeholders untouched so typos stay visible", () => {
    expect(resolvePanelLinkURL("/admin/metrics?${__nope}", { from: "x" })).toBe("/admin/metrics?${__nope}")
  })
})

describe("read-only link hiding", () => {
  it("only hides when the dashboard turns it on explicitly", () => {
    expect(linksHiddenInReadOnly(definition)).toBe(false)
    expect(linksHiddenInReadOnly({ ...definition, hide_links: false })).toBe(false)
    expect(linksHiddenInReadOnly({ ...definition, hide_links: true })).toBe(true)
  })
})
