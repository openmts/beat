import { describe, expect, it } from "vitest"
import { sortWorkspaceStats, workspaceSeriesStats, workspaceStatsCSV, workspaceStatsTotals } from "@/lib/workspace-stats"
import type { ExpandedSeries, MetricQueryResult } from "@/types/metric-workspace"

function result(series: MetricQueryResult["series"]): MetricQueryResult {
  return { from: "2026-10-17T00:00:00.000Z", to: "2026-10-17T01:00:00.000Z", interval_ms: 60000, series }
}

function series(points: (number | null)[]): MetricQueryResult["series"][number] {
  return { ref_id: "A", node_id: "node-one", metric: "cpu", aggregation: "avg", label: "CPU usage", unit: "percent",
    available_from: "2026-10-17T00:00:00.000Z", state: "ok",
    points: points.map((value, index) => ({ timestamp: Date.parse("2026-10-17T00:00:00.000Z") + index * 60000, value })) }
}

describe("workspace series stats", () => {
  it("computes min, max, mean, first, last and counts gaps", () => {
    const [stats] = workspaceSeriesStats(result([series([40, null, 20, 60])]))
    expect(stats!.min).toBe(20)
    expect(stats!.max).toBe(60)
    expect(stats!.mean).toBe(40)
    expect(stats!.first).toBe(40)
    expect(stats!.last).toBe(60)
    expect(stats!.samples).toBe(3)
    expect(stats!.gaps).toBe(1)
  })

  it("reports null statistics for an all-gap series", () => {
    const [stats] = workspaceSeriesStats(result([series([null, null])]))
    expect(stats!.min).toBeNull()
    expect(stats!.mean).toBeNull()
    expect(stats!.samples).toBe(0)
    expect(stats!.gaps).toBe(2)
  })

  it("keeps the first and last valid samples when the series starts or ends with a gap", () => {
    const [stats] = workspaceSeriesStats(result([series([null, 10, 30, null])]))
    expect(stats!.first).toBe(10)
    expect(stats!.last).toBe(30)
    expect(stats!.mean).toBe(20)
  })

  it("uses the expanded presentation label and one row per series", () => {
    const expanded: ExpandedSeries[] = [{ ref_id: "A", node_id: "node-one", metric: "cpu", aggregation: "avg",
      color: "blue", label: "CPU usage", node_name: "Production host" }]
    const stats = workspaceSeriesStats(result([series([1, 2]), { ...series([3]), ref_id: "B", node_id: "node-two", unit: "bytes" }]), expanded)
    expect(stats).toHaveLength(2)
    expect(stats[0]!.label).toBe("CPU usage · Production host")
    expect(stats[1]!.label).toBe("CPU usage")
    expect(stats[1]!.unit).toBe("bytes")
    expect(stats[1]!.max).toBe(3)
  })

  it("sorts numeric columns and always keeps empty values last", () => {
    const rows = workspaceSeriesStats(result([series([null, null]), series([5, 1]), series([2, 3, 4])]))
    const ascending = sortWorkspaceStats(rows, "max", "asc").map((row) => row.max)
    expect(ascending).toEqual([4, 5, null])
    const descending = sortWorkspaceStats(rows, "max", "desc").map((row) => row.max)
    expect(descending).toEqual([5, 4, null])
  })

  it("totals the samples, gaps and coverage of the whole table", () => {
    const rows = workspaceSeriesStats(result([series([40, null, 20]), series([10, null, null])]))
    expect(workspaceStatsTotals(rows)).toEqual({ series: 2, samples: 3, gaps: 3, coverage: 0.5 })
    expect(workspaceStatsTotals([])).toEqual({ series: 0, samples: 0, gaps: 0, coverage: null })
  })

  it("exports raw values with localized headers and empty cells for nulls", () => {
    const rows = workspaceSeriesStats(result([series([40, null, 20])]))
    const csv = workspaceStatsCSV(rows, ["Series", "Min", "Max", "Mean", "First", "Last", "Count", "Gaps"])
    const lines = csv.split("\r\n")
    expect(lines[0]).toBe(`"Series","Min","Max","Mean","First","Last","Count","Gaps"`)
    expect(lines[1]).toBe(`"CPU usage","20","40","30","40","20","2","1"`)
  })
})
