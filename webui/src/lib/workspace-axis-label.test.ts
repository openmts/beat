import { describe, expect, it } from "vitest"
import { axisLabelError, axisLabelPatch, maximumAxisLabelBytes, validAxisLabel } from "@/lib/workspace-axis-label"
import type { DashboardPanel } from "@/types/metric-workspace"

const panel = (patch: Partial<DashboardPanel>): DashboardPanel => ({ id: "p1", title: "CPU", type: "timeseries", size: "medium", legend: true, thresholds: [], series: [], ...patch })

describe("axis label", () => {
  it("stays optional and mirrors the server rules", () => {
    expect(maximumAxisLabelBytes).toBe(40)
    expect(axisLabelError(panel({}))).toBeNull()
    expect(axisLabelError(panel({ axis_label: "" }))).toBeNull()
    expect(axisLabelError(panel({ axis_label: "CPU 使用率" }))).toBeNull()
    expect(axisLabelError(panel({ type: "barchart", axis_label: "bytes" }))).toBeNull()
    expect(axisLabelError(panel({ axis_label: "a".repeat(maximumAxisLabelBytes) }))).toBeNull()
    expect(axisLabelError(panel({ axis_label: "a".repeat(maximumAxisLabelBytes + 1) }))).toBe("workspace.axis_label_invalid")
    expect(axisLabelError(panel({ axis_label: "CPU\nusage" }))).toBe("workspace.axis_label_invalid")
    expect(axisLabelError(panel({ type: "stat", axis_label: "CPU" }))).toBe("workspace.axis_label_panels_only")
  })

  it("measures the label in UTF-8 bytes", () => {
    expect(validAxisLabel("已用内存")).toBe(true)
    // 14 个汉字是 42 字节，超过 40 字节上限；11 个汉字是 33 字节，仍然合法。
    expect(validAxisLabel("内存使用率上限提示文本说明内容")).toBe(false)
    expect(validAxisLabel("内存使用率上限提示文本")).toBe(true)
  })

  it("clears the field when the panel type cannot render it", () => {
    expect(axisLabelPatch("timeseries")).toEqual({})
    expect(axisLabelPatch("barchart")).toEqual({})
    expect(axisLabelPatch("stat")).toEqual({ axis_label: undefined })
    expect(axisLabelPatch("row")).toEqual({ axis_label: undefined })
  })
})
