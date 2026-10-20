import { describe, expect, it } from "vitest"
import {
  defaultHistogramBuckets, defaultHistogramColor, defaultHistogramMerge, defaultHistogramScale, histogramBarRatio,
  histogramBins, histogramDistributions, histogramError, histogramPatch, histogramPeak, maxHistogramBuckets, minHistogramBuckets, panelHistogram,
} from "./workspace-histogram"
import type { DashboardPanel, MetricQueryResult } from "@/types/metric-workspace"

function histogramPanel(panel: Partial<DashboardPanel> = {}): DashboardPanel {
  return { id: "p1", title: "Latency", type: "histogram", size: "medium", legend: true, thresholds: [], series: [], ...panel }
}

function resultWith(series: { ref_id: string; label: string; color: "blue" | "green"; values: (number | null)[] }[]): MetricQueryResult {
  return { from: "2026-10-16T00:00:00Z", to: "2026-10-16T01:00:00Z", interval_ms: 1000,
    series: series.map((entry) => ({ ref_id: entry.ref_id, node_id: "n1", metric: "cpu", aggregation: "avg", label: entry.label,
      unit: "percent", available_from: null, state: "ok",
      points: entry.values.map((value, index) => ({ timestamp: 1_000_000 + index * 1000, value })) })) }
}

describe("panel histogram", () => {
  it("falls back to the documented defaults", () => {
    expect(panelHistogram({ type: "histogram" })).toEqual({ buckets: defaultHistogramBuckets, scale: defaultHistogramScale, color: defaultHistogramColor, merge: defaultHistogramMerge })
    expect(panelHistogram({ type: "histogram", histogram_buckets: 1, histogram_scale: "sqrt", histogram_color: "magenta", histogram_merge: "yes" as unknown as boolean }))
      .toEqual({ buckets: defaultHistogramBuckets, scale: defaultHistogramScale, color: defaultHistogramColor, merge: defaultHistogramMerge })
  })

  it("keeps documented values and mirrors the server enums", () => {
    expect(panelHistogram({ type: "histogram", histogram_buckets: 24, histogram_scale: "log", histogram_color: "violet", histogram_merge: true }))
      .toEqual({ buckets: 24, scale: "log", color: "violet", merge: true })
    expect(histogramError({ type: "histogram" })).toBeNull()
    expect(histogramError({ type: "histogram", histogram_buckets: maxHistogramBuckets, histogram_scale: "log", histogram_color: "amber", histogram_merge: false })).toBeNull()
    expect(histogramError({ type: "histogram", histogram_buckets: maxHistogramBuckets + 1 })).toBe("workspace.histogram_buckets_invalid")
    expect(histogramError({ type: "histogram", histogram_buckets: minHistogramBuckets - 0.5 })).toBe("workspace.histogram_buckets_invalid")
    expect(histogramError({ type: "histogram", histogram_scale: "sqrt" })).toBe("workspace.histogram_scale_invalid")
    expect(histogramError({ type: "histogram", histogram_color: "magenta" })).toBe("workspace.histogram_color_invalid")
    expect(histogramError({ type: "timeseries", histogram_merge: true })).toBe("workspace.histogram_histogram_only")
    expect(histogramError({ type: "table" })).toBeNull()
  })

  it("clears the histogram fields only when the panel stops being a histogram", () => {
    expect(histogramPatch("histogram")).toEqual({})
    expect(histogramPatch("timeseries")).toEqual({ histogram_buckets: undefined, histogram_scale: undefined, histogram_color: undefined, histogram_merge: undefined })
  })

  it("buckets samples into equal value ranges and keeps the upper bound in the last bin", () => {
    // 20 等于第一个分桶的上界，按 [from, to) 规则落到下一桶；40 等于整体上界，归入最后一桶。
    const bins = histogramBins([10, 20, 30, 40], { min: 0, max: 40, buckets: 2, scale: "linear" })
    expect(bins.map((bin) => bin.count)).toEqual([1, 3])
    expect(bins[0]).toEqual({ from: 0, to: 20, count: 1 })
    expect(histogramBins([], { buckets: 4, scale: "linear" })).toEqual([])
  })

  it("keeps one distribution per series unless series are combined", () => {
    const result = resultWith([{ ref_id: "A", label: "fast", color: "blue", values: [1, 2, null] }, { ref_id: "B", label: "slow", color: "green", values: [30, 40] }])
    const panel = histogramPanel({ histogram_buckets: 2, min: 0, max: 40, series: [
      { ref_id: "A", node_id: "$node", metric: "cpu", aggregation: "avg", label: "fast", color: "blue" },
      { ref_id: "B", node_id: "$node", metric: "cpu", aggregation: "avg", label: "slow", color: "green" }] })
    const separate = histogramDistributions(panel, result)
    expect(separate.map((entry) => entry.total)).toEqual([2, 2])
    expect(separate.map((entry) => entry.color)).toEqual(["blue", "green"])
    expect(separate[0].bins.map((bin) => bin.count)).toEqual([2, 0])
    const combined = histogramDistributions({ ...panel, histogram_merge: true, histogram_color: "violet" }, result)
    expect(combined).toHaveLength(1)
    expect(combined[0].color).toBe("violet")
    expect(combined[0].total).toBe(4)
    expect(histogramPeak(separate)).toBe(2)
  })

  it("ignores series that only carry null samples", () => {
    const result = resultWith([{ ref_id: "A", label: "fast", color: "blue", values: [null, null] }])
    expect(histogramDistributions(histogramPanel(), result)).toEqual([])
    expect(histogramPeak([])).toBe(0)
  })

  it("scales bar heights linearly or logarithmically", () => {
    expect(histogramBarRatio(5, 10, "linear")).toBe(0.5)
    expect(histogramBarRatio(0, 10, "linear")).toBe(0)
    expect(histogramBarRatio(9, 9, "linear")).toBe(1)
    expect(histogramBarRatio(0, 0, "log")).toBe(0)
    expect(histogramBarRatio(9, 9, "log")).toBe(1)
    expect(histogramBarRatio(3, 100, "log")).toBeGreaterThan(histogramBarRatio(3, 100, "linear"))
  })
})
