import { describe, expect, it } from "vitest"
import { workspaceVariableParameters, workspaceVariableSearch, workspaceVariableSelections } from "@/lib/workspace-url-state"
import type { DashboardVariable } from "@/types/metric-workspace"

const variables: DashboardVariable[] = [
  { name: "node", type: "node", label: "Node" },
  { name: "farm", type: "group", label: "Farm", multi: true },
  { name: "all_farm", type: "group", label: "All farm", multi: true, include_all: true },
]

describe("workspace URL variable state", () => {
  it("reads single, multi and All selections from the address bar", () => {
    const search = new URLSearchParams("var-node=node-a&var-farm=group-a,group-b&var-all_farm=$__all")
    expect(workspaceVariableSelections(variables, search)).toEqual({ node: ["node-a"], farm: ["group-a", "group-b"], all_farm: ["$__all"] })
  })

  it("truncates single-value variables and trims blank entries", () => {
    const search = new URLSearchParams("var-node=node-a,node-b&var-farm= , group-a ,")
    expect(workspaceVariableSelections(variables, search)).toEqual({ node: ["node-a"], farm: ["group-a"] })
  })

  it("ignores unknown keys, empty values and variables the dashboard no longer declares", () => {
    const search = new URLSearchParams("dashboard=board&var-node=&var-gone=node-a")
    expect(workspaceVariableSelections(variables, search)).toEqual({})
  })

  it("writes only declared variables and skips empty selections", () => {
    expect(workspaceVariableParameters(variables, { node: ["node-a", "node-b"], farm: [], gone: ["x"] }))
      .toEqual([["var-node", "node-a"]])
    expect(workspaceVariableParameters(variables, { farm: ["group-a", "group-b"] })).toEqual([["var-farm", "group-a,group-b"]])
  })

  it("replaces stale variable keys while keeping control parameters", () => {
    const previous = new URLSearchParams("dashboard=board&node_id=node-a&var-node=stale&var-gone=old")
    expect(workspaceVariableSearch(variables, { node: ["node-a"] }, previous).toString())
      .toBe("dashboard=board&node_id=node-a&var-node=node-a")
  })

  it("keeps the previous query string when nothing changes", () => {
    const previous = new URLSearchParams("dashboard=board&var-node=node-a")
    expect(workspaceVariableSearch(variables, { node: ["node-a"] }, previous).toString()).toBe(previous.toString())
  })
})
