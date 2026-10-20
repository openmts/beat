import { describe, expect, it } from "vitest"
import { axisDisplayError, axisDisplayPatch, axisGrid, axisPlacement, axisPlacements, centeredZeroDomain, centeredZeroError } from "@/lib/workspace-axis-display"
import type { DashboardPanel } from "@/types/metric-workspace"

const panel = (patch: Partial<DashboardPanel>): DashboardPanel => ({ id: "p1", title: "CPU", type: "timeseries", size: "medium", legend: true, thresholds: [], series: [], ...patch })

describe("axis display options", () => {
  it("defaults to grid lines on the left", () => {
    expect(axisPlacements).toEqual(["left", "right"])
    expect(axisGrid(panel({}))).toBe(true)
    expect(axisGrid(panel({ grid: true }))).toBe(true)
    expect(axisGrid(panel({ grid: false }))).toBe(false)
    expect(axisPlacement(panel({}))).toBe("left")
    expect(axisPlacement(panel({ axis_placement: "left" }))).toBe("left")
    expect(axisPlacement(panel({ axis_placement: "right" }))).toBe("right")
    expect(axisPlacement(panel({ axis_placement: "top" as never }))).toBe("left")
  })

  it("mirrors the server rules", () => {
    expect(axisDisplayError(panel({}))).toBeNull()
    expect(axisDisplayError(panel({ grid: false }))).toBeNull()
    expect(axisDisplayError(panel({ type: "barchart", axis_placement: "right" }))).toBeNull()
    expect(axisDisplayError(panel({ axis_placement: "left" }))).toBeNull()
    expect(axisDisplayError(panel({ type: "stat", grid: false }))).toBe("workspace.axis_display_panels_only")
    expect(axisDisplayError(panel({ type: "table", axis_placement: "right" }))).toBe("workspace.axis_display_panels_only")
    expect(axisDisplayError(panel({ axis_placement: "top" as never }))).toBe("workspace.axis_placement_invalid")
    // 空字符串与未声明等效：切回缺省时不留无效组合。
    expect(axisDisplayError({ type: "stat", axis_placement: "" })).toBeNull()
  })

  it("centers the domain around zero and mirrors the server rules", () => {
    expect(centeredZeroError(panel({ centered_zero: true }))).toBeNull()
    expect(centeredZeroError(panel({ type: "barchart", centered_zero: true }))).toBeNull()
    expect(centeredZeroError(panel({ centered_zero: false }))).toBeNull()
    expect(centeredZeroError(panel({ type: "stat", centered_zero: true }))).toBe("workspace.centered_zero_panels_only")
    expect(centeredZeroError(panel({ centered_zero: true, min: 0 }))).toBe("workspace.centered_zero_fixed_limits")
    expect(centeredZeroError(panel({ centered_zero: true, max: 100 }))).toBe("workspace.centered_zero_fixed_limits")

    expect(centeredZeroDomain(panel({}), 12, [0, 100])).toEqual([0, 100])
    expect(centeredZeroDomain(panel({ centered_zero: true }), 12, [0, 100])).toEqual([-12, 12])
    // 柔性上下限也参与对称展开：它们可能比数据更大。
    expect(centeredZeroDomain(panel({ centered_zero: true, soft_max: 50 }), 12, ["auto", "auto"])).toEqual([-50, 50])
    expect(centeredZeroDomain(panel({ centered_zero: true, soft_min: -30 }), 12, ["auto", "auto"])).toEqual([-30, 30])
    // 没有可用正数时保持原 domain，避免出现 [0, 0] 这种退化坐标轴。
    expect(centeredZeroDomain(panel({ centered_zero: true }), 0, ["auto", "auto"])).toEqual(["auto", "auto"])
    expect(centeredZeroDomain(panel({ centered_zero: true }), Number.NaN, ["auto", "auto"])).toEqual(["auto", "auto"])
  })

  it("clears the fields when the panel type cannot render them", () => {
    expect(axisDisplayPatch("timeseries")).toEqual({})
    expect(axisDisplayPatch("barchart")).toEqual({})
    expect(axisDisplayPatch("stat")).toEqual({ grid: undefined, axis_placement: undefined, centered_zero: undefined })
    expect(axisDisplayPatch("row")).toEqual({ grid: undefined, axis_placement: undefined, centered_zero: undefined })
  })
})
