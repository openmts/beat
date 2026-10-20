import { describe, expect, it } from "vitest"
import { barChartError, barChartPatch, barRadiusCorners, defaultBarWidth, panelBarChart } from "./workspace-bar-chart"

describe("panel bar chart", () => {
  it("falls back to the documented defaults", () => {
    expect(panelBarChart({ type: "barchart" })).toEqual({ width: defaultBarWidth, radius: 0 })
    expect(panelBarChart({ type: "barchart", bar_width: 99, bar_radius: -1 })).toEqual({ width: defaultBarWidth, radius: 0 })
    expect(panelBarChart({ type: "barchart", bar_width: 20.5, bar_radius: 2.5 })).toEqual({ width: defaultBarWidth, radius: 0 })
  })

  it("keeps the resolved values when they are documented", () => {
    expect(panelBarChart({ type: "barchart", bar_width: 4, bar_radius: 0 })).toEqual({ width: 4, radius: 0 })
    expect(panelBarChart({ type: "barchart", bar_width: 40, bar_radius: 10 })).toEqual({ width: 40, radius: 10 })
  })

  it("mirrors the server bounds and the barchart-only rule", () => {
    expect(barChartError({ type: "barchart" })).toBeNull()
    expect(barChartError({ type: "barchart", bar_width: 4, bar_radius: 10 })).toBeNull()
    expect(barChartError({ type: "barchart", bar_width: 3 })).toBe("workspace.bar_width_invalid")
    expect(barChartError({ type: "barchart", bar_width: 41 })).toBe("workspace.bar_width_invalid")
    expect(barChartError({ type: "barchart", bar_radius: 11 })).toBe("workspace.bar_radius_invalid")
    expect(barChartError({ type: "timeseries", bar_width: 12 })).toBe("workspace.bar_chart_barchart_only")
    expect(barChartError({ type: "bargauge", bar_radius: 4 })).toBe("workspace.bar_chart_barchart_only")
    expect(barChartError({ type: "timeseries" })).toBeNull()
  })

  it("clears the fields on other panel types", () => {
    expect(barChartPatch("barchart")).toEqual({})
    expect(barChartPatch("timeseries")).toEqual({ bar_width: undefined, bar_radius: undefined })
  })

  it("rounds only the bar top", () => {
    expect(barRadiusCorners(0)).toEqual([0, 0, 0, 0])
    expect(barRadiusCorners(6)).toEqual([6, 6, 0, 0])
    expect(barRadiusCorners(99)).toEqual([0, 0, 0, 0])
  })
})
