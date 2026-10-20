import { describe, expect, it } from "vitest"
import {
  appendedOverrideThreshold, emptyOverride, fillBelowStackPatch, legendHidden, maximumOverrideFillBelowBytes,
  maximumOverrideThresholds, maximumOverrideUnitBytes, maximumPanelNoValueBytes, maximumPanelOverrides, noValueError,
  noValuePanelTypes, noValuePatch, overrideColor, overrideError, overrideLimitPanelTypes, overrideMatcherTypeList,
  overrideStandardPanelTypes,
  overrideThresholdPanelTypes, overrideTypePatch, seriesOverride, switchOverrideMatcherType, workspaceNoValue,
  workspaceSeriesAxisPlacement, workspaceSeriesColor, workspaceSeriesDecimals, workspaceSeriesFillBelowTo,
  workspaceSeriesFillOpacity, workspaceSeriesLimits, workspaceSeriesLineWidth, workspaceSeriesName,
  workspaceSeriesThresholdColor, workspaceSeriesThresholds, workspaceSeriesUnit, workspaceSideAxisOptions,
} from "@/lib/workspace-overrides"
import { formatWorkspaceValue } from "@/lib/workspace-format"
import type { DashboardOverride, DashboardPanel, MetricResultSeries, PanelColor } from "@/types/metric-workspace"

function series(label: string, unit = "percent", refId = "A"): MetricResultSeries {
  return {
    ref_id: refId, node_id: "node-1", metric: "cpu", aggregation: "avg", label, unit,
    available_from: null, state: "ok", points: [{ timestamp: 1_700_000_000_000, value: 1 }],
  }
}

function panel(...overrides: DashboardOverride[]): Pick<DashboardPanel, "overrides"> {
  return overrides.length ? { overrides } : {}
}

describe("workspace field overrides", () => {
  it("matches an exact series name and a regular expression", () => {
    const exact = panel({ matcher: { type: "by_name", value: "CPU usage" }, color: "rose" })
    expect(workspaceSeriesColor(exact, series("CPU usage"))).toBe("#e11d48")
    expect(workspaceSeriesColor(exact, series("CPU usage total"))).toBe("#3b82f6")
    const regex = panel({ matcher: { type: "by_regex", value: "^node-" }, color: "green" })
    expect(workspaceSeriesColor(regex, series("node-a"))).toBe("#16a34a")
    expect(workspaceSeriesColor(regex, series("db"))).toBe("#3b82f6")
  })

  it("resolves the value axis per series: override first, then the panel placement", () => {
    expect(workspaceSeriesAxisPlacement(undefined, series("CPU usage"))).toBe("left")
    expect(workspaceSeriesAxisPlacement({ overrides: [], axis_placement: "right" }, series("CPU usage"))).toBe("right")
    const moved = { overrides: [{ matcher: { type: "by_name", value: "CPU usage" }, axis_placement: "left" } as const], axis_placement: "right" as const }
    expect(workspaceSeriesAxisPlacement(moved, series("CPU usage"))).toBe("left")
    expect(workspaceSeriesAxisPlacement(moved, series("Memory usage"))).toBe("right")
  })

  it("rejects an unknown side and panels without a switchable value axis", () => {
    const matcher = { type: "by_name", value: "CPU usage" } as const
    expect(overrideError({ type: "timeseries", overrides: [{ matcher, axis_placement: "centre" as "left" }] })).toBe("workspace.override_axis_placement_invalid")
    expect(overrideError({ type: "table", overrides: [{ matcher, axis_placement: "right" }] })).toBe("workspace.override_axis_placement_unsupported")
    expect(overrideError({ type: "row", overrides: [{ matcher, axis_placement: "left" }] })).toBe("workspace.override_axis_placement_unsupported")
    // 轴位置是真实属性：只写它的覆盖不是空规则，两个可切换值轴的面板类型都接受它。
    expect(overrideError({ type: "timeseries", overrides: [{ matcher, axis_placement: "right" }] })).toBeNull()
    expect(overrideError({ type: "barchart", overrides: [{ matcher, axis_placement: "left" }] })).toBeNull()
    expect(overrideError(panel({ matcher, axis_placement: "right" }))).toBeNull()
  })

  it("drops the placement when the panel type cannot render a second axis", () => {
    const overrides: DashboardOverride[] = [{ matcher: { type: "by_name", value: "CPU usage" }, axis_placement: "right" }]
    expect(overrideTypePatch("timeseries", overrides)).toEqual({ overrides })
    expect(overrideTypePatch("barchart", overrides)).toEqual({ overrides })
    expect(overrideTypePatch("table", overrides)).toEqual({ overrides: [] })
    const mixed = [{ ...overrides[0], color: "rose" as const }]
    expect(overrideTypePatch("table", mixed)).toEqual({ overrides: [{ matcher: { type: "by_name", value: "CPU usage" }, color: "rose" }] })
  })

  it("rotates the default palette for derived series that declare no colour", () => {
    // 二元运算与 group_by 产出的序列没有声明颜色，按渲染顺序轮流取色，避免都落成同一个蓝。
    expect(workspaceSeriesColor(panel(), series("derived", "percent", "group_by_0"), undefined, 0)).toBe("#3b82f6")
    expect(workspaceSeriesColor(panel(), series("derived", "percent", "group_by_1"), undefined, 1)).toBe("#16a34a")
    expect(workspaceSeriesColor(panel(), series("derived", "percent", "group_by_7"), undefined, 7)).toBe("#3b82f6")
    // 面板里声明过颜色的序列仍然优先用声明的颜色，与序号无关。
    const declared = { series: [{ ref_id: "declared", label: "declared", node_id: "node-1", metric: "cpu", aggregation: "avg" as const, color: "rose" as const }] }
    expect(workspaceSeriesColor(declared, series("declared", "percent", "declared"), undefined, 3)).toBe("#e11d48")
  })

  it("keeps the series untouched when the pattern does not compile", () => {
    const broken = panel({ matcher: { type: "by_regex", value: "([" }, color: "rose" })
    expect(seriesOverride(broken, series("node-a"))).toEqual({})
    expect(workspaceSeriesColor(broken, series("node-a"))).toBe("#3b82f6")
  })

  it("stacks every matching override in order and lets the later one win", () => {
    const stacked = panel(
      { matcher: { type: "by_regex", value: "." }, color: "rose", unit: "bytes" },
      { matcher: { type: "by_name", value: "node-a" }, color: "violet", hide_legend: true },
    )
    expect(seriesOverride(stacked, series("node-a"))).toEqual({ color: "violet", unit: "bytes", hideLegend: true })
    expect(seriesOverride(stacked, series("node-b"))).toEqual({ color: "rose", unit: "bytes" })
    expect(legendHidden(stacked, series("node-a"))).toBe(true)
    expect(legendHidden(stacked, series("node-b"))).toBe(false)
  })

  it("resolves the override unit, colour and legend flag for a single series", () => {
    const one = panel({ matcher: { type: "by_name", value: "CPU usage" }, unit: "bytes_per_second" })
    expect(workspaceSeriesUnit(one, series("CPU usage"))).toBe("bytes_per_second")
    expect(workspaceSeriesUnit(one, series("Memory usage"))).toBe("percent")
    expect(overrideColor(one, series("CPU usage"))).toBeNull()
    expect(legendHidden(one, series("CPU usage"))).toBe(false)
    expect(seriesOverride(undefined, series("CPU usage"))).toEqual({})
  })

  it("resolves the per series line width and fill opacity with the panel value as fallback", () => {
    const overridden = panel({ matcher: { type: "by_name", value: "CPU usage" }, line_width: 4, fill_opacity: 0 })
    expect(workspaceSeriesLineWidth(overridden, series("CPU usage"), 2)).toBe(4)
    expect(workspaceSeriesFillOpacity(overridden, series("CPU usage"), 30)).toBe(0)
    expect(workspaceSeriesLineWidth(overridden, series("Memory usage"), 2)).toBe(2)
    expect(workspaceSeriesFillOpacity(overridden, series("Memory usage"), 30)).toBe(30)
    expect(workspaceSeriesLineWidth(undefined, series("CPU usage"), 1)).toBe(1)
    // 越界或非整数的覆盖值按未覆盖处理，避免把服务端会拒绝的值画到图上。
    const broken = panel({ matcher: { type: "by_name", value: "CPU usage" }, line_width: 9, fill_opacity: 12.5 })
    expect(workspaceSeriesLineWidth(broken, series("CPU usage"), 2)).toBe(2)
    expect(workspaceSeriesFillOpacity(broken, series("CPU usage"), 30)).toBe(30)
  })

  it("stacks the line style overrides so the later rule wins", () => {
    const stacked = panel(
      { matcher: { type: "by_regex", value: "." }, line_width: 1, fill_opacity: 10 },
      { matcher: { type: "by_name", value: "node-a" }, line_width: 3, fill_opacity: 0 },
    )
    expect(seriesOverride(stacked, series("node-a"))).toEqual({ lineWidth: 3, fillOpacity: 0 })
    expect(seriesOverride(stacked, series("node-b"))).toEqual({ lineWidth: 1, fillOpacity: 10 })
  })

  it("validates the override ranges and the panel type gate", () => {
    const matcher = { type: "by_name" as const, value: "CPU usage" }
    expect(overrideError({ type: "timeseries", overrides: [{ matcher, line_width: 4, fill_opacity: 0 }] })).toBeNull()
    expect(overrideError({ type: "barchart", overrides: [{ matcher, fill_opacity: 100 }] })).toBeNull()
    expect(overrideError({ type: "timeseries", overrides: [{ matcher, line_width: 0 }] })).toBe("workspace.override_line_width_invalid")
    expect(overrideError({ type: "timeseries", overrides: [{ matcher, line_width: 2.5 }] })).toBe("workspace.override_line_width_invalid")
    expect(overrideError({ type: "timeseries", overrides: [{ matcher, fill_opacity: 101 }] })).toBe("workspace.override_fill_opacity_invalid")
    expect(overrideError({ type: "barchart", overrides: [{ matcher, line_width: 2 }] })).toBe("workspace.override_line_style_unsupported")
    expect(overrideError({ type: "piechart", overrides: [{ matcher, fill_opacity: 20 }] })).toBe("workspace.override_line_style_unsupported")
    // 不给面板类型时不做类型门禁：调用点自己保证只解析它消费的属性。
    expect(overrideError(panel({ matcher, line_width: 2 }))).toBeNull()
  })

  it("drops the properties a new panel type cannot render when switching type", () => {
    const overrides: DashboardOverride[] = [
      { matcher: { type: "by_name", value: "a" }, color: "rose", line_width: 3, fill_opacity: 20 },
      { matcher: { type: "by_name", value: "b" }, line_width: 4 },
    ]
    expect(overrideTypePatch("timeseries", overrides)).toEqual({ overrides })
    // 柱状图保留填充、丢掉线宽；只剩线宽的那条覆盖整条移除。
    expect(overrideTypePatch("barchart", overrides)).toEqual({ overrides: [
      { matcher: { type: "by_name", value: "a" }, color: "rose", fill_opacity: 20 },
    ] })
    expect(overrideTypePatch("piechart", overrides)).toEqual({ overrides: [
      { matcher: { type: "by_name", value: "a" }, color: "rose" },
    ] })
    expect(overrideTypePatch("stat", undefined)).toEqual({})
  })

  it("builds a usable default and keeps the match value when the type changes", () => {
    expect(emptyOverride()).toEqual({ matcher: { type: "by_regex", value: "^node-" }, color: "green" })
    const switched = switchOverrideMatcherType(emptyOverride(), "by_name")
    expect(switched).toEqual({ matcher: { type: "by_name", value: "^node-" }, color: "green" })
    expect(switchOverrideMatcherType(emptyOverride(), "nonsense").matcher.type).toBe("by_name")
  })

  it("matches a series by its ref_id and lists the matcher for the editor", () => {
    expect(overrideMatcherTypeList).toEqual(["by_name", "by_regex", "by_ref_id", "by_type", "by_value"])
    const byRefId = panel({ matcher: { type: "by_ref_id", value: "A" }, color: "rose" })
    expect(workspaceSeriesColor(byRefId, series("CPU usage", "percent", "A"))).toBe("#e11d48")
    expect(workspaceSeriesColor(byRefId, series("CPU usage", "percent", "B"))).toBe("#3b82f6")
    expect(overrideError({ type: "table", overrides: [{ matcher: { type: "by_ref_id", value: "A" }, color: "rose" }] })).toBeNull()
  })

  it("prefers the per series threshold band over the panel thresholds", () => {
    const band: { value: number; color: PanelColor }[] = [{ value: 0, color: "blue" }, { value: 50, color: "amber" }]
    const declared: DashboardPanel["series"] = [{ ref_id: "A", node_id: "node-1", metric: "cpu", aggregation: "avg", color: "green", label: "CPU usage" }]
    const withOverrides = {
      overrides: [{ matcher: { type: "by_name" as const, value: "CPU usage" }, thresholds: band }],
      thresholds: [{ value: 80, color: "rose" as const }], series: declared, type: "stat",
    }
    expect(workspaceSeriesThresholds(withOverrides, series("CPU usage"))).toEqual(band)
    expect(workspaceSeriesThresholds({ ...withOverrides, overrides: [] }, series("CPU usage"))).toEqual([{ value: 80, color: "rose" }])
    expect(workspaceSeriesThresholds(undefined, undefined)).toEqual([])
    expect(workspaceSeriesThresholds({ thresholds: [{ value: 10, color: "cyan" }] }, undefined)).toEqual([{ value: 10, color: "cyan" }])
    // 覆盖阈值优先：0 起步的蓝 + 50 起的琥珀；没有覆盖时回落到面板阈值。
    expect(workspaceSeriesThresholdColor(withOverrides, series("CPU usage"), 60)).toBe("#d97706")
    expect(workspaceSeriesThresholdColor(withOverrides, series("CPU usage"), 10)).toBe("#3b82f6")
    expect(workspaceSeriesThresholdColor(withOverrides, series("CPU usage"), 90)).toBe("#d97706")
    expect(workspaceSeriesThresholdColor({ ...withOverrides, overrides: [] }, series("CPU usage"), 90)).toBe("#e11d48")
    // 没有数值、或阈值取色被关闭时回到序列自身声明的颜色。
    expect(workspaceSeriesThresholdColor(withOverrides, series("CPU usage"), null)).toBe("#16a34a")
    expect(workspaceSeriesThresholdColor({ ...withOverrides, threshold_mode: "off" }, series("CPU usage"), 90)).toBe("#16a34a")
    expect(workspaceSeriesThresholdColor(undefined, series("CPU usage"), 90)).toBe("#3b82f6")
  })

  it("validates the override thresholds against the panel type", () => {
    const matcher = { type: "by_ref_id" as const, value: "A" }
    const band = [{ value: 0, color: "green" as const }, { value: 90, color: "rose" as const }]
    for (const type of overrideThresholdPanelTypes) expect(overrideError({ type, overrides: [{ matcher, thresholds: band }] })).toBeNull()
    // 饼图与折线只按序列色渲染，带阈值只会保存出渲染器忽略的配置。
    expect(overrideThresholdPanelTypes).not.toContain("piechart")
    expect(overrideError({ type: "piechart", overrides: [{ matcher, thresholds: band }] })).toBe("workspace.override_thresholds_unsupported")
    expect(overrideError({ type: "timeseries", overrides: [{ matcher, thresholds: band }] })).toBe("workspace.override_thresholds_unsupported")
    expect(overrideError({ overrides: [{ matcher, thresholds: band }] })).toBeNull()
    const tooMany = Array.from({ length: maximumOverrideThresholds + 1 }, (_, index) => ({ value: index * 10, color: "green" as const }))
    expect(overrideError({ type: "stat", overrides: [{ matcher, thresholds: tooMany }] })).toBe("workspace.override_thresholds_invalid")
    expect(overrideError({ type: "stat", overrides: [{ matcher, thresholds: [{ value: 90, color: "rose" }, { value: 10, color: "green" }] }] }))
      .toBe("workspace.override_thresholds_invalid")
    expect(overrideError({ type: "stat", overrides: [{ matcher, thresholds: [{ value: 10, color: "chartreuse" as "green" }] }] }))
      .toBe("workspace.override_thresholds_invalid")
    expect(overrideError({ type: "stat", overrides: [{ matcher, thresholds: [{ value: Number.NaN, color: "green" }] }] }))
      .toBe("workspace.override_thresholds_invalid")
    expect(overrideError({ type: "stat", overrides: [{ matcher, thresholds: [] }] })).toBe("workspace.override_empty")
  })

  it("drops the per series thresholds when the new panel type cannot colour by threshold", () => {
    const overrides: DashboardOverride[] = [{ matcher: { type: "by_ref_id", value: "A" }, thresholds: [{ value: 0, color: "green" }] }]
    expect(overrideTypePatch("stat", overrides)).toEqual({ overrides })
    // 折线丢掉阈值：这条覆盖只剩阈值，整条移除。
    expect(overrideTypePatch("timeseries", overrides)).toEqual({ overrides: [] })
    const mixed = [{ ...overrides[0], color: "rose" as const }]
    expect(overrideTypePatch("timeseries", mixed)).toEqual({ overrides: [{ matcher: { type: "by_ref_id", value: "A" }, color: "rose" }] })
  })

  it("appends a usable default threshold to the per series band", () => {
    expect(appendedOverrideThreshold(undefined)).toEqual([{ value: 80, color: "amber" }])
    expect(appendedOverrideThreshold([{ value: 90, color: "rose" }])).toEqual([{ value: 90, color: "rose" }, { value: 100, color: "amber" }])
  })

  it("validates the fill below target against the panel type and stacking", () => {
    const matcher = { type: "by_ref_id" as const, value: "A" }
    const band = { type: "timeseries", overrides: [{ matcher, fill_below_to: "CPU max" }] }
    expect(overrideError(band)).toBeNull()
    expect(overrideError({ ...band, stack: "none" })).toBeNull()
    expect(overrideError({ ...band, stack: "normal" })).toBe("workspace.override_fill_below_stacked")
    expect(overrideError({ ...band, stack: "percent" })).toBe("workspace.override_fill_below_stacked")
    expect(overrideError({ ...band, type: "barchart" })).toBe("workspace.override_fill_below_unsupported")
    expect(overrideError({ overrides: [{ matcher, fill_below_to: "CPU max" }] })).toBeNull()
    expect(overrideError({ ...band, overrides: [{ matcher, fill_below_to: "  " }] })).toBe("workspace.override_fill_below_invalid")
    expect(overrideError({ ...band, overrides: [{ matcher, fill_below_to: "CPU\u0001max" }] })).toBe("workspace.override_fill_below_invalid")
    expect(overrideError({ ...band, overrides: [{ matcher, fill_below_to: "a".repeat(maximumOverrideFillBelowBytes + 1) }] }))
      .toBe("workspace.override_fill_below_invalid")
    // 只写填充目标的覆盖是真实属性，不是空规则。
    expect(overrideError({ ...band, overrides: [{ matcher, fill_below_to: "CPU max" }] })).toBeNull()
  })

  it("resolves the fill below target per series and drops it on stacking or type switches", () => {
    const overrides: DashboardOverride[] = [
      { matcher: { type: "by_ref_id", value: "A" }, fill_below_to: "CPU max" },
      { matcher: { type: "by_ref_id", value: "B" }, color: "rose", fill_below_to: "CPU max" },
    ]
    expect(workspaceSeriesFillBelowTo({ overrides }, series("CPU min"))).toBe("CPU max")
    expect(workspaceSeriesFillBelowTo({ overrides }, series("Gateways", "percent", "C"))).toBeUndefined()
    expect(overrideTypePatch("timeseries", overrides)).toEqual({ overrides })
    // 柱状图没有填充带：折线以外整条丢掉只带填充目标的覆盖。
    expect(overrideTypePatch("barchart", overrides)).toEqual({ overrides: [
      { matcher: { type: "by_ref_id", value: "B" }, color: "rose" },
    ] })
    // 打开堆叠时同样清理：堆叠面积与填充带不能同时渲染。
    expect(fillBelowStackPatch("none", overrides)).toEqual({})
    expect(fillBelowStackPatch("normal", overrides)).toEqual({ overrides: [
      { matcher: { type: "by_ref_id", value: "B" }, color: "rose" },
    ] })
    expect(fillBelowStackPatch("percent", [{ matcher: { type: "by_ref_id", value: "A" }, color: "rose" }])).toEqual({})
    expect(fillBelowStackPatch("normal", undefined)).toEqual({})
  })

  it("resolves the per side axis label and soft limits so the later override wins", () => {
    const overrides = panel(
      { matcher: { type: "by_name", value: "CPU usage" }, axis_label: "CPU", axis_soft_min: 0, axis_soft_max: 100 },
      { matcher: { type: "by_regex", value: "^CPU" }, axis_label: "CPU %" },
    )
    const left = workspaceSideAxisOptions({ ...overrides, type: "timeseries" }, [series("CPU usage")])
    expect(left).toEqual({ label: "CPU %", softMin: 0, softMax: 100 })
    // 没有命中的轴拿不到任何覆盖，调用方继续沿用面板级设置。
    expect(workspaceSideAxisOptions({ ...overrides, type: "timeseries" }, [series("Memory usage")])).toEqual({})
  })

  it("validates the per series axis options and clears them on other panel types", () => {
    const invalid = panel({ matcher: { type: "by_name", value: "CPU usage" }, axis_soft_min: 90, axis_soft_max: 10 })
    expect(overrideError({ ...invalid, type: "timeseries" })).toBe("workspace.soft_axis_order")
    const hard = panel({ matcher: { type: "by_name", value: "CPU usage" }, axis_soft_min: 0 })
    expect(overrideError({ ...hard, type: "timeseries", min: 0 })).toBe("workspace.soft_axis_conflict")
    const label = panel({ matcher: { type: "by_name", value: "CPU usage" }, axis_label: "a".repeat(41) })
    expect(overrideError({ ...label, type: "timeseries" })).toBe("workspace.axis_label_invalid")
    const stat = panel({ matcher: { type: "by_name", value: "CPU usage" }, axis_label: "CPU" })
    expect(overrideError({ ...stat, type: "stat" })).toBe("workspace.override_axis_placement_unsupported")
    expect(overrideTypePatch("stat", [{ matcher: { type: "by_name", value: "CPU usage" }, axis_label: "CPU", axis_soft_min: 0 }]))
      .toEqual({ overrides: [] })
  })

  it("mirrors the server rules when validating the editor payload", () => {
    expect(overrideError(panel())).toBeNull()
    expect(overrideError(panel({ matcher: { type: "by_name", value: "CPU usage" }, color: "rose", unit: "percent", hide_legend: false }))).toBeNull()
    expect(overrideError(panel(...Array.from({ length: maximumPanelOverrides + 1 }, () => ({ matcher: { type: "by_name" as const, value: "a" }, color: "rose" as const })))))
      .toBe("workspace.overrides_too_many")
    expect(overrideError(panel({ matcher: { type: "by_name", value: "  " }, color: "rose" }))).toBe("workspace.override_matcher_invalid")
    expect(overrideError(panel({ matcher: { type: "by_name", value: "a".repeat(129) }, color: "rose" }))).toBe("workspace.override_matcher_invalid")
    expect(overrideError(panel({ matcher: { type: "by_regex", value: "([" }, color: "rose" }))).toBe("workspace.override_matcher_invalid")
    expect(overrideError(panel({ matcher: { type: "by_label" as "by_name", value: "a" }, color: "rose" }))).toBe("workspace.override_matcher_invalid")
    expect(overrideError(panel({ matcher: { type: "by_name", value: "a" }, color: "chartreuse" as "rose" }))).toBe("workspace.override_color_invalid")
    expect(overrideError(panel({ matcher: { type: "by_name", value: "a" }, unit: "u".repeat(maximumOverrideUnitBytes + 1) }))).toBe("workspace.override_unit_invalid")
    expect(overrideError(panel({ matcher: { type: "by_name", value: "a" } }))).toBe("workspace.override_empty")
  })

  it("matches a series by its unit family and by its latest sample", () => {
    const typed = panel({ matcher: { type: "by_type", value: "percent" }, color: "rose" })
    expect(workspaceSeriesColor(typed, series("CPU usage", "percent"))).toBe("#e11d48")
    expect(workspaceSeriesColor(typed, series("Memory", "bytes"))).toBe("#3b82f6")
    // 速率后缀先判定：bytes_per_second 落进速率族而不是字节族。
    expect(workspaceSeriesColor(panel({ matcher: { type: "by_type", value: "rate" }, color: "green" }), series("RX", "bytes_per_second"))).toBe("#16a34a")
    const valued = panel({ matcher: { type: "by_value", value: "1" }, color: "violet" })
    expect(workspaceSeriesColor(valued, series("CPU usage"))).toBe("#8b5cf6")
    const other = { ...series("Memory", "bytes", "B"), points: [{ timestamp: 1_700_000_000_000, value: 42 }] }
    expect(workspaceSeriesColor(valued, other)).toBe("#3b82f6")
  })

  it("rejects matcher values the renderer could never match", () => {
    expect(overrideError(panel({ matcher: { type: "by_type", value: "percent" }, color: "rose" }))).toBeNull()
    expect(overrideError(panel({ matcher: { type: "by_type", value: "gigabytes" }, color: "rose" }))).toBe("workspace.override_series_type_invalid")
    expect(overrideError(panel({ matcher: { type: "by_value", value: "0" }, color: "rose" }))).toBeNull()
    expect(overrideError(panel({ matcher: { type: "by_value", value: "zero" }, color: "rose" }))).toBe("workspace.override_value_invalid")
    expect(switchOverrideMatcherType(emptyOverride(), "by_type").matcher).toEqual({ type: "by_type", value: "percent" })
  })

  it("applies the standard options overrides and gates them by panel type", () => {
    const standard = { matcher: { type: "by_name", value: "CPU usage" } as const, display_name: "CPU", decimals: 1, min: 0, max: 90 }
    expect(seriesOverride({ type: "gauge", overrides: [standard] }, series("CPU usage")))
      .toEqual({ displayName: "CPU", decimals: 1, min: 0, max: 90 })
    expect(workspaceSeriesName({ type: "timeseries", overrides: [standard] }, series("CPU usage"))).toBe("CPU")
    expect(workspaceSeriesName(undefined, series("CPU usage"))).toBe("CPU usage")
    expect(workspaceSeriesDecimals({ decimals: 3, overrides: [standard] }, series("CPU usage"))).toBe(1)
    expect(workspaceSeriesDecimals({ decimals: 3, overrides: [] }, series("CPU usage"))).toBe(3)
    expect(workspaceSeriesLimits({ min: 5, max: 50, overrides: [standard] }, series("CPU usage"))).toEqual({ min: 0, max: 90 })
    // 覆盖只给一侧时不会与面板的另一侧组成空量程。
    expect(workspaceSeriesLimits({ max: 50, overrides: [{ matcher: standard.matcher, min: 80 }] }, series("CPU usage"))).toEqual({ min: 80, max: undefined })
    const labelled = { matcher: standard.matcher, display_name: "CPU", decimals: 1 }
    for (const type of overrideStandardPanelTypes) expect(overrideError({ type, overrides: [labelled] })).toBeNull()
    for (const type of overrideLimitPanelTypes) expect(overrideError({ type, overrides: [standard] })).toBeNull()
    expect(overrideError({ type: "table", overrides: [standard] })).toBe("workspace.override_limits_unsupported")
    expect(overrideError({ type: "heatmap", overrides: [standard] })).toBe("workspace.override_standard_unsupported")
    expect(overrideError({ type: "gauge", overrides: [{ ...standard, decimals: 7 }] })).toBe("workspace.override_decimals_invalid")
    expect(overrideError({ type: "gauge", overrides: [{ ...standard, min: 90 }] })).toBe("workspace.override_limits_invalid")
    expect(overrideError({ type: "gauge", overrides: [{ matcher: standard.matcher, display_name: "x".repeat(129) }] })).toBe("workspace.override_display_name_invalid")
    // 线宽与填充仍然按形状门禁，Standard options 不改变它们的适用面板。
    expect(overrideTypePatch("table", [standard])).toEqual({ overrides: [{ matcher: standard.matcher, display_name: "CPU", decimals: 1 }] })
    expect(overrideTypePatch("heatmap", [standard])).toEqual({ overrides: [] })
  })

  it("resolves the panel placeholder and validates the no value text", () => {
    expect(workspaceNoValue(undefined)).toBe("—")
    expect(workspaceNoValue({ no_value: "" })).toBe("—")
    expect(workspaceNoValue({ no_value: "n/a" })).toBe("n/a")
    expect(formatWorkspaceValue(null, "percent", { noValue: "n/a" })).toBe("n/a")
    expect(formatWorkspaceValue(50, "percent", { noValue: "n/a" })).toBe("50%")
    for (const type of noValuePanelTypes) expect(noValueError({ type, no_value: "n/a" })).toBeNull()
    expect(noValueError({ type: "timeseries", no_value: "n/a" })).toBe("workspace.no_value_unsupported")
    expect(noValueError({ type: "stat", no_value: "x".repeat(maximumPanelNoValueBytes + 1) })).toBe("workspace.no_value_invalid")
    expect(noValuePatch("stat")).toEqual({})
    expect(noValuePatch("timeseries")).toEqual({ no_value: undefined })
  })
})
