import { describe, expect, it } from "vitest"
import { panelQueryDomain, panelTimeDomain, panelTimeLabel, panelTimeOverrideError, panelTimeShiftError, panelTimeShiftSeconds, panelTimeTokenSeconds, shiftQueryDomain } from "@/lib/workspace-time-override"
import type { DashboardPanel } from "@/types/metric-workspace"

const now = Date.parse("2026-09-13T01:00:00.000Z")

function panel(time_from?: string, time_to?: string): DashboardPanel {
  return { id: "cpu", title: "CPU", type: "timeseries", size: "medium", legend: true, thresholds: [], series: [], time_from, time_to }
}

describe("panel time override", () => {
  it("resolves relative tokens to a window ending at now", () => {
    expect(panelTimeDomain(panel("now-30m"), now)).toEqual({ from: "2026-09-13T00:30:00.000Z", to: "2026-09-13T01:00:00.000Z" })
    expect(panelTimeDomain(panel("now-6h", "now-5h"), now)).toEqual({ from: "2026-09-12T19:00:00.000Z", to: "2026-09-12T20:00:00.000Z" })
  })

  it("leaves the dashboard range in place when the override is absent or invalid", () => {
    expect(panelTimeDomain(panel(), now)).toBeNull()
    expect(panelTimeDomain(panel("now-30s"), now)).toBeNull()
  })

  it("rejects malformed and out-of-bounds tokens", () => {
    const invalid = ["", "now-", "now-6", "30m", "now-6w", "now-12345m"]
    for (const token of invalid) expect(panelTimeTokenSeconds(token)).toBeNull()
    expect(panelTimeTokenSeconds("now")).toBe(0)
    expect(panelTimeTokenSeconds("now-2d")).toBe(172800)
  })

  it("reports the window error for end-only, reversed and unbounded spans", () => {
    expect(panelTimeOverrideError(panel())).toBeNull()
    expect(panelTimeOverrideError(panel("now-1m"))).toBeNull()
    expect(panelTimeOverrideError(panel("now-31d"))).toBeNull()
    expect(panelTimeOverrideError(panel(undefined, "now"))).toBe("workspace.time_override_invalid")
    expect(panelTimeOverrideError(panel("now-1h", "now-2h"))).toBe("workspace.time_override_invalid")
    expect(panelTimeOverrideError(panel("now-40d"))).toBe("workspace.time_override_invalid")
  })

  it("labels the active override and shift with the tokens the author typed", () => {
    expect(panelTimeLabel(panel())).toBeNull()
    expect(panelTimeLabel(panel("now-6h"))).toBe("now-6h")
    expect(panelTimeLabel(panel("now-6h", "now"))).toBe("now-6h → now")
    expect(panelTimeLabel({ ...panel(), time_shift: "1d" })).toBe("↩ 1d")
    expect(panelTimeLabel({ ...panel("now-6h"), time_shift: "12h" })).toBe("now-6h · ↩ 12h")
  })

  it("shifts the resolved window back by the declared duration", () => {
    const base = { from: "2026-09-13T00:00:00.000Z", to: "2026-09-13T01:00:00.000Z" }
    expect(shiftQueryDomain(base, 86400)).toEqual({ from: "2026-09-12T00:00:00.000Z", to: "2026-09-12T01:00:00.000Z" })
    expect(panelQueryDomain(panel(), base, now)).toEqual(base)
    expect(panelQueryDomain(panel(undefined, undefined), base, now)).toEqual(base)
    expect(panelQueryDomain({ ...panel(), time_shift: "1d" }, base, now)).toEqual({ from: "2026-09-12T00:00:00.000Z", to: "2026-09-12T01:00:00.000Z" })
    expect(panelQueryDomain({ ...panel("now-30m"), time_shift: "1h" }, base, now))
      .toEqual({ from: "2026-09-12T23:30:00.000Z", to: "2026-09-13T00:00:00.000Z" })
    expect(panelQueryDomain({ ...panel("now-30m"), time_shift: "1w" }, base, now))
      .toEqual({ from: "2026-09-13T00:30:00.000Z", to: "2026-09-13T01:00:00.000Z" })
  })

  it("accepts only bounded durations for the time shift", () => {
    expect(panelTimeShiftSeconds({ ...panel() })).toBeNull()
    expect(panelTimeShiftSeconds({ ...panel(), time_shift: "1d" })).toBe(86400)
    expect(panelTimeShiftSeconds({ ...panel(), time_shift: "30s" })).toBe(30)
    expect(panelTimeShiftSeconds({ ...panel(), time_shift: "31d" })).toBe(2678400)
    for (const value of ["0s", "1w", "32d", "now-1d", "1", "-1d", "99999m"]) {
      expect(panelTimeShiftSeconds({ ...panel(), time_shift: value })).toBeNull()
    }
    expect(panelTimeShiftError({ ...panel() })).toBeNull()
    expect(panelTimeShiftError({ ...panel(), time_shift: "1d" })).toBeNull()
    expect(panelTimeShiftError({ ...panel(), time_shift: "0s" })).toBe("workspace.time_shift_invalid")
    expect(panelTimeShiftError({ ...panel(), time_shift: "8d" })).toBeNull()
  })
})
