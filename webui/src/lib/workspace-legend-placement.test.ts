import { describe, expect, it } from "vitest"
import { legendPlacement, legendPlacementError, legendPlacementPatch } from "@/lib/workspace-legend-placement"
import type { DashboardPanel } from "@/types/metric-workspace"

const panel = (patch: Partial<DashboardPanel>): DashboardPanel => ({ id: "p1", title: "CPU", type: "timeseries", size: "medium", legend: true, thresholds: [], series: [], ...patch })

describe("legend placement", () => {
  it("falls back to the bottom placement", () => {
    expect(legendPlacement(panel({}))).toBe("bottom")
    expect(legendPlacement(panel({ legend_placement: "right" }))).toBe("right")
    expect(legendPlacement(panel({ legend_placement: "top" as never }))).toBe("bottom")
  })

  it("mirrors the server rules", () => {
    expect(legendPlacementError(panel({}))).toBeNull()
    expect(legendPlacementError(panel({ legend_placement: "bottom" }))).toBeNull()
    expect(legendPlacementError(panel({ legend_placement: "right" }))).toBeNull()
    expect(legendPlacementError(panel({ type: "barchart", legend_placement: "right" }))).toBeNull()
    expect(legendPlacementError(panel({ legend: false, legend_placement: "right" }))).toBe("workspace.legend_placement_requires_legend")
    expect(legendPlacementError(panel({ type: "stat", legend_placement: "right" }))).toBe("workspace.legend_placement_panels_only")
    expect(legendPlacementError(panel({ type: "text", legend_placement: "bottom" }))).toBe("workspace.legend_placement_panels_only")
    expect(legendPlacementError(panel({ legend_placement: "top" as never }))).toBe("workspace.legend_placement_invalid")
  })

  it("omits the field when the placement is the default", () => {
    expect(legendPlacementPatch("bottom")).toEqual({ legend_placement: undefined })
    expect(legendPlacementPatch("top")).toEqual({ legend_placement: undefined })
    expect(legendPlacementPatch("right")).toEqual({ legend_placement: "right" })
  })
})
