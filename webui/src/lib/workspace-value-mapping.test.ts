import { describe, expect, it } from "vitest"
import { emptyValueMapping, mappedWorkspaceValue, maximumValueMappings, switchValueMappingType, valueMappingError } from "./workspace-value-mapping"
import type { DashboardValueMapping } from "@/types/metric-workspace"

const stat = { type: "stat" as const }

describe("panel value mappings", () => {
  it("returns the first matching mapping and ignores the rest", () => {
    const mappings: DashboardValueMapping[] = [{ type: "value", value: 0, text: "offline" }, { type: "range", to: 10, color: "amber" }]
    const panel = { ...stat, value_mappings: mappings }
    expect(mappedWorkspaceValue(panel, 0)).toEqual({ text: "offline" })
    expect(mappedWorkspaceValue(panel, 9)).toEqual({ color: "amber" })
    expect(mappedWorkspaceValue(panel, 10)).toBeNull()
  })

  it("matches ranges with open bounds and empty samples separately", () => {
    const mappings: DashboardValueMapping[] = [
      { type: "range", from: 60, text: "warm" }, { type: "range", to: 0, text: "below" }, { type: "special", match: "null", text: "no data" },
    ]
    const panel = { ...stat, value_mappings: mappings }
    expect(mappedWorkspaceValue(panel, 60)).toEqual({ text: "warm" })
    expect(mappedWorkspaceValue(panel, -1)).toEqual({ text: "below" })
    expect(mappedWorkspaceValue(panel, null)).toEqual({ text: "no data" })
    expect(mappedWorkspaceValue(panel, 30)).toBeNull()
  })

  it("never matches numeric mappings against empty samples", () => {
    expect(mappedWorkspaceValue({ ...stat, value_mappings: [{ type: "value", value: 0, text: "offline" }] }, null)).toBeNull()
    expect(mappedWorkspaceValue({ ...stat }, 1)).toBeNull()
  })

  it("mirrors the server bounds and the panel type rule", () => {
    expect(valueMappingError({ ...stat, value_mappings: [{ type: "value", value: 1, text: "one" }] })).toBeNull()
    expect(valueMappingError({ ...stat, value_mappings: [{ type: "value", value: 1, color: "rose" }] })).toBeNull()
    expect(valueMappingError({ ...stat, value_mappings: [{ type: "value", value: 0 }] })).toBe("workspace.value_mapping_empty")
    expect(valueMappingError({ ...stat, value_mappings: [{ type: "value", text: "x" }] })).toBe("workspace.value_mapping_value_invalid")
    expect(valueMappingError({ ...stat, value_mappings: [{ type: "range", text: "x" }] })).toBe("workspace.value_mapping_range_invalid")
    expect(valueMappingError({ ...stat, value_mappings: [{ type: "range", from: 2, to: 2, text: "x" }] })).toBe("workspace.value_mapping_range_invalid")
    expect(valueMappingError({ ...stat, value_mappings: [{ type: "special", text: "x" }] })).toBe("workspace.value_mapping_match_invalid")
    expect(valueMappingError({ ...stat, value_mappings: [{ type: "value", value: 1, text: "x".repeat(33) }] })).toBe("workspace.value_mapping_text_too_long")
    expect(valueMappingError({ ...stat, value_mappings: Array.from({ length: maximumValueMappings + 1 }, () => ({ type: "value" as const, value: 1, text: "x" })) }))
      .toBe("workspace.value_mappings_too_many")
    expect(valueMappingError({ type: "timeseries", value_mappings: [{ type: "value", value: 1, text: "x" }] })).toBe("workspace.value_mappings_timeseries_only")
    expect(valueMappingError({ type: "histogram", value_mappings: [{ type: "value", value: 1, text: "x" }] })).toBe("workspace.value_mappings_timeseries_only")
    expect(valueMappingError({ type: "gauge" })).toBeNull()
  })

  it("seeds editable defaults per mapping type", () => {
    const source: DashboardValueMapping = { type: "value", value: 3, text: "three", color: "rose" }
    expect(switchValueMappingType(source, "range")).toEqual({ type: "range", from: 0, to: 10, text: "three", color: "rose" })
    expect(switchValueMappingType(source, "special")).toEqual({ type: "special", match: "null", text: "three", color: "rose" })
    expect(emptyValueMapping()).toEqual({ type: "value", value: 0, text: "0", color: "rose" })
  })
})
