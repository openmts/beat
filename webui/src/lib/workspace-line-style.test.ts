import { describe, expect, it } from "vitest"
import { connectNullsPatch, defaultLineWidth, gradientFillOpacity, gradientPatch, interpolationCurve, interpolationPatch, linePatternDash, linePatternPatch, lineStyleError, panelLineStyle, panelTypePatch, stackPatch, stackedFillOpacity } from "./workspace-line-style"

describe("panel line style", () => {
  it("falls back to the documented defaults", () => {
    expect(panelLineStyle({ type: "timeseries" })).toEqual({ lineWidth: defaultLineWidth, fillOpacity: 0, showPoints: "auto", pointSize: null, stack: "none", gradientMode: "none", interpolation: "linear", pattern: "solid", connectNulls: "none" })
    expect(panelLineStyle({ type: "timeseries", line_width: 99, fill_opacity: -1, show_points: "sometimes", point_size: 0, stack: "cumulative", gradient_mode: "scheme", line_interpolation: "cubic", line_pattern: "wavy", connect_nulls: "threshold" }))
      .toEqual({ lineWidth: defaultLineWidth, fillOpacity: 0, showPoints: "auto", pointSize: null, stack: "none", gradientMode: "none", interpolation: "linear", pattern: "solid", connectNulls: "none" })
  })

  it("keeps the resolved values when they are documented", () => {
    expect(panelLineStyle({ type: "timeseries", line_width: 4, fill_opacity: 25, show_points: "always", point_size: 6, stack: "normal", gradient_mode: "hue", line_interpolation: "step_after", line_pattern: "dashed", connect_nulls: "always" }))
      .toEqual({ lineWidth: 4, fillOpacity: 25, showPoints: "always", pointSize: 6, stack: "normal", gradientMode: "hue", interpolation: "step_after", pattern: "dashed", connectNulls: "always" })
  })

  it("mirrors the server bounds and the timeseries-only rule", () => {
    expect(lineStyleError({ type: "timeseries" })).toBeNull()
    expect(lineStyleError({ type: "timeseries", line_width: 0 })).toBe("workspace.line_width_invalid")
    expect(lineStyleError({ type: "timeseries", line_width: 5 })).toBe("workspace.line_width_invalid")
    expect(lineStyleError({ type: "timeseries", fill_opacity: 101 })).toBe("workspace.fill_opacity_invalid")
    expect(lineStyleError({ type: "timeseries", show_points: "sometimes" })).toBe("workspace.show_points_invalid")
    expect(lineStyleError({ type: "timeseries", stack: "cumulative" })).toBe("workspace.stack_invalid")
    expect(lineStyleError({ type: "timeseries", stack: "percent" })).toBeNull()
    expect(lineStyleError({ type: "timeseries", stack: "percent", scale: "log" })).toBe("workspace.stack_percent_linear_axis")
    expect(lineStyleError({ type: "timeseries", gradient_mode: "scheme" })).toBe("workspace.gradient_mode_invalid")
    expect(lineStyleError({ type: "timeseries", gradient_mode: "hue" })).toBe("workspace.gradient_requires_fill")
    expect(lineStyleError({ type: "timeseries", gradient_mode: "hue", fill_opacity: 0 })).toBe("workspace.gradient_requires_fill")
    expect(lineStyleError({ type: "timeseries", gradient_mode: "hue", fill_opacity: 30 })).toBeNull()
    expect(lineStyleError({ type: "barchart", gradient_mode: "opacity" })).toBeNull()
    expect(lineStyleError({ type: "timeseries", line_interpolation: "cubic" })).toBe("workspace.line_interpolation_invalid")
    expect(lineStyleError({ type: "timeseries", line_interpolation: "step_before" })).toBeNull()
    expect(lineStyleError({ type: "barchart", line_interpolation: "smooth" })).toBe("workspace.line_style_line_only")
    expect(lineStyleError({ type: "gauge", line_interpolation: "smooth" })).toBe("workspace.line_style_timeseries_only")
    expect(lineStyleError({ type: "gauge", line_width: 2 })).toBe("workspace.line_style_timeseries_only")
    expect(lineStyleError({ type: "gauge", gradient_mode: "hue" })).toBe("workspace.line_style_timeseries_only")
    expect(lineStyleError({ type: "gauge" })).toBeNull()
    expect(lineStyleError({ type: "timeseries", line_pattern: "solid" })).toBeNull()
    expect(lineStyleError({ type: "timeseries", line_pattern: "dashed" })).toBeNull()
    expect(lineStyleError({ type: "timeseries", line_pattern: "wavy" })).toBe("workspace.line_pattern_invalid")
    expect(lineStyleError({ type: "barchart", line_pattern: "dashed" })).toBe("workspace.line_style_line_only")
    expect(lineStyleError({ type: "gauge", line_pattern: "dotted" })).toBe("workspace.line_style_timeseries_only")
    expect(lineStyleError({ type: "timeseries", connect_nulls: "always" })).toBeNull()
    expect(lineStyleError({ type: "timeseries", point_size: 1 })).toBeNull()
    expect(lineStyleError({ type: "timeseries", point_size: 12 })).toBeNull()
    expect(lineStyleError({ type: "timeseries", point_size: 13 })).toBe("workspace.point_size_invalid")
    expect(lineStyleError({ type: "timeseries", point_size: 2.5 })).toBe("workspace.point_size_invalid")
    expect(lineStyleError({ type: "timeseries", point_size: 6, show_points: "never" })).toBe("workspace.point_size_requires_points")
    expect(lineStyleError({ type: "barchart", point_size: 6 })).toBe("workspace.line_style_line_only")
    expect(lineStyleError({ type: "gauge", point_size: 6 })).toBe("workspace.line_style_timeseries_only")
    expect(lineStyleError({ type: "timeseries", connect_nulls: "none" })).toBeNull()
    expect(lineStyleError({ type: "timeseries", connect_nulls: "threshold" })).toBe("workspace.connect_nulls_invalid")
    expect(lineStyleError({ type: "barchart", connect_nulls: "always" })).toBe("workspace.line_style_line_only")
    expect(lineStyleError({ type: "table", connect_nulls: "always" })).toBe("workspace.line_style_timeseries_only")
  })

  it("clears the null-handling field when it is the default", () => {
    expect(connectNullsPatch("none")).toEqual({ connect_nulls: undefined })
    expect(connectNullsPatch("threshold")).toEqual({ connect_nulls: undefined })
    expect(connectNullsPatch("always")).toEqual({ connect_nulls: "always" })
    expect(panelTypePatch("barchart", { type: "timeseries", connect_nulls: "always" })).toMatchObject({ connect_nulls: undefined })
  })

  it("maps interpolation to the renderer curve and clears it off the line", () => {
    expect(interpolationPatch("linear")).toEqual({ line_interpolation: undefined })
    expect(interpolationPatch("cubic")).toEqual({ line_interpolation: undefined })
    expect(interpolationPatch("step_after")).toEqual({ line_interpolation: "step_after" })
    expect(interpolationCurve("linear")).toBe("linear")
    expect(interpolationCurve("smooth")).toBe("monotone")
    expect(interpolationCurve("step_before")).toBe("stepBefore")
    expect(interpolationCurve("step_after")).toBe("stepAfter")
    expect(panelTypePatch("barchart")).toMatchObject({ line_interpolation: undefined })
    expect(panelTypePatch("table")).toMatchObject({ line_interpolation: undefined })
  })

  it("maps the line pattern to a dash array and clears solid off the panel", () => {
    expect(linePatternPatch("solid")).toEqual({ line_pattern: undefined })
    expect(linePatternPatch("wavy")).toEqual({ line_pattern: undefined })
    expect(linePatternPatch("dashed")).toEqual({ line_pattern: "dashed" })
    expect(linePatternDash("solid")).toBeUndefined()
    expect(linePatternDash("dashed")).toBe("6 4")
    expect(linePatternDash("dotted")).toBe("1 3")
    expect(panelTypePatch("barchart", { type: "timeseries", line_pattern: "dashed" })).toMatchObject({ line_pattern: undefined })
  })

  it("clears line-only fields when the panel stops being a time series", () => {
    expect(panelTypePatch("timeseries")).toEqual({ type: "timeseries", bar_orientation: undefined, bar_display: undefined, bar_unfilled: undefined, pie_chart_type: undefined, pie_label_mode: undefined })
    expect(panelTypePatch("table")).toEqual({ type: "table", line_width: undefined, fill_opacity: undefined, show_points: undefined, stack: undefined, gradient_mode: undefined, bar_orientation: undefined, bar_display: undefined, bar_unfilled: undefined, pie_chart_type: undefined, pie_label_mode: undefined })
    expect(panelTypePatch("bargauge")).toEqual({ type: "bargauge", line_width: undefined, fill_opacity: undefined, show_points: undefined, stack: undefined, gradient_mode: undefined, pie_chart_type: undefined, pie_label_mode: undefined })
    expect(panelTypePatch("piechart")).toEqual({ type: "piechart", line_width: undefined, fill_opacity: undefined, show_points: undefined, stack: undefined, gradient_mode: undefined, bar_orientation: undefined, bar_display: undefined, bar_unfilled: undefined })
  })

  it("keeps the gradient only for panels that draw a fill", () => {
    expect(panelTypePatch("barchart", { type: "timeseries", fill_opacity: 20, stack: "normal", gradient_mode: "hue" }))
      .toEqual({ type: "barchart", line_width: undefined, show_points: undefined, fill_opacity: 20, stack: "normal", gradient_mode: "hue",
        bar_unfilled: undefined, pie_chart_type: undefined, pie_label_mode: undefined })
    expect(panelTypePatch("barchart", { type: "timeseries", gradient_mode: "none" })).toMatchObject({ gradient_mode: undefined })
  })

  it("offers a readable fill when the gradient is switched on", () => {
    expect(gradientPatch({ type: "timeseries" }, "none")).toEqual({ gradient_mode: undefined })
    expect(gradientPatch({ type: "timeseries" }, "scheme")).toEqual({ gradient_mode: undefined })
    expect(gradientPatch({ type: "timeseries" }, "hue")).toEqual({ gradient_mode: "hue", fill_opacity: gradientFillOpacity })
    expect(gradientPatch({ type: "timeseries", fill_opacity: 40 }, "opacity")).toEqual({ gradient_mode: "opacity" })
    expect(gradientPatch({ type: "barchart" }, "opacity")).toEqual({ gradient_mode: "opacity" })
  })

  it("offers a readable fill when stacking is switched on", () => {
    expect(stackPatch({ type: "timeseries" }, "none")).toEqual({ stack: undefined })
    expect(stackPatch({ type: "timeseries" }, "normal")).toEqual({ stack: "normal", fill_opacity: stackedFillOpacity })
    expect(stackPatch({ type: "timeseries", fill_opacity: 40 }, "normal")).toEqual({ stack: "normal" })
  })
})
