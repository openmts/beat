import { describe, expect, it } from "vitest"
import { expandPanelSeries } from "@/lib/workspace-query"
import { maxPanelInstances, repeatPanelTitle, repeatedNodes, workspacePanelInstances } from "@/lib/workspace-repeat"
import { workspaceDashboard } from "@/test/metric-workspace-fixtures"
import type { Node } from "@/types"

function node(id: string, group: string, alias = ""): Node {
  return { id, name: `node-${id}`, alias, group_id: group, status: "online" } as Node
}

const nodes = [node("a", "g1", "Alpha"), node("b", "g1"), node("c", "g2", "Gamma")]

function definition(repeat?: string) {
  const base = workspaceDashboard().definition
  return { ...base, variables: [{ name: "group", type: "group" as const, label: "Group" }],
    panels: [{ ...base.panels[0], id: "panel-1", title: "CPU $group", repeat,
      series: [{ ...base.panels[0].series[0], node_id: "$group" }] }] }
}

describe("workspace panel repeat", () => {
  it("renders one instance per node of the selected variable", () => {
    const value = definition("group")
    const instances = workspacePanelInstances(value, { group: ["g1"] }, nodes)
    expect(instances.map((instance) => instance.key)).toEqual(["panel-1::a", "panel-1::b"])
    expect(instances.map((instance) => instance.panel.title)).toEqual(["CPU Alpha", "CPU node-b"])
    expect(instances.every((instance) => instance.base.id === "panel-1")).toBe(true)
    expect(instances[0].binding).toEqual({ variable: "group", node_id: "a", label: "Alpha" })
  })

  it("keeps a single instance when the panel does not repeat or the variable is unset", () => {
    expect(workspacePanelInstances(definition(), { group: ["g1"] }, nodes).map((instance) => instance.key)).toEqual(["panel-1"])
    expect(workspacePanelInstances(definition("group"), {}, nodes).map((instance) => instance.key)).toEqual(["panel-1"])
    const unknown = definition("missing")
    expect(workspacePanelInstances(unknown, { group: ["g1"] }, nodes)[0].key).toBe("panel-1")
  })

  it("binds the repeated variable to the instance node and refuses crossing series", () => {
    const value = definition("group")
    const [first] = workspacePanelInstances(value, { group: ["g1"] }, nodes)
    const expanded = expandPanelSeries(first.panel, { definition: value, selections: { group: ["g1"] }, nodes, binding: first.binding })
    expect(expanded.map((series) => series.node_id)).toEqual(["a"])
    expect(() => expandPanelSeries(first.panel, { definition: value, selections: { group: ["g1"] }, nodes: [], binding: first.binding })).toThrow("workspace.select_node")
  })

  it("caps rendered instances and reports the truncated node count", () => {
    const many = Array.from({ length: maxPanelInstances + 3 }, (_, index) => node(`n${index}`, "g1"))
    const value = definition("group")
    expect(workspacePanelInstances(value, { group: ["g1"] }, many)).toHaveLength(maxPanelInstances)
    expect(repeatedNodes(value.panels[0], value, { group: ["g1"] }, many)).toBe(maxPanelInstances + 3)
    expect(repeatedNodes(value.panels[0], value, { group: ["g1"] }, many)).toBeGreaterThan(maxPanelInstances)
  })

  it("repeats across every selected group of a multi-select variable", () => {
    const base = workspaceDashboard().definition
    const multi = { ...base, variables: [{ name: "group", type: "group" as const, label: "Group", multi: true }],
      panels: [{ ...base.panels[0], id: "panel-1", title: "CPU", repeat: "group" }] }
    const instances = workspacePanelInstances(multi, { group: ["g1", "g2"] }, nodes)
    expect(instances.map((instance) => instance.key)).toEqual(["panel-1::a", "panel-1::c", "panel-1::b"])
    expect(repeatedNodes(multi.panels[0], multi, { group: ["g1", "g2"] }, nodes)).toBe(3)
    expect(workspacePanelInstances(multi, { group: [] }, nodes)[0].key).toBe("panel-1")
  })

  it("appends the node label when the title has no variable token", () => {
    expect(repeatPanelTitle("CPU", "group", "Alpha")).toBe("CPU · Alpha")
    expect(repeatPanelTitle("CPU $group", "group", "Alpha")).toBe("CPU Alpha")
  })
})
