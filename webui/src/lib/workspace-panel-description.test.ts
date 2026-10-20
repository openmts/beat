import { describe, expect, it } from "vitest"
import { maximumPanelDescriptionBytes, panelDescriptionError, validPanelDescription } from "@/lib/workspace-panel-description"
import type { DashboardPanel } from "@/types/metric-workspace"

const panel = (patch: Partial<DashboardPanel>): DashboardPanel => ({ id: "p1", title: "CPU", type: "stat", size: "medium", legend: false, thresholds: [], series: [], ...patch })

describe("panel description", () => {
  it("accepts an absent, empty or filled description", () => {
    expect(panelDescriptionError(panel({}))).toBeNull()
    expect(panelDescriptionError(panel({ description: "" }))).toBeNull()
    expect(panelDescriptionError(panel({ description: "当前主机的 CPU 使用率" }))).toBeNull()
    expect(panelDescriptionError(panel({ description: "x".repeat(maximumPanelDescriptionBytes) }))).toBeNull()
  })

  it("rejects over-long and control-character descriptions", () => {
    expect(panelDescriptionError(panel({ description: "x".repeat(maximumPanelDescriptionBytes + 1) }))).toBe("workspace.panel_description_invalid")
    expect(panelDescriptionError(panel({ description: "汉".repeat(171) }))).toBe("workspace.panel_description_invalid")
    expect(panelDescriptionError(panel({ description: "bell\u0007" }))).toBe("workspace.panel_description_invalid")
    expect(panelDescriptionError(panel({ description: "nul\u0000" }))).toBe("workspace.panel_description_invalid")
  })

  it("counts bytes rather than code points and keeps newlines", () => {
    expect(validPanelDescription("汉".repeat(170))).toBe(true)
    expect(validPanelDescription("line\nbreak\ttab")).toBe(true)
    expect(validPanelDescription("carriage\rreturn")).toBe(true)
  })
})
