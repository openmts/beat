import { describe, expect, it } from "vitest"
import { filterWorkspaceOptions, maximumVariableRegexBytes, variableRegexError, workspaceVariableOptions } from "@/lib/workspace-variable-options"
import { workspaceNode } from "@/test/metric-workspace-fixtures"
import type { Group } from "@/types"

const groups: Group[] = [
  { id: "group-one", name: "Production", sort_order: 0, is_default: true, created_at: "", updated_at: "" },
  { id: "group-two", name: "Staging", sort_order: 1, is_default: false, created_at: "", updated_at: "" },
]

function nodes() {
  const aliased = workspaceNode("node-one")
  aliased.alias = "West host"
  return [aliased, workspaceNode("node-two")]
}

describe("workspace variable options", () => {
  it("maps node variables to aliases and group variables to names", () => {
    expect(workspaceVariableOptions({ name: "node", type: "node", label: "Node" }, nodes(), groups))
      .toEqual([{ value: "node-one", label: "West host" }, { value: "node-two", label: "Production host" }])
    expect(workspaceVariableOptions({ name: "group", type: "group", label: "Group" }, nodes(), groups))
      .toEqual([{ value: "group-one", label: "Production" }, { value: "group-two", label: "Staging" }])
  })

  it("filters labels with the variable regex and keeps the original list otherwise", () => {
    const options = workspaceVariableOptions({ name: "node", type: "node", label: "Node", regex: "^West" }, nodes(), groups)
    expect(options.map((option) => option.value)).toEqual(["node-one"])
    expect(filterWorkspaceOptions(options, "")).toEqual(options)
    expect(filterWorkspaceOptions([], "^West")).toEqual([])
  })

  it("ignores invalid or oversized filters instead of failing a render", () => {
    const options = [{ value: "a", label: "Alpha" }]
    expect(filterWorkspaceOptions(options, "[" )).toEqual(options)
    expect(filterWorkspaceOptions(options, "a".repeat(maximumVariableRegexBytes + 1))).toEqual(options)
    expect(variableRegexError("^West")).toBeNull()
    expect(variableRegexError("")).toBeNull()
    expect(variableRegexError("[")).toBe("workspace.variable_regex_invalid")
    expect(variableRegexError("a".repeat(maximumVariableRegexBytes + 1))).toBe("workspace.variable_regex_too_long")
  })
})
