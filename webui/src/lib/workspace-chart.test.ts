import { describe, expect, it } from "vitest"
import { seriesLegendStatistics, workspaceLegendOrder } from "./workspace-chart"
import { workspaceDashboard, workspaceResult } from "@/test/metric-workspace-fixtures"
import type { MetricQueryResult, MetricResultSeries } from "@/types/metric-workspace"

function series(refId: string, values: (number | null)[]): MetricResultSeries {
  const base = workspaceResult().series[0]
  return { ...base, ref_id: refId, label: refId, points: values.map((value, index) => ({ timestamp: index + 1, value })) }
}

function multi(): MetricQueryResult {
  return { ...workspaceResult(), series: [series("A", [10, null, 30]), series("B", [50, 20]), series("C", [5]), series("D", [null])] }
}

describe("workspace legend", () => {
  // 值计算（Grafana 的 Calculation）与图例取值复用同一趟扫描：六类统计量一起断言，
  // 且「没有有效样本」必须是 null 而不是 0，否则 sum/count 会把无数据渲染成真实的零。
  it("summarizes finite samples and keeps missing values empty", () => {
    expect(seriesLegendStatistics(series("A", [10, null, 30]))).toEqual({ last: 30, min: 10, max: 30, mean: 20, sum: 40, count: 2 })
    expect(seriesLegendStatistics(series("D", [null, null]))).toEqual({ last: null, min: null, max: null, mean: null, sum: null, count: null })
  })

  it("orders entries by the first displayed value without moving the default order", () => {
    const panel = workspaceDashboard().definition.panels[0]
    expect(workspaceLegendOrder({ ...panel, legend_sort: "none" }, multi()).map((entry) => entry.ref_id)).toEqual(["A", "B", "C", "D"])
    expect(workspaceLegendOrder({ ...panel, legend_sort: "desc" }, multi()).map((entry) => entry.ref_id)).toEqual(["A", "B", "C", "D"])
    expect(workspaceLegendOrder({ ...panel, legend_sort: "asc" }, multi()).map((entry) => entry.ref_id)).toEqual(["C", "B", "A", "D"])
  })

  it("orders by the first selected statistic", () => {
    const panel = workspaceDashboard().definition.panels[0]
    const ordered = workspaceLegendOrder({ ...panel, legend_values: ["mean"], legend_sort: "desc" }, multi())
    expect(ordered.map((entry) => entry.ref_id)).toEqual(["B", "A", "C", "D"])
  })
})
