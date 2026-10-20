import { describe, expect, it } from "vitest"
import { panelStatStyle, readableStatForeground, statAppearanceStyles, statOrientationLayout, statPatch, statStyleError, statStylePatch, statTextSizeClass } from "@/lib/workspace-stat"
import type { DashboardPanel } from "@/types/metric-workspace"

const panel = (patch: Partial<DashboardPanel>): DashboardPanel => ({ id: "p1", title: "CPU", type: "stat", size: "medium", legend: false, thresholds: [], series: [], ...patch })

describe("stat styles", () => {
  it("falls back to the documented defaults", () => {
    expect(panelStatStyle(panel({}))).toEqual({ colorMode: "value", textMode: "auto", textSize: "auto", orientation: "auto" })
    expect(panelStatStyle(panel({ stat_color_mode: "background", stat_text_mode: "value", stat_text_size: "xl", stat_orientation: "horizontal" })))
      .toEqual({ colorMode: "background", textMode: "value", textSize: "xl", orientation: "horizontal" })
    expect(panelStatStyle(panel({ stat_color_mode: "gradient" as never, stat_text_mode: "labels" as never, stat_text_size: "xxl" as never, stat_orientation: "diagonal" as never })))
      .toEqual({ colorMode: "value", textMode: "auto", textSize: "auto", orientation: "auto" })
    expect(panelStatStyle(panel({ stat_text_mode: "name" }))).toEqual({ colorMode: "value", textMode: "name", textSize: "auto", orientation: "auto" })
    expect(panelStatStyle(panel({ stat_text_mode: "none" }))).toEqual({ colorMode: "value", textMode: "none", textSize: "auto", orientation: "auto" })
  })

  it("maps the orientation onto a layout and keeps auto on the previous order", () => {
    expect(statOrientationLayout("auto")).toBe("stacked-below")
    expect(statOrientationLayout("vertical")).toBe("stacked-above")
    expect(statOrientationLayout("horizontal")).toBe("inline")
    expect(statOrientationLayout("diagonal" as never)).toBe("stacked-below")
  })

  it("maps every text size onto a font class and falls back to auto", () => {
    expect(statTextSizeClass("auto")).toBe("text-4xl")
    expect(statTextSizeClass("sm")).toBe("text-2xl")
    expect(statTextSizeClass("md")).toBe("text-3xl")
    expect(statTextSizeClass("lg")).toBe("text-4xl")
    expect(statTextSizeClass("xl")).toBe("text-5xl")
    expect(statTextSizeClass("xxl" as never)).toBe("text-4xl")
  })

  it("mirrors the server rules", () => {
    expect(statStyleError(panel({}))).toBeNull()
    expect(statStyleError(panel({ stat_color_mode: "none", stat_text_mode: "value" }))).toBeNull()
    expect(statStyleError(panel({ stat_color_mode: "gradient" as never }))).toBe("workspace.stat_color_mode_invalid")
    expect(statStyleError(panel({ stat_text_mode: "labels" as never }))).toBe("workspace.stat_text_mode_invalid")
    expect(statStyleError(panel({ stat_text_mode: "name" }))).toBeNull()
    expect(statStyleError(panel({ stat_text_mode: "none" }))).toBeNull()
    expect(statStyleError(panel({ stat_text_size: "sm" }))).toBeNull()
    expect(statStyleError(panel({ stat_text_size: "xxl" as never }))).toBe("workspace.stat_text_size_invalid")
    expect(statStyleError(panel({ stat_orientation: "vertical" }))).toBeNull()
    expect(statStyleError(panel({ stat_orientation: "diagonal" as never }))).toBe("workspace.stat_orientation_invalid")
    expect(statStyleError(panel({ type: "timeseries", stat_color_mode: "background" }))).toBe("workspace.stat_style_stat_only")
    expect(statStyleError(panel({ type: "gauge", stat_text_mode: "value" }))).toBe("workspace.stat_style_stat_only")
  })

  it("omits the fields when they are the default and clears them off the stat panel", () => {
    expect(statPatch("stat_color_mode", "value")).toEqual({ stat_color_mode: undefined })
    expect(statPatch("stat_color_mode", "background")).toEqual({ stat_color_mode: "background" })
    expect(statPatch("stat_text_mode", "auto")).toEqual({ stat_text_mode: undefined })
    expect(statPatch("stat_text_mode", "value")).toEqual({ stat_text_mode: "value" })
    expect(statPatch("stat_text_mode", "name")).toEqual({ stat_text_mode: "name" })
    expect(statPatch("stat_text_mode", "none")).toEqual({ stat_text_mode: "none" })
    expect(statPatch("stat_text_size", "auto")).toEqual({ stat_text_size: undefined })
    expect(statPatch("stat_text_size", "lg")).toEqual({ stat_text_size: "lg" })
    expect(statPatch("stat_text_size", "xxl")).toEqual({ stat_text_size: undefined })
    expect(statPatch("stat_color_mode", "gradient")).toEqual({ stat_color_mode: undefined })
    expect(statPatch("stat_orientation", "auto")).toEqual({ stat_orientation: undefined })
    expect(statPatch("stat_orientation", "horizontal")).toEqual({ stat_orientation: "horizontal" })
    expect(statPatch("stat_orientation", "diagonal")).toEqual({ stat_orientation: undefined })
    expect(statStylePatch("stat")).toEqual({})
    expect(statStylePatch("gauge")).toEqual({ stat_color_mode: undefined, stat_text_mode: undefined, stat_text_size: undefined, stat_orientation: undefined })
  })

  it("paints the value, the background or nothing", () => {
    expect(statAppearanceStyles(panel({}), "#16a34a")).toEqual({ value: { color: "#16a34a" }, accent: "#16a34a" })
    expect(statAppearanceStyles(panel({ stat_color_mode: "none" }), "#16a34a")).toEqual({ accent: "currentColor" })
    // 绿色与深蓝都按相对亮度选浅色前景，黄色这种高亮度底才切到深色前景（与表格着色同一套规则）。
    expect(statAppearanceStyles(panel({ stat_color_mode: "background" }), "#16a34a"))
      .toEqual({ container: { backgroundColor: "#16a34a", color: "#f8fafc" }, accent: "#f8fafc" })
    expect(statAppearanceStyles(panel({ stat_color_mode: "background" }), "#facc15"))
      .toEqual({ container: { backgroundColor: "#facc15", color: "#0f172a" }, accent: "#0f172a" })
  })

  it("picks a readable foreground for non-hex colours", () => {
    expect(readableStatForeground("#ffffff")).toBe("#0f172a")
    expect(readableStatForeground("#000000")).toBe("#f8fafc")
    expect(readableStatForeground("var(--chart-1)")).toBe("#f8fafc")
  })
})
