import { describe, expect, it } from "vitest"
import { barGaugeError, barGaugeFraction, barGaugeLitSegments, barGaugePatch, barGaugeSegments, panelBarGauge, resolvedBarOrientation } from "./workspace-bar-gauge"

describe("panel bar gauge", () => {
  it("falls back to the documented defaults", () => {
    expect(panelBarGauge({ type: "bargauge" })).toEqual({ orientation: "auto", display: "basic", unfilled: true })
    expect(panelBarGauge({ type: "bargauge", bar_orientation: "diagonal", bar_display: "neon", bar_unfilled: false }))
      .toEqual({ orientation: "auto", display: "basic", unfilled: false })
  })

  it("keeps the resolved values when they are documented", () => {
    expect(panelBarGauge({ type: "bargauge", bar_orientation: "horizontal", bar_display: "lcd", bar_unfilled: true }))
      .toEqual({ orientation: "horizontal", display: "lcd", unfilled: true })
  })

  it("mirrors the server enums and the bar-gauge-only rule", () => {
    expect(barGaugeError({ type: "bargauge" })).toBeNull()
    expect(barGaugeError({ type: "bargauge", bar_orientation: "horizontal", bar_display: "gradient", bar_unfilled: false })).toBeNull()
    expect(barGaugeError({ type: "bargauge", bar_orientation: "diagonal" })).toBe("workspace.bar_orientation_invalid")
    expect(barGaugeError({ type: "bargauge", bar_display: "neon" })).toBe("workspace.bar_display_invalid")
    expect(barGaugeError({ type: "bargauge", bar_unfilled: "yes" as unknown as boolean })).toBe("workspace.bar_unfilled_invalid")
    expect(barGaugeError({ type: "stat", bar_display: "basic" })).toBe("workspace.bar_gauge_bargauge_only")
    expect(barGaugeError({ type: "stat", bar_unfilled: false })).toBe("workspace.bar_gauge_bargauge_only")
    expect(barGaugeError({ type: "stat" })).toBeNull()
  })

  it("resolves auto orientation from the series count", () => {
    expect(resolvedBarOrientation({ type: "bargauge" }, 1)).toBe("vertical")
    expect(resolvedBarOrientation({ type: "bargauge" }, 4)).toBe("horizontal")
    expect(resolvedBarOrientation({ type: "bargauge", bar_orientation: "horizontal" }, 1)).toBe("horizontal")
  })

  it("clamps the fill fraction to the configured scale", () => {
    expect(barGaugeFraction(0, 100, 40)).toBeCloseTo(0.4)
    expect(barGaugeFraction(0, 100, null)).toBe(0)
    expect(barGaugeFraction(0, 100, -5)).toBe(0)
    expect(barGaugeFraction(0, 100, 250)).toBe(1)
    expect(barGaugeFraction(0, 100, Number.NaN)).toBe(0)
    expect(barGaugeFraction(100, 100, 100)).toBe(0)
  })

  it("lights a bounded number of LCD segments", () => {
    expect(barGaugeLitSegments(0)).toBe(0)
    expect(barGaugeLitSegments(-1)).toBe(0)
    expect(barGaugeLitSegments(0.01)).toBe(1)
    expect(barGaugeLitSegments(0.5)).toBe(barGaugeSegments / 2)
    expect(barGaugeLitSegments(1)).toBe(barGaugeSegments)
    expect(barGaugeLitSegments(4)).toBe(barGaugeSegments)
  })

  it("clears the bar fields only when the panel stops being a bar gauge", () => {
    expect(barGaugePatch("bargauge")).toEqual({})
    expect(barGaugePatch("gauge")).toEqual({ bar_orientation: undefined, bar_display: undefined, bar_unfilled: undefined })
  })
})
