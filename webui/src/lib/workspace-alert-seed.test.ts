import { describe, expect, it } from "vitest"
import { workspaceAlertLink, workspaceAlertSeed, workspaceAlertSeedFromQuery } from "@/lib/workspace-alert-seed"
import { workspaceDashboard, workspaceNode } from "@/test/metric-workspace-fixtures"

const definition = workspaceDashboard().definition
const panel = definition.panels[0]

describe("workspace alert seed", () => {
  it("maps the first series, the dashboard window and the first threshold", () => {
    const seed = workspaceAlertSeed({ ...panel, thresholds: [{ value: 95, color: "rose" }, { value: 85, color: "amber" }] },
      { definition, selections: { node: ["node-one"] }, nodes: [workspaceNode()] })
    expect(seed).toEqual({ metric: "cpu", aggregation: "avg", window_seconds: 3600,
      scope: { kind: "nodes", node_ids: ["node-one"] }, threshold: 85, name: "CPU usage" })
    expect(workspaceAlertLink(seed!)).toBe("/admin/alerts?tab=rules&metric=cpu&aggregation=avg&scope=nodes&window=3600&node_id=node-one&threshold=85&name=CPU+usage")
  })

  it("falls back to the whole fleet when a group variable expands to several nodes", () => {
    const grouped = { ...panel, series: [{ ...panel.series[0], node_id: "$group" }] }
    const groupedDefinition = { ...definition, variables: [{ name: "group", type: "group" as const, label: "Group" }] }
    const seed = workspaceAlertSeed(grouped, { definition: groupedDefinition, selections: { group: ["group-one"] }, nodes: [workspaceNode(), workspaceNode("node-two")] })
    expect(seed?.scope).toEqual({ kind: "all" })
    expect(seed?.window_seconds).toBe(3600)
  })

  it("returns null when the panel has no series or cannot resolve a node", () => {
    const context = { definition, selections: {}, nodes: [workspaceNode()] }
    expect(workspaceAlertSeed(panel, context)).toBeNull()
    expect(workspaceAlertSeed({ ...panel, series: [] }, { definition, selections: { node: ["node-one"] }, nodes: [workspaceNode()] })).toBeNull()
  })

  it("round-trips a link and drops malformed fields", () => {
    const query = new URLSearchParams("tab=rules&metric=cpu&aggregation=avg&scope=nodes&window=900&node_id=node-one&threshold=75&name=Node+CPU+high")
    expect(workspaceAlertSeedFromQuery(query)).toEqual({ metric: "cpu", aggregation: "avg", window_seconds: 900,
      scope: { kind: "nodes", node_ids: ["node-one"] }, threshold: 75, name: "Node CPU high" })
    expect(workspaceAlertSeedFromQuery(new URLSearchParams("metric=../../etc/passwd"))).toBeNull()
    expect(workspaceAlertSeedFromQuery(new URLSearchParams("metric=CPU"))).toBeNull()
    expect(workspaceAlertSeedFromQuery(new URLSearchParams("metric=cpu"))).toEqual({ metric: "cpu", aggregation: "last", window_seconds: 0, scope: { kind: "all" }, threshold: undefined, name: undefined })
    expect(workspaceAlertSeedFromQuery(new URLSearchParams("metric=cpu&aggregation=%3Bdrop&scope=nodes&node_id=%3Brm+-rf&window=5&threshold=abc"))).toEqual(
      { metric: "cpu", aggregation: "last", window_seconds: 0, scope: { kind: "all" }, threshold: undefined, name: undefined })
  })

  it("keeps an accelerator device only when it is a well-formed id", () => {
    const device = "0123456789abcdef0123456789abcdef"
    // 加速器序列必须是固定节点 + 合法设备号，否则面板本身就无法查询。
    const gpuPanel = { ...panel, series: [{ ...panel.series[0], node_id: "node-one", metric: "gpu_utilization_percent", device_id: device }] }
    const seed = workspaceAlertSeed(gpuPanel, { definition, selections: {}, nodes: [workspaceNode()] })
    expect(seed?.scope).toEqual({ kind: "nodes", node_ids: ["node-one"], device_id: device })
    const parsed = workspaceAlertSeedFromQuery(new URLSearchParams(`metric=gpu_memory_percent&scope=nodes&node_id=node-one&device_id=${device}`))
    expect(parsed?.scope).toEqual({ kind: "nodes", node_ids: ["node-one"], device_id: device })
    const spoofed = workspaceAlertSeedFromQuery(new URLSearchParams("metric=cpu&scope=nodes&node_id=node-one&device_id=not-a-device"))
    expect(spoofed?.scope).toEqual({ kind: "nodes", node_ids: ["node-one"] })
  })
})
