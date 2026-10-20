import { describe, expect, it } from "vitest"
import {
  applyFillBelowRows, fillBelowBands, fillBelowBaseKey, fillBelowDeltaKey, fillBelowFillOpacity, fillBelowOpacity,
  fillBelowStackId, fillBelowTooltipValue, type FillBelowRow,
} from "@/lib/workspace-fill-below"
import type { DashboardOverride, MetricResultSeries } from "@/types/metric-workspace"

function series(refId: string, label: string, values: (number | null)[], nodeId = "node-one"): MetricResultSeries {
  return { ref_id: refId, node_id: nodeId, metric: "cpu", aggregation: "avg", label, unit: "percent",
    available_from: null, state: "ok", points: values.map((value, index) => ({ timestamp: index * 1000, value })) }
}

function panel(type = "timeseries", overrides: DashboardOverride[] = [], stack?: string) {
  return { type, stack, overrides }
}

const override = (value: string, matcher = "by_ref_id"): DashboardOverride =>
  ({ matcher: { type: matcher as "by_ref_id", value: "A" }, fill_below_to: value })

describe("fill below bands", () => {
  it("resolves the target series by label on the same node", () => {
    const source = series("A", "CPU min", [1, 2]), sameNode = series("B", "CPU max", [3, 4])
    const otherNode = series("B", "CPU max", [5, 6], "node-two")
    const bands = fillBelowBands(panel("timeseries", [override("CPU max")]), [source, otherNode, sameNode])
    expect(bands).toEqual([{ refId: "A", targetRefId: "B", baseKey: fillBelowBaseKey("A"),
      deltaKey: fillBelowDeltaKey("A"), stackId: fillBelowStackId("A"), side: "left" }])
  })

  it("falls back to the first series with that name and skips its own query", () => {
    const source = series("A", "CPU", [1])
    // 同名同 ref_id 的序列在行数据里共用一列，差值恒为 0，因此不构成终点。
    const twin = series("A", "CPU", [2], "node-two")
    expect(fillBelowBands(panel("timeseries", [override("CPU")]), [source, twin])).toEqual([])
    const other = series("B", "CPU", [9], "node-two")
    expect(fillBelowBands(panel("timeseries", [override("CPU")]), [source, other]).map((band) => band.targetRefId)).toEqual(["B"])
  })

  it("ignores unknown names, other panel types and stacked panels", () => {
    const source = series("A", "CPU", [1]), target = series("B", "Memory", [2])
    expect(fillBelowBands(panel("timeseries", [override("Disk")]), [source, target])).toEqual([])
    expect(fillBelowBands(panel("barchart", [override("Memory")]), [source, target])).toEqual([])
    expect(fillBelowBands(panel("timeseries", [override("Memory")], "normal"), [source, target])).toEqual([])
    expect(fillBelowBands(panel("timeseries", []), [source, target])).toEqual([])
    expect(fillBelowBands(undefined, [source, target])).toEqual([])
  })

  it("puts the band on the axis of the target series", () => {
    const source = series("A", "CPU min", [1]), target = series("B", "CPU max", [2])
    const moved = [{ matcher: { type: "by_ref_id" as const, value: "B" }, axis_placement: "right" as const }, override("CPU max")]
    expect(fillBelowBands(panel("timeseries", moved), [source, target])[0]?.side).toBe("right")
  })
})

describe("fill below rows", () => {
  const band = { refId: "A", targetRefId: "B", baseKey: "__fill_base_A", deltaKey: "__fill_delta_A", stackId: "beat-fill-A", side: "left" as const }

  it("writes the target value and the difference into the derived keys", () => {
    const rows: FillBelowRow[] = [{ timestamp: 0, A: 10, B: 4 }]
    expect(applyFillBelowRows(rows, [band])[0]).toMatchObject({ __fill_base_A: 4, __fill_delta_A: 6 })
  })

  it("blanks both layers when either series has no sample so the area breaks", () => {
    const rows: FillBelowRow[] = [{ timestamp: 0, A: 10, B: null }, { timestamp: 1, A: null, B: 4 }, { timestamp: 2, A: 10, B: 4 }]
    const filled = applyFillBelowRows(rows, [band])
    expect(filled.map((row) => [row.__fill_base_A, row.__fill_delta_A])).toEqual([[null, null], [null, null], [4, 6]])
    // 没有填充带时不复制行数据，折线面板保持既有对象引用。
    expect(applyFillBelowRows(rows, [])).toBe(rows)
  })

  it("keeps the tooltip on the covered series reading", () => {
    const rows = applyFillBelowRows([{ timestamp: 0, A: 10, B: 4 }], [band])
    expect(fillBelowTooltipValue(6, { dataKey: band.deltaKey, payload: rows[0] }, [band])).toBe(10)
    expect(fillBelowTooltipValue(4, { dataKey: "B", payload: rows[0] }, [band])).toBe(4)
    expect(fillBelowTooltipValue(6, { dataKey: band.deltaKey }, [band])).toBe(6)
  })

  it("gives an invisible fill a readable floor", () => {
    expect(fillBelowOpacity(40)).toBe(40)
    expect(fillBelowOpacity(0)).toBe(fillBelowFillOpacity)
  })
})
