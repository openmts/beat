import { describe, expect, it } from "vitest"
import {
  defaultHeatmapBuckets, defaultHeatmapColor, defaultHeatmapScale, heatmapCellColor, heatmapColumnBudget,
  heatmapError, heatmapGrid, heatmapIntensity, heatmapPatch, heatmapTimeColumns, heatmapValueBounds, maxHeatmapCells, panelHeatmap,
} from "./workspace-heatmap"
import type { DashboardPanel, MetricQueryResult } from "@/types/metric-workspace"

function heatmapPanel(panel: Partial<DashboardPanel> = {}): DashboardPanel {
  return { id: "p1", title: "Heat", type: "heatmap", size: "medium", legend: true, thresholds: [], series: [], ...panel }
}

function pointResult(points: { timestamp: number; value: number | null }[], interval = 1000): MetricQueryResult {
  return { from: "2026-10-16T00:00:00Z", to: "2026-10-16T01:00:00Z", interval_ms: interval,
    series: [{ ref_id: "A", node_id: "n1", metric: "cpu", aggregation: "avg", label: "CPU", unit: "percent", available_from: null, state: "ok", points }] }
}

describe("panel heatmap", () => {
  it("falls back to the documented defaults", () => {
    expect(panelHeatmap({ type: "heatmap" })).toEqual({ buckets: defaultHeatmapBuckets, scale: defaultHeatmapScale, color: defaultHeatmapColor })
    expect(panelHeatmap({ type: "heatmap", heatmap_buckets: 1, heatmap_scale: "sqrt", heatmap_color: "magenta" }))
      .toEqual({ buckets: defaultHeatmapBuckets, scale: defaultHeatmapScale, color: defaultHeatmapColor })
  })

  it("keeps documented values", () => {
    expect(panelHeatmap({ type: "heatmap", heatmap_buckets: 24, heatmap_scale: "log", heatmap_color: "violet" }))
      .toEqual({ buckets: 24, scale: "log", color: "violet" })
  })

  it("mirrors the server enums and the heatmap-only rule", () => {
    expect(heatmapError({ type: "heatmap" })).toBeNull()
    expect(heatmapError({ type: "heatmap", heatmap_buckets: 64, heatmap_scale: "log", heatmap_color: "amber" })).toBeNull()
    expect(heatmapError({ type: "heatmap", heatmap_buckets: 65 })).toBe("workspace.heatmap_buckets_invalid")
    expect(heatmapError({ type: "heatmap", heatmap_buckets: 2.5 })).toBe("workspace.heatmap_buckets_invalid")
    expect(heatmapError({ type: "heatmap", heatmap_scale: "sqrt" })).toBe("workspace.heatmap_scale_invalid")
    expect(heatmapError({ type: "heatmap", heatmap_color: "magenta" })).toBe("workspace.heatmap_color_invalid")
    expect(heatmapError({ type: "timeseries", heatmap_scale: "log" })).toBe("workspace.heatmap_heatmap_only")
    expect(heatmapError({ type: "table" })).toBeNull()
  })

  it("clears the heatmap fields only when the panel stops being a heatmap", () => {
    expect(heatmapPatch("heatmap")).toEqual({})
    expect(heatmapPatch("timeseries")).toEqual({ heatmap_buckets: undefined, heatmap_scale: undefined, heatmap_color: undefined })
  })
})

describe("heatmap bucketing", () => {
  it("splits values into equal linear buckets", () => {
    expect(heatmapValueBounds([], { min: 0, max: 40, buckets: 4, scale: "linear" })).toEqual([0, 10, 20, 30, 40])
  })

  it("derives bounds from the samples when the axis is automatic", () => {
    expect(heatmapValueBounds([2, 6], { buckets: 2, scale: "linear" })).toEqual([2, 4, 6])
  })

  it("spaces log bounds geometrically and needs a positive lower bound", () => {
    const bounds = heatmapValueBounds([], { min: 1, max: 100, buckets: 2, scale: "log" })
    expect(bounds.map((value) => Number(value.toFixed(6)))).toEqual([1, 10, 100])
    expect(heatmapValueBounds([], { min: 0, max: 100, buckets: 2, scale: "log" })).toEqual([0, 50, 100])
  })

  it("widens an empty range so every sample still lands in one bucket", () => {
    expect(heatmapValueBounds([5], { buckets: 2, scale: "linear" })).toEqual([5, 5.5, 6])
  })

  it("merges time columns down to the cell budget", () => {
    const columns = heatmapTimeColumns([0, 10, 20, 30], 2)
    expect(columns).toEqual([{ from: 0, to: 15 }, { from: 15, to: 30 }])
    expect(heatmapTimeColumns([0], 4)).toEqual([{ from: 0, to: 0 }])
    expect(heatmapTimeColumns([], 4)).toEqual([])
    expect(heatmapColumnBudget(12)).toBe(Math.floor(maxHeatmapCells / 12))
    expect(heatmapColumnBudget(0)).toBe(maxHeatmapCells)
  })

  it("scales cell intensity linearly and logarithmically", () => {
    expect(heatmapIntensity(0, 10, "linear")).toBe(0)
    expect(heatmapIntensity(5, 10, "linear")).toBe(0.5)
    expect(heatmapIntensity(20, 10, "linear")).toBe(1)
    expect(heatmapIntensity(0, 10, "log")).toBe(0)
    expect(heatmapIntensity(10, 10, "log")).toBe(1)
    expect(heatmapIntensity(1, 10, "log")).toBeGreaterThan(0.25)
    expect(heatmapIntensity(1, 10, "log")).toBeLessThan(0.5)
  })

  it("tints the palette color by intensity", () => {
    expect(heatmapCellColor("blue", 0)).toBe("rgba(59, 130, 246, 0.060)")
    expect(heatmapCellColor("blue", 1)).toBe("rgba(59, 130, 246, 0.960)")
    expect(heatmapCellColor("violet", 2)).toBe("rgba(139, 92, 246, 0.960)")
    expect(heatmapCellColor("unknown", 0.5)).toBe("rgba(59, 130, 246, 0.510)")
  })
})

describe("heatmap grid", () => {
  it("counts every finite sample into its time column and value bucket", () => {
    const grid = heatmapGrid(heatmapPanel({ heatmap_buckets: 2, min: 0, max: 100 }), pointResult([
      { timestamp: 1000, value: 10 }, { timestamp: 1000, value: 90 }, { timestamp: 2000, value: 90 },
    ]))
    expect(grid.rows).toEqual([{ from: 0, to: 50 }, { from: 50, to: 100 }])
    expect(grid.columns.length).toBe(2)
    expect(grid.counts).toEqual([[1, 0], [1, 1]])
    expect(grid.max).toBe(1)
    expect(grid.total).toBe(3)
  })

  it("drops null and non-finite samples and reports an empty grid", () => {
    const grid = heatmapGrid(heatmapPanel(), pointResult([{ timestamp: 1000, value: null }, { timestamp: 2000, value: Number.NaN }]))
    expect(grid.counts).toEqual([])
    expect(grid.total).toBe(0)
    expect(heatmapGrid(heatmapPanel(), pointResult([])).columns).toEqual([])
  })

  it("merges series and keeps the busiest cell as the colour maximum", () => {
    const result = pointResult([{ timestamp: 1000, value: 10 }, { timestamp: 2000, value: 10 }, { timestamp: 3000, value: 10 }])
    result.series.push({ ...result.series[0], ref_id: "B", points: [{ timestamp: 1000, value: 10 }] })
    const grid = heatmapGrid(heatmapPanel({ heatmap_buckets: 3, min: 0, max: 30 }), result)
    expect(grid.total).toBe(4)
    expect(grid.max).toBe(2)
  })
})
