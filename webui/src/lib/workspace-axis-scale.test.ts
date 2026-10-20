import { describe, expect, it } from "vitest"
import { axisScale, axisScaleError, axisScaleModes, axisScalePanelTypes, axisScalePatch, maskLogRows } from "@/lib/workspace-axis-scale"
import type { DashboardPanel } from "@/types/metric-workspace"

const panel = (patch: Partial<DashboardPanel>): DashboardPanel => ({ id: "p1", title: "CPU", type: "timeseries", size: "medium", legend: true, thresholds: [], series: [], ...patch })

describe("axis scale", () => {
  it("falls back to the linear default", () => {
    expect(axisScaleModes).toEqual(["linear", "log"])
    expect(axisScalePanelTypes).toEqual(["timeseries", "barchart"])
    expect(axisScale(panel({}))).toBe("linear")
    expect(axisScale(panel({ scale: "linear" }))).toBe("linear")
    expect(axisScale(panel({ scale: "log" }))).toBe("log")
    expect(axisScale(panel({ scale: "log2" as never }))).toBe("linear")
  })

  it("mirrors the server rules", () => {
    expect(axisScaleError(panel({}))).toBeNull()
    expect(axisScaleError(panel({ scale: "linear" }))).toBeNull()
    expect(axisScaleError(panel({ type: "barchart", scale: "log" }))).toBeNull()
    expect(axisScaleError(panel({ scale: "log", min: 1 }))).toBeNull()
    expect(axisScaleError(panel({ scale: "log", soft_min: 0.5 }))).toBeNull()
    expect(axisScaleError(panel({ scale: "log2" as never }))).toBe("workspace.axis_scale_invalid")
    expect(axisScaleError(panel({ type: "stat", scale: "log" }))).toBe("workspace.axis_scale_panels_only")
    expect(axisScaleError(panel({ type: "stat", scale: "linear" }))).toBe("workspace.axis_scale_panels_only")
    expect(axisScaleError(panel({ scale: "log", min: 0 }))).toBe("workspace.axis_scale_positive_minimum")
    expect(axisScaleError(panel({ scale: "log", soft_min: -1 }))).toBe("workspace.axis_scale_positive_minimum")
  })

  it("clears the field when the panel type cannot render it", () => {
    expect(axisScalePatch("timeseries")).toEqual({})
    expect(axisScalePatch("barchart")).toEqual({})
    expect(axisScalePatch("stat")).toEqual({ scale: undefined })
    expect(axisScalePatch("table")).toEqual({ scale: undefined })
  })

  it("drops non-positive samples on a log axis", () => {
    const rows = [{ timestamp: 1, A: 0, B: 12 }, { timestamp: 2, A: -4, B: null }, { timestamp: 3, A: 8, B: 0.5 }]
    expect(maskLogRows(rows, ["A", "B"])).toEqual([
      { timestamp: 1, A: null, B: 12 },
      { timestamp: 2, A: null, B: null },
      { timestamp: 3, A: 8, B: 0.5 },
    ])
    expect(rows[0]).toEqual({ timestamp: 1, A: 0, B: 12 })
  })
})
