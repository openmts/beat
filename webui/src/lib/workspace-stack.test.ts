import { describe, expect, it } from "vitest"
import { percentStackGroups, percentStackRows, percentStackValue, stackedTooltipValue, stackedTooltipValueByRef } from "@/lib/workspace-stack"

function rows() {
  return [
    { timestamp: 1_000, a: 1, b: 1, c: 3 },
    { timestamp: 2_000, a: null, b: 0, c: 0 },
  ]
}

describe("workspace percent stacking", () => {
  it("normalises each time point into 100% and keeps a zero total untouched", () => {
    const stacked = percentStackRows(rows(), ["a", "b"])
    // 只有被归一化的列留在行里，其它列由分组调用方自己拼回去。
    expect(stacked.rows.map((row) => [row.a, row.b, row.c])).toEqual([[50, 50, undefined], [null, 0, undefined]])
    expect(stacked.totals.get(1_000)).toBe(2)
    expect(stacked.totals.get(2_000)).toBe(0)
  })

  it("keeps the single axis result and normalises each axis on its own", () => {
    const single = percentStackGroups(rows(), [["a", "b"], []])
    // 单轴面板只多了一组总量表：行数据与直接调用 percentStackRows 的口径一致，且未参与归一化的列原样保留。
    expect(single.rows).toEqual([
      { timestamp: 1_000, a: 50, b: 50, c: 3 },
      { timestamp: 2_000, a: null, b: 0, c: 0 },
    ])
    expect(single.totals.get("a")).toEqual(new Map([[1_000, 2], [2_000, 0]]))
    expect(single.totals.get("a")).toEqual(single.totals.get("b"))
    const dual = percentStackGroups(rows(), [["a"], ["b", "c"]])
    expect(dual.rows.map((row) => [row.a, row.b, row.c])).toEqual([[100, 25, 75], [null, 0, 0]])
    expect(dual.totals.get("a")?.get(1_000)).toBe(1)
    expect(dual.totals.get("b")?.get(1_000)).toBe(4)
    expect(dual.totals.has("c")).toBe(true)
  })

  it("restores the original value from the right axis group and falls back to the coordinate", () => {
    const dual = percentStackGroups(rows(), [["a"], ["b", "c"]]).totals
    const item = { dataKey: "c", payload: { timestamp: 1_000 } }
    expect(stackedTooltipValueByRef(75, item, dual)).toBe(3)
    expect(stackedTooltipValueByRef(75, { ...item, dataKey: "missing" }, dual)).toBe(75)
    expect(stackedTooltipValueByRef(75, item, null)).toBe(75)
    // 单轴调用点继续沿用手写总量表的老口径。
    expect(stackedTooltipValue(50, item, new Map([[1_000, 2]]))).toBe(1)
    expect(percentStackValue(50, 2_000, new Map([[2_000, 0]]))).toBe(50)
  })
})
