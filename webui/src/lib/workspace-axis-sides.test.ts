import { describe, expect, it } from "vitest"
import { axisHasBothSides, axisSideGroup, axisSideMaximum, axisSideSupported } from "@/lib/workspace-axis-sides"
import type { MetricResultSeries } from "@/types/metric-workspace"

function series(refId: string, values: (number | null)[]): MetricResultSeries {
  return {
    ref_id: refId, node_id: "node-1", metric: "cpu", aggregation: "avg", label: refId, unit: "percent",
    available_from: null, state: "ok", points: values.map((value, index) => ({ timestamp: index * 1000, value })),
  }
}

describe("workspace axis sides", () => {
  it("groups series by side and only reports two axes when both sides carry one", () => {
    const groups = axisSideGroup([1, 2, 3, 4], (value) => value % 2 === 0 ? "right" : "left")
    expect(groups).toEqual({ left: [1, 3], right: [2, 4] })
    expect(axisHasBothSides(groups)).toBe(true)
    expect(axisHasBothSides({ left: [1], right: [] })).toBe(false)
    expect(axisHasBothSides({ left: [], right: [] })).toBe(false)
  })

  it("takes the largest absolute sample of one side and tolerates an empty side", () => {
    expect(axisSideMaximum([series("a", [1, -7, null]), series("b", [3])])).toBe(7)
    expect(axisSideMaximum([])).toBe(0)
  })

  it("only accepts panels that own a switchable value axis", () => {
    expect([axisSideSupported("timeseries"), axisSideSupported("barchart")]).toEqual([true, true])
    expect([axisSideSupported("table"), axisSideSupported("row"), axisSideSupported(undefined)]).toEqual([false, false, false])
  })
})
