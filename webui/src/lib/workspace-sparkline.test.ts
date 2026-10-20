import { describe, expect, it } from "vitest"
import { defaultSparklineLineWidth, maxSparklineFillOpacity, panelSparkline, sparklineAreaPaths, sparklineError, sparklineGradientPatch, sparklinePatch, sparklineSegments, sparklineStyle } from "@/lib/workspace-sparkline"
import type { DashboardPanel, MetricResultSeries } from "@/types/metric-workspace"

const panel = (patch: Partial<DashboardPanel>): DashboardPanel => ({ id: "p1", title: "CPU", type: "stat", size: "medium", legend: false, thresholds: [], series: [], ...patch })
type Point = MetricResultSeries["points"][number]

describe("sparkline switch", () => {
  it("reads the switch only when it is explicitly on", () => {
    expect(panelSparkline(panel({ sparkline: true }))).toBe(true)
    expect(panelSparkline(panel({}))).toBe(false)
    expect(panelSparkline(panel({ sparkline: false }))).toBe(false)
  })

  it("only allows the switch on stat panels", () => {
    expect(sparklineError(panel({ sparkline: true }))).toBeNull()
    expect(sparklineError(panel({ sparkline: false }))).toBeNull()
    expect(sparklineError(panel({}))).toBeNull()
    expect(sparklineError(panel({ type: "timeseries", sparkline: true }))).toBe("workspace.sparkline_stat_only")
    expect(sparklineError(panel({ type: "gauge", sparkline: true }))).toBe("workspace.sparkline_stat_only")
  })

  it("clears the switch when the panel type changes", () => {
    expect(sparklinePatch("stat")).toEqual({})
    expect(sparklinePatch("gauge")).toEqual({ sparkline: undefined })
    expect(sparklinePatch("text")).toEqual({ sparkline: undefined })
  })
})

describe("sparkline geometry", () => {
  const sample = (points: Point[], width?: number, height?: number) => sparklineSegments(points, width, height)

  it("scales the series across the canvas and breaks the line on gaps", () => {
    expect(sample([{ timestamp: 0, value: 0 }, { timestamp: 1000, value: 10 }, { timestamp: 2000, value: null }, { timestamp: 3000, value: 20 }, { timestamp: 4000, value: 30 }]))
      .toEqual(["M 0.00 32.00 L 40.00 21.33", "M 120.00 10.67 L 160.00 0.00"])
  })

  it("draws a flat series on the midline", () => {
    expect(sample([{ timestamp: 0, value: 5 }, { timestamp: 1000, value: 5 }, { timestamp: 2000, value: 5 }]))
      .toEqual(["M 0.00 16.00 L 80.00 16.00 L 160.00 16.00"])
  })

  it("returns nothing when fewer than two samples are readable", () => {
    expect(sample([])).toEqual([])
    expect(sample([{ timestamp: 0, value: 3 }])).toEqual([])
    expect(sample([{ timestamp: 0, value: 3 }, { timestamp: 1000, value: null }])).toEqual([])
    expect(sample([{ timestamp: 0, value: null }, { timestamp: 1000, value: 4 }, { timestamp: 2000, value: null }])).toEqual([])
  })

  it("honours a custom canvas size", () => {
    expect(sample([{ timestamp: 0, value: 0 }, { timestamp: 1000, value: 10 }], 20, 10))
      .toEqual(["M 0.00 10.00 L 20.00 0.00"])
  })
})

describe("sparkline line width and fill", () => {
  it("falls back to the defaults when the panel leaves them out or goes out of range", () => {
    expect(sparklineStyle(panel({ sparkline: true }))).toEqual({ lineWidth: defaultSparklineLineWidth, fillOpacity: 0, gradientMode: "none" })
    expect(sparklineStyle(panel({ sparkline: true, sparkline_line_width: 99, sparkline_fill_opacity: -5 })))
      .toEqual({ lineWidth: defaultSparklineLineWidth, fillOpacity: 0, gradientMode: "none" })
    expect(sparklineStyle(panel({ sparkline: true, sparkline_line_width: 3.5 }))).toEqual({ lineWidth: defaultSparklineLineWidth, fillOpacity: 0, gradientMode: "none" })
  })

  it("reads the configured width and fill", () => {
    expect(sparklineStyle(panel({ sparkline: true, sparkline_line_width: 4, sparkline_fill_opacity: 40 })))
      .toEqual({ lineWidth: 4, fillOpacity: 40, gradientMode: "none" })
  })

  it("validates the two options like the server does", () => {
    expect(sparklineError(panel({ sparkline: true, sparkline_line_width: 1, sparkline_fill_opacity: maxSparklineFillOpacity }))).toBeNull()
    expect(sparklineError(panel({ sparkline: true, sparkline_line_width: 0 }))).toBe("workspace.line_width_invalid")
    expect(sparklineError(panel({ sparkline: true, sparkline_line_width: 5 }))).toBe("workspace.line_width_invalid")
    expect(sparklineError(panel({ sparkline: true, sparkline_fill_opacity: 101 }))).toBe("workspace.fill_opacity_invalid")
    expect(sparklineError(panel({ sparkline: true, sparkline_fill_opacity: -1 }))).toBe("workspace.fill_opacity_invalid")
  })

  it("requires the switch before the options count", () => {
    expect(sparklineError(panel({ sparkline_line_width: 3 }))).toBe("workspace.sparkline_options_require_on")
    expect(sparklineError(panel({ sparkline: false, sparkline_fill_opacity: 10 }))).toBe("workspace.sparkline_options_require_on")
    expect(sparklineError(panel({ type: "timeseries", sparkline_line_width: 3 }))).toBe("workspace.sparkline_stat_only")
    expect(sparklineError(panel({ sparkline: false }))).toBeNull()
  })

  it("clears the options together with the switch when the type changes", () => {
    expect(sparklinePatch("table")).toEqual({ sparkline: undefined, sparkline_line_width: undefined, sparkline_fill_opacity: undefined, sparkline_gradient_mode: undefined })
  })

  it("reads and validates the fill gradient like the server does", () => {
    expect(sparklineStyle(panel({ sparkline: true, sparkline_gradient_mode: "hue" })).gradientMode).toBe("hue")
    expect(sparklineStyle(panel({ sparkline: true, sparkline_gradient_mode: "scheme" })).gradientMode).toBe("none")
    expect(sparklineError(panel({ sparkline: true, sparkline_fill_opacity: 20, sparkline_gradient_mode: "opacity" }))).toBeNull()
    expect(sparklineError(panel({ sparkline: true, sparkline_fill_opacity: 20, sparkline_gradient_mode: "none" }))).toBeNull()
    expect(sparklineError(panel({ sparkline: true, sparkline_fill_opacity: 20, sparkline_gradient_mode: "scheme" }))).toBe("workspace.gradient_mode_invalid")
    expect(sparklineError(panel({ sparkline: true, sparkline_gradient_mode: "hue" }))).toBe("workspace.gradient_requires_fill")
    expect(sparklineError(panel({ sparkline: true, sparkline_fill_opacity: 0, sparkline_gradient_mode: "hue" }))).toBe("workspace.gradient_requires_fill")
    expect(sparklineError(panel({ sparkline: false, sparkline_gradient_mode: "hue" }))).toBe("workspace.sparkline_options_require_on")
    expect(sparklineError(panel({ type: "timeseries", sparkline: true, sparkline_gradient_mode: "hue" }))).toBe("workspace.sparkline_stat_only")
  })

  it("fills in a readable opacity when a gradient is picked without one", () => {
    expect(sparklineGradientPatch(panel({ sparkline: true }), "none")).toEqual({ sparkline_gradient_mode: undefined })
    expect(sparklineGradientPatch(panel({ sparkline: true }), "scheme")).toEqual({ sparkline_gradient_mode: undefined })
    expect(sparklineGradientPatch(panel({ sparkline: true }), "hue")).toEqual({ sparkline_gradient_mode: "hue", sparkline_fill_opacity: 20 })
    expect(sparklineGradientPatch(panel({ sparkline: true, sparkline_fill_opacity: 40 }), "opacity")).toEqual({ sparkline_gradient_mode: "opacity" })
  })
})

describe("sparkline fill paths", () => {
  it("closes every segment on the baseline", () => {
    expect(sparklineAreaPaths([{ timestamp: 0, value: 0 }, { timestamp: 1000, value: 10 }], 20, 10))
      .toEqual(["M 0.00 10.00 L 20.00 0.00 L 20.00 10.00 L 0.00 10.00 Z"])
  })

  it("returns nothing when there is no line to fill", () => {
    expect(sparklineAreaPaths([])).toEqual([])
    expect(sparklineAreaPaths([{ timestamp: 0, value: 1 }])).toEqual([])
  })
})

describe("sparkline time axis", () => {
  const sample = (points: Point[], width?: number, height?: number) => sparklineSegments(points, width, height)

  it("places uneven samples by their real timestamps", () => {
    expect(sample([{ timestamp: 0, value: 0 }, { timestamp: 1000, value: 10 }, { timestamp: 4000, value: 20 }]))
      .toEqual(["M 0.00 32.00 L 40.00 16.00 L 160.00 0.00"])
  })

  it("keeps a flat uneven series on the midline with time spacing", () => {
    expect(sample([{ timestamp: 100, value: 5 }, { timestamp: 1100, value: 5 }, { timestamp: 6100, value: 5 }], 20, 10))
      .toEqual(["M 0.00 5.00 L 3.33 5.00 L 20.00 5.00"])
  })

  it("falls back to equal index spacing when timestamps cannot span an axis", () => {
    expect(sample([{ timestamp: 700, value: 0 }, { timestamp: 700, value: 10 }]))
      .toEqual(["M 0.00 32.00 L 160.00 0.00"])
    expect(sample([{ timestamp: Number.NaN, value: 0 }, { timestamp: 1000, value: 10 }]))
      .toEqual(["M 0.00 32.00 L 160.00 0.00"])
  })

  it("fills the area under a time-positioned segment", () => {
    expect(sparklineAreaPaths([{ timestamp: 0, value: 0 }, { timestamp: 3000, value: 10 }], 30, 10))
      .toEqual(["M 0.00 10.00 L 30.00 0.00 L 30.00 10.00 L 0.00 10.00 Z"])
  })
})
