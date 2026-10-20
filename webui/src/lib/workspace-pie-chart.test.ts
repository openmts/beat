import { describe, expect, it } from "vitest"
import { panelPieChart, pieArcPath, pieChartError, pieChartPatch, pieSliceFractions, pieSliceStarts, pointOnCircle } from "./workspace-pie-chart"

describe("panel pie chart", () => {
  it("falls back to the documented defaults", () => {
    expect(panelPieChart({ type: "piechart" })).toEqual({ chartType: "pie", labelMode: "percent" })
    expect(panelPieChart({ type: "piechart", pie_chart_type: "radar", pie_label_mode: "legend" }))
      .toEqual({ chartType: "pie", labelMode: "percent" })
  })

  it("keeps documented values", () => {
    expect(panelPieChart({ type: "piechart", pie_chart_type: "donut", pie_label_mode: "both" }))
      .toEqual({ chartType: "donut", labelMode: "both" })
  })

  it("mirrors the server enums and the pie-chart-only rule", () => {
    expect(pieChartError({ type: "piechart" })).toBeNull()
    expect(pieChartError({ type: "piechart", pie_chart_type: "donut", pie_label_mode: "value" })).toBeNull()
    expect(pieChartError({ type: "piechart", pie_chart_type: "radar" })).toBe("workspace.pie_chart_type_invalid")
    expect(pieChartError({ type: "piechart", pie_label_mode: "legend" })).toBe("workspace.pie_label_mode_invalid")
    expect(pieChartError({ type: "bargauge", pie_chart_type: "pie" })).toBe("workspace.pie_chart_piechart_only")
    expect(pieChartError({ type: "stat", pie_label_mode: "both" })).toBe("workspace.pie_chart_piechart_only")
    expect(pieChartError({ type: "stat" })).toBeNull()
  })

  it("clears the pie fields only when the panel stops being a pie chart", () => {
    expect(pieChartPatch("piechart")).toEqual({})
    expect(pieChartPatch("table")).toEqual({ pie_chart_type: undefined, pie_label_mode: undefined })
  })

  it("turns latest values into shares of the positive total", () => {
    expect(pieSliceFractions([40, 60])).toEqual([0.4, 0.6])
    expect(pieSliceFractions([0, 90])).toEqual([0, 1])
    expect(pieSliceFractions([-5, null, Number.NaN, 30])).toEqual([0, 0, 0, 1])
    expect(pieSliceFractions([0, null])).toEqual([0, 0])
  })

  it("accumulates slice start offsets", () => {
    expect(pieSliceStarts([0.25, 0.25, 0.5])).toEqual([0, 0.25, 0.5])
  })

  it("renders solid wedges and donut rings", () => {
    const wedge = pieArcPath({ radius: 100, innerRadius: 0 }, 0, 0.25)
    expect(wedge.startsWith("M 100 100 L 100 0")).toBe(true)
    expect(wedge.endsWith("Z")).toBe(true)
    const ring = pieArcPath({ radius: 100, innerRadius: 50 }, 0.5, 1)
    expect(ring).toContain("L 150 100")
    expect(ring).toContain("A 50 50 0 0 0")
  })

  it("returns no path for empty slices and rounds circle points", () => {
    expect(pieArcPath({ radius: 100, innerRadius: 0 }, 0.4, 0.4)).toBe("")
    expect(pieArcPath({ radius: 100, innerRadius: 0 }, 0.6, 0.4)).toBe("")
    expect(pointOnCircle(10, 10, 0)).toEqual({ x: 10, y: 0 })
    expect(pointOnCircle(10, 10, 0.25)).toEqual({ x: 20, y: 10 })
  })
})
