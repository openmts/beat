import { describe, expect, it } from "vitest"
import { axisDomain, axisError, softAxisFieldNames, softAxisPanelTypes, softAxisPatch } from "@/lib/workspace-axis"
import type { DashboardPanel } from "@/types/metric-workspace"

const panel = (patch: Partial<DashboardPanel>): DashboardPanel => ({ id: "p1", title: "CPU", type: "timeseries", size: "medium", legend: true, thresholds: [], series: [], ...patch })

const evaluate = (limit: unknown, value: number): number => {
  if (typeof limit !== "function") throw new Error("expected a data-driven axis limit")
  return (limit as (input: number) => number)(value)
}

describe("soft axis limits", () => {
  it("stays empty until a soft limit is declared", () => {
    expect(axisError(panel({}))).toBeNull()
    expect(axisError(panel({ min: 0, max: 100 }))).toBeNull()
    expect(axisError(panel({ type: "stat", soft_min: 0 }))).toBe("workspace.soft_axis_timeseries_only")
    expect(softAxisPanelTypes).toEqual(["timeseries", "barchart"])
    expect(softAxisFieldNames).toEqual(["soft_min", "soft_max"])
  })

  it("mirrors the server rules", () => {
    expect(axisError(panel({ soft_min: -10, soft_max: 90 }))).toBeNull()
    expect(axisError(panel({ type: "barchart", soft_max: 90 }))).toBeNull()
    expect(axisError(panel({ soft_min: Number.NaN }))).toBe("workspace.soft_axis_invalid")
    expect(axisError(panel({ soft_max: Number.POSITIVE_INFINITY }))).toBe("workspace.soft_axis_invalid")
    expect(axisError(panel({ soft_min: 90, soft_max: 10 }))).toBe("workspace.soft_axis_order")
    expect(axisError(panel({ soft_min: 10, soft_max: 10 }))).toBe("workspace.soft_axis_order")
    expect(axisError(panel({ min: 0, soft_min: -10 }))).toBe("workspace.soft_axis_conflict")
    expect(axisError(panel({ max: 100, soft_max: 90 }))).toBe("workspace.soft_axis_conflict")
    expect(axisError(panel({ min: 0, soft_max: 90 }))).toBeNull()
  })

  it("clears the fields when the panel type cannot render them", () => {
    expect(softAxisPatch("timeseries")).toEqual({})
    expect(softAxisPatch("barchart")).toEqual({})
    expect(softAxisPatch("stat")).toEqual({ soft_min: undefined, soft_max: undefined })
    expect(softAxisPatch("table")).toEqual({ soft_min: undefined, soft_max: undefined })
  })
})

describe("axis domain", () => {
  it("prefers fixed limits and keeps the percentage default", () => {
    expect(axisDomain(panel({ min: 5, max: 95 }), "percent")).toEqual([5, 95])
    expect(axisDomain(panel({}), "percent")).toEqual([0, 100])
    expect(axisDomain(panel({}), "bytes")).toEqual(["auto", "auto"])
  })

  it("opens the axis only when the data leaves the soft range", () => {
    const lower = axisDomain(panel({ soft_min: -10 }), "bytes")[0]
    expect(evaluate(lower, 5)).toBe(-10)
    expect(evaluate(lower, -50)).toBe(-50)
    const upper = axisDomain(panel({ soft_max: 90 }), "bytes")[1]
    expect(evaluate(upper, 5)).toBe(90)
    expect(evaluate(upper, 200)).toBe(200)
  })

  it("hands a logarithmic axis to the renderer instead of a zero default", () => {
    expect(axisDomain(panel({ scale: "log" }), "percent")).toEqual(["auto", "auto"])
    expect(axisDomain(panel({ scale: "log" }), "bytes")).toEqual(["auto", "auto"])
    expect(axisDomain(panel({ scale: "log", min: 1, soft_max: 90 }), "percent")[0]).toBe(1)
    expect(evaluate(axisDomain(panel({ scale: "log", soft_max: 90 }), "bytes")[1], 5)).toBe(90)
    // 对数轴不接受非正下限：服务端会先拒绝，渲染器这里按自动刻度兜底。
    expect(axisDomain(panel({ scale: "log", min: 0 }), "bytes")).toEqual(["auto", "auto"])
    expect(axisDomain(panel({ scale: "log", soft_min: -1 }), "bytes")).toEqual(["auto", "auto"])
  })

  it("keeps a fixed limit on the other side as a plain number", () => {
    const domain = axisDomain(panel({ min: 0, soft_max: 90 }), "bytes")
    expect(domain[0]).toBe(0)
    expect(evaluate(domain[1], 5)).toBe(90)
  })
})
