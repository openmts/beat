import { describe, expect, it } from "vitest"
import {
  applyPanelTransformations, applyPanelTransformationsWithFrame, emptyTransformation, maximumPanelTransformations, maximumTransformationPatternBytes,
  maximumTransformationReplacementBytes, switchTransformationKind, transformationError,
} from "@/lib/workspace-transformations"
import type { DashboardTransformation, MetricQueryResult, MetricResultSeries } from "@/types/metric-workspace"

function series(refId: string, values: (number | null)[], label = refId): MetricResultSeries {
  return {
    ref_id: refId, node_id: "node-1", metric: "cpu", aggregation: "avg", label, unit: "percent",
    available_from: null, state: "ok",
    points: values.map((value, index) => ({ timestamp: 1_700_000_000_000 + index * 30_000, value })),
  }
}

function result(...seriesList: MetricResultSeries[]): MetricQueryResult {
  return { from: "", to: "", interval_ms: 30_000, series: seriesList }
}

function panel(...transformations: DashboardTransformation[]) {
  return { transformations: transformations.length ? transformations : undefined }
}

describe("workspace transformations", () => {
  it("returns the untouched result when the panel carries no chain", () => {
    const original = result(series("A", [1, 2]))
    expect(applyPanelTransformations(panel(), original)).toBe(original)
  })

  it("renames series labels with a global regular expression", () => {
    const applied = applyPanelTransformations(panel({ kind: "rename_by_regex", pattern: "cpu", replacement: "processor" }),
      result(series("A", [1], "node cpu total"), series("B", [2], "cpu idle")))
    expect(applied.series.map((value) => value.label)).toEqual(["node processor total", "processor idle"])
    expect(applied.series.map((value) => value.points.map((point) => point.value))).toEqual([[1], [2]])
  })

  it("keeps series when a rename pattern does not compile", () => {
    const applied = applyPanelTransformations(panel({ kind: "rename_by_regex", pattern: "([", replacement: "x" }), result(series("A", [1], "cpu")))
    expect(applied.series.map((value) => value.label)).toEqual(["cpu"])
  })

  it("filters series by their latest finite sample", () => {
    const applied = applyPanelTransformations(panel({ kind: "filter_by_value", comparison: "gte", value: 50 }),
      result(series("A", [10, 80]), series("B", [90, 20]), series("C", [null, null])))
    expect(applied.series.map((value) => value.ref_id)).toEqual(["A"])
  })

  it("supports every documented comparison", () => {
    const cases: [DashboardTransformation["comparison"], number[]][] = [
      ["gt", [41, 40]], ["gte", [40, 39]], ["lt", [39, 40]], ["lte", [40, 41]], ["eq", [40, 41]], ["ne", [41, 40]],
    ]
    for (const [comparison, [keep, drop]] of cases) {
      const applied = applyPanelTransformations(panel({ kind: "filter_by_value", comparison, value: 40 }),
        result(series("keep", [keep]), series("drop", [drop])))
      expect([comparison, applied.series.map((value) => value.ref_id)]).toEqual([comparison, ["keep"]])
    }
  })

  it("treats an unknown comparison as a no-op instead of emptying the panel", () => {
    const applied = applyPanelTransformations(panel({ kind: "filter_by_value", comparison: undefined, value: 1 }), result(series("A", [0])))
    expect(applied.series.map((value) => value.ref_id)).toEqual(["A"])
  })

  it("sorts by the requested statistic and keeps missing samples last", () => {
    const ascending = applyPanelTransformations(panel({ kind: "sort_by", statistic: "mean", order: "asc" }),
      result(series("high", [10, 10]), series("none", [null]), series("low", [1, 3])))
    expect(ascending.series.map((value) => value.ref_id)).toEqual(["low", "high", "none"])
    const descending = applyPanelTransformations(panel({ kind: "sort_by", statistic: "max", order: "desc" }),
      result(series("low", [1, 2]), series("high", [1, 9])))
    expect(descending.series.map((value) => value.ref_id)).toEqual(["high", "low"])
  })

  it("limits the series count after the earlier steps", () => {
    const applied = applyPanelTransformations(panel(
      { kind: "sort_by", statistic: "last", order: "desc" },
      { kind: "limit", count: 2 },
    ), result(series("a", [1]), series("c", [3]), series("b", [2])))
    expect(applied.series.map((value) => value.ref_id)).toEqual(["c", "b"])
    expect(applyPanelTransformations(panel({ kind: "limit", count: 0 }), result(series("a", [1]))).series).toEqual([])
  })
})

describe("transformation editing helpers", () => {
  it("builds an identity rename by default so a fresh step never blanks labels", () => {
    expect(emptyTransformation("rename_by_regex")).toEqual({ kind: "rename_by_regex", pattern: "^(.*)$", replacement: "$1" })
    expect(emptyTransformation("sort_by")).toEqual({ kind: "sort_by", statistic: "last", order: "desc" })
    expect(emptyTransformation("limit")).toEqual({ kind: "limit", count: 5 })
    expect(emptyTransformation("binary_operation")).toEqual({ kind: "binary_operation", left: "A", operator: "+", right: "B" })
    expect(emptyTransformation()).toEqual({ kind: "filter_by_value", comparison: "gt", value: 0 })
  })

  it("replaces the whole payload when the kind changes", () => {
    expect(switchTransformationKind("limit")).toEqual({ kind: "limit", count: 5 })
    expect(switchTransformationKind("nonsense")).toEqual({ kind: "filter_by_value", comparison: "gt", value: 0 })
  })

  it("accepts the documented chains", () => {
    expect(transformationError(panel())).toBeNull()
    expect(transformationError(panel(
      { kind: "rename_by_regex", pattern: "^(.*)$", replacement: "$1" },
      { kind: "filter_by_value", comparison: "gt", value: 0 },
      { kind: "sort_by", statistic: "mean", order: "asc" },
      { kind: "limit", count: 8 },
    ))).toBeNull()
    expect(transformationError(panel({ kind: "binary_operation", left: "used", operator: "/", right: "/^total/", alias: "usage" }))).toBeNull()
  })

  it("mirrors the server rules for every kind", () => {
    expect(transformationError(panel(...Array.from({ length: maximumPanelTransformations + 1 }, () => ({ kind: "limit" as const, count: 1 })))))
      .toBe("workspace.transformations_too_many")
    expect(transformationError(panel({ kind: "rename_by_regex", pattern: "([", replacement: "x" }))).toBe("workspace.transformation_pattern_invalid")
    expect(transformationError(panel({ kind: "rename_by_regex", pattern: "a".repeat(129), replacement: "x" }))).toBe("workspace.transformation_pattern_invalid")
    expect(transformationError(panel({ kind: "rename_by_regex", pattern: "a", replacement: "x".repeat(maximumTransformationReplacementBytes + 1) })))
      .toBe("workspace.transformation_replacement_invalid")
    expect(transformationError(panel({ kind: "filter_by_value", value: 1 }))).toBe("workspace.transformation_comparison_invalid")
    expect(transformationError(panel({ kind: "filter_by_value", comparison: "gt" }))).toBe("workspace.transformation_value_invalid")
    expect(transformationError(panel({ kind: "sort_by", statistic: "last", order: "up" as "asc" }))).toBe("workspace.transformation_order_invalid")
    expect(transformationError(panel({ kind: "filter_by_label" }))).toBe("workspace.transformation_pattern_invalid")
    expect(transformationError(panel({ kind: "filter_by_label", pattern: "([" }))).toBe("workspace.transformation_pattern_invalid")
    expect(transformationError(panel({ kind: "filter_by_label", pattern: "a".repeat(maximumTransformationPatternBytes + 1) }))).toBe("workspace.transformation_pattern_invalid")
    expect(transformationError(panel({ kind: "limit", count: 9 }))).toBe("workspace.transformation_count_invalid")
    expect(transformationError(panel({ kind: "binary_operation", operator: "+", right: "b" }))).toBe("workspace.transformation_selector_invalid")
    expect(transformationError(panel({ kind: "binary_operation", left: "a", operator: "+", right: "/(/" }))).toBe("workspace.transformation_selector_invalid")
    expect(transformationError(panel({ kind: "binary_operation", left: "a".repeat(maximumTransformationPatternBytes + 1), operator: "+", right: "b" })))
      .toBe("workspace.transformation_selector_invalid")
    expect(transformationError(panel({ kind: "binary_operation", left: "a", operator: "%" as "+", right: "b" }))).toBe("workspace.transformation_operator_invalid")
    expect(transformationError(panel({ kind: "binary_operation", left: "a", operator: "+", right: "b", alias: "x".repeat(maximumTransformationReplacementBytes + 1) })))
      .toBe("workspace.transformation_alias_invalid")
    expect(transformationError(panel({ kind: "organize" as "limit", count: 1 }))).toBe("workspace.transformation_kind_invalid")
    expect(transformationError(panel({ kind: "reduce" }))).toBe("workspace.transformation_calculation_invalid")
    expect(transformationError(panel({ kind: "reduce", calculation: "median" as "mean" }))).toBe("workspace.transformation_calculation_invalid")
    expect(transformationError(panel({ kind: "reduce", calculation: "mean", calculations: ["max"] }))).toBe("workspace.transformation_calculation_invalid")
    expect(transformationError(panel({ kind: "reduce", mode: "series_to_rows", calculations: ["min", "min"] }))).toBe("workspace.transformation_calculations_invalid")
    expect(transformationError(panel({ kind: "reduce", mode: "reduce_fields", calculations: ["min", "max"] }))).toBe("workspace.transformation_calculations_invalid")
    expect(transformationError(panel({ kind: "reduce", mode: "median" as "reduce_fields", calculations: ["mean"] }))).toBe("workspace.transformation_reduce_mode_invalid")
    expect(transformationError(panel({ kind: "group_by", calculation: "sum" }))).toBe("workspace.transformation_group_by_invalid")
    expect(transformationError(panel({ kind: "group_by", group_by: "label" as "metric", calculation: "sum" }))).toBe("workspace.transformation_group_by_invalid")
    expect(transformationError(panel({ kind: "group_by", group_by: "metric" }))).toBe("workspace.transformation_calculation_invalid")
    expect(transformationError(panel({ kind: "group_by", group_by: "metric", calculation: "delta" as "sum" }))).toBe("workspace.transformation_calculation_invalid")
    expect(transformationError(panel({ kind: "group_by", group_by: "metric", calculation: "sum", calculations: ["max"] }))).toBe("workspace.transformation_calculations_invalid")
    expect(transformationError(panel({ kind: "group_by", group_by: "metric", calculations: ["sum", "sum"] }))).toBe("workspace.transformation_calculations_invalid")
    expect(transformationError(panel({ kind: "group_by", group_by: "metric", calculations: ["delta"] }))).toBe("workspace.transformation_calculation_invalid")
    expect(transformationError(panel({ kind: "group_by", group_by: "metric", calculation: "sum" }))).toBeNull()
  })
})

describe("workspace reduce transformation", () => {
  it("collapses every series to the last finite sample with the requested calculation", () => {
    const source = result(series("A", [2, 4, 6]), series("B", [null, 3, 1]))
    const mean = applyPanelTransformations(panel({ kind: "reduce", calculation: "mean" }), source)
    expect(mean.series.map((value) => [value.points.length, value.points[0].value, value.state])).toEqual([[1, 4, "ok"], [1, 2, "ok"]])
    const range = applyPanelTransformations(panel({ kind: "reduce", calculation: "range" }), source)
    expect(range.series.map((value) => value.points[0].value)).toEqual([4, 2])
    const count = applyPanelTransformations(panel({ kind: "reduce", calculation: "count" }), source)
    expect(count.series.map((value) => value.points[0].value)).toEqual([3, 2])
  })

  it("keeps the reduced sample inside the window and leaves empty series unsampled", () => {
    const reduced = applyPanelTransformations(panel({ kind: "reduce", calculation: "last" }),
      result(series("A", [1, null, null]), series("B", [null, null, null])))
    // 最后一个有效采样落在索引 0；整条序列没有有效样本时留下空样本，渲染层继续走「无数据」。
    expect(reduced.series[0].points).toEqual([{ timestamp: 1_700_000_000_000, value: 1 }])
    expect(reduced.series[1].points).toEqual([])
    expect(reduced.series[1].state).toBe("empty")
  })

  it("expands series to rows when every calculation should be readable on its own row", () => {
    const rows = applyPanelTransformations(panel({ kind: "reduce", mode: "series_to_rows", calculations: ["min", "max"] }),
      result(series("A", [2, 4, 6])))
    // 「序列 × 计算方式」逐行展开：ref_id 与行标签都带上计算方式，后一行不会覆盖前一行。
    expect(rows.series.map((value) => [value.ref_id, value.label, value.points[0].value])).toEqual([
      ["reduce_0_min", "A · min", 2],
      ["reduce_0_max", "A · max", 6],
    ])
  })

  it("treats an unknown calculation as a no-op and composes with the earlier steps", () => {
    const original = result(series("high", [9]), series("low", [1]))
    expect(applyPanelTransformations(panel({ kind: "reduce", calculation: "median" as "mean" }), original).series).toBe(original.series)
    const chained = applyPanelTransformations(panel(
      { kind: "sort_by", statistic: "max", order: "desc" },
      { kind: "limit", count: 1 },
      { kind: "reduce", calculation: "max" },
    ), result(series("high", [9, 1]), series("low", [1, 2])))
    expect(chained.series.map((value) => [value.ref_id, value.points[0].value])).toEqual([["high", 9]])
  })
})

describe("workspace group by transformation", () => {
  function nodeSeries(node: string, metric: string, values: (number | null)[], label = node): MetricResultSeries {
    return {
      ref_id: `ref-${node}-${metric}`, node_id: node, metric, aggregation: "avg", label, unit: "percent",
      available_from: null, state: "ok",
      points: values.map((value, index) => ({ timestamp: 1_700_000_000_000 + index * 30_000, value })),
    }
  }

  it("merges every series that shares the group field into one timestamp-aligned series", () => {
    const grouped = applyPanelTransformations(panel({ kind: "group_by", group_by: "metric", calculation: "sum" }),
      result(nodeSeries("node-a", "cpu", [1, 2]), nodeSeries("node-b", "cpu", [3, 4]), nodeSeries("node-a", "mem", [10, 20])))
    expect(grouped.series.map((value) => [value.ref_id, value.label])).toEqual([["group_by_0", "cpu"], ["group_by_1", "mem"]])
    expect(grouped.series[0].points.map((point) => point.value)).toEqual([4, 6])
    expect(grouped.series[0].unit).toBe("percent")
  })

  it("emits one merged series per calculation and names them from the optional alias", () => {
    const grouped = applyPanelTransformations(panel({ kind: "group_by", group_by: "metric", calculations: ["sum", "max"], alias: "Cluster CPU" }),
      result(nodeSeries("node-a", "cpu", [1, 2]), nodeSeries("node-b", "cpu", [3, 4])))
    expect(grouped.series.map((value) => [value.ref_id, value.label, value.points[0].value])).toEqual([
      ["group_by_0_sum", "Cluster CPU (sum)", 4],
      ["group_by_0_max", "Cluster CPU (max)", 3],
    ])
  })

  it("supports every documented calculation and skips buckets without a usable sample", () => {
    const source = () => result(nodeSeries("node-a", "cpu", [1, null]), nodeSeries("node-b", "cpu", [3, 4]))
    const values = (calculation: DashboardTransformation["calculation"]) =>
      applyPanelTransformations(panel({ kind: "group_by", group_by: "metric", calculation }), source()).series[0].points.map((point) => point.value)
    expect(values("sum")).toEqual([4, 4])
    expect(values("mean")).toEqual([2, 4])
    expect(values("min")).toEqual([1, 4])
    expect(values("max")).toEqual([3, 4])
    // 第二个时间点只有一条有效样本，count 记 1，不能把缺样本的序列算成 0。
    expect(values("count")).toEqual([2, 1])
    const grouped = applyPanelTransformations(panel({ kind: "group_by", group_by: "metric", calculation: "sum" }), source())
    expect(grouped.series[0].points.map((point) => point.timestamp)).toEqual([1_700_000_000_000, 1_700_000_030_000])
  })

  it("leaves a completely empty group unsampled and groups by the node identity field", () => {
    const empty = applyPanelTransformations(panel({ kind: "group_by", group_by: "node_id", calculation: "sum" }),
      result(nodeSeries("node-a", "cpu", [null, null]), nodeSeries("node-b", "cpu", [1, 2])))
    expect(empty.series.map((value) => [value.label, value.state, value.points.length])).toEqual([["node-a", "empty", 0], ["node-b", "ok", 2]])
    const byRefId = applyPanelTransformations(panel({ kind: "group_by", group_by: "ref_id", calculation: "max" }),
      result(nodeSeries("node-a", "cpu", [1, 5]), nodeSeries("node-a", "mem", [3, 4])))
    expect(byRefId.series.map((value) => [value.label, value.points.map((point) => point.value)])).toEqual([
      ["ref-node-a-cpu", [1, 5]], ["ref-node-a-mem", [3, 4]],
    ])
    const byAggregation = applyPanelTransformations(panel({ kind: "group_by", group_by: "aggregation", calculation: "sum" }),
      result(nodeSeries("node-a", "cpu", [1, 2]), nodeSeries("node-b", "cpu", [3, 4])))
    expect(byAggregation.series.map((value) => [value.label, value.points.map((point) => point.value)])).toEqual([["avg", [4, 6]]])
  })

  it("treats an unknown field or calculation as a no-op and composes with the earlier steps", () => {
    const original = result(nodeSeries("node-a", "cpu", [1]), nodeSeries("node-b", "cpu", [2]))
    expect(applyPanelTransformations(panel({ kind: "group_by" }), original).series).toBe(original.series)
    expect(applyPanelTransformations(panel({ kind: "group_by", group_by: "label" as "metric", calculation: "sum" }), original).series).toBe(original.series)
    expect(applyPanelTransformations(panel({ kind: "group_by", group_by: "metric", calculation: "delta" as "sum" }), original).series).toBe(original.series)
    const chained = applyPanelTransformations(panel(
      { kind: "group_by", group_by: "metric", calculation: "sum" },
      { kind: "sort_by", statistic: "max", order: "desc" },
      { kind: "limit", count: 1 },
    ), result(nodeSeries("node-a", "mem", [1, 1]), nodeSeries("node-b", "mem", [5, 5]), nodeSeries("node-a", "cpu", [9, 9])))
    expect(chained.series.map((value) => [value.label, value.points.map((point) => point.value)])).toEqual([["cpu", [9, 9]]])
  })
})

describe("workspace binary operation transformation", () => {
  const twoSeries = () => result(series("used", [50, 25, 10, null], "used"), series("total", [100, 50, 0, 40], "total"))

  it("appends the aligned result as a new series", () => {
    const applied = applyPanelTransformations(panel({ kind: "binary_operation", left: "used", operator: "/", right: "total", alias: "usage" }), twoSeries())
    expect(applied.series.map((value) => [value.ref_id, value.label])).toEqual([
      ["used", "used"], ["total", "total"], ["binary_operation_2", "usage"],
    ])
    // 除数为零与左值缺样本的样本被跳过，剩下的仍按时间戳升序排列。
    expect(applied.series[2].points).toEqual([
      { timestamp: 1_700_000_000_000, value: 0.5 },
      { timestamp: 1_700_000_030_000, value: 0.5 },
    ])
    expect(applied.series[2].state).toBe("ok")
    expect(applied.series[2].unit).toBe("percent")
  })

  it("supports every documented operator and derives the label when no alias is set", () => {
    const cases: [DashboardTransformation["operator"], number][] = [["+", 9], ["-", 5], ["*", 14], ["/", 3.5]]
    for (const [operator, expected] of cases) {
      const applied = applyPanelTransformations(panel({ kind: "binary_operation", left: "a", operator, right: "b" }),
        result(series("A", [7], "a"), series("B", [2], "b")))
      expect([operator, applied.series[2].label, applied.series[2].points.map((point) => point.value)]).toEqual([operator, `a ${operator} b`, [expected]])
    }
  })

  it("matches a regular expression selector against the first hit", () => {
    const applied = applyPanelTransformations(panel({ kind: "binary_operation", left: "/^node-/", operator: "+", right: "B" }),
      result(series("A", [1], "node-a"), series("C", [100], "node-b"), series("B", [2], "B")))
    expect(applied.series.at(-1)?.points.map((point) => point.value)).toEqual([3])
  })

  it("keeps the chain when a selector misses, a pattern is invalid or the samples never overlap", () => {
    const missed = applyPanelTransformations(panel({ kind: "binary_operation", left: "missing", operator: "+", right: "total" }), twoSeries())
    expect(missed.series).toHaveLength(2)
    const broken = applyPanelTransformations(panel({ kind: "binary_operation", left: "/(/", operator: "+", right: "total" }), twoSeries())
    expect(broken.series).toHaveLength(2)
    const empty = applyPanelTransformations(panel({ kind: "binary_operation", left: "used", operator: "+", right: "total" }),
      result(series("used", [null], "used"), series("total", [1], "total")))
    expect(empty.series[2].state).toBe("empty")
    expect(empty.series[2].points).toEqual([])
  })
})

describe("workspace label filter transformation", () => {
  const labelled = () => result(series("A", [1], "node-a"), series("B", [2], "node-b"), series("C", [3], "db-primary"))

  it("keeps the series whose label matches the pattern", () => {
    const filtered = applyPanelTransformations(panel({ kind: "filter_by_label", pattern: "^node-" }), labelled())
    expect(filtered.series.map((value) => value.label)).toEqual(["node-a", "node-b"])
  })

  it("inverts the match set when requested", () => {
    const filtered = applyPanelTransformations(panel({ kind: "filter_by_label", pattern: "^node-", invert: true }), labelled())
    expect(filtered.series.map((value) => value.label)).toEqual(["db-primary"])
  })

  it("returns the original chain when the pattern is missing or invalid", () => {
    expect(applyPanelTransformations(panel({ kind: "filter_by_label" }), labelled()).series).toHaveLength(3)
    expect(applyPanelTransformations(panel({ kind: "filter_by_label", pattern: "([" }), labelled()).series).toHaveLength(3)
  })

  it("starts a new label filter from a usable pattern", () => {
    expect(emptyTransformation("filter_by_label")).toEqual({ kind: "filter_by_label", pattern: "^node-" })
  })
})

describe("workspace result frames", () => {
  function nodeSeries(node: string, metric: string, values: (number | null)[]): MetricResultSeries {
    return {
      ref_id: `ref-${node}-${metric}`, node_id: node, metric, aggregation: "avg", label: `${node} ${metric}`, unit: "percent",
      available_from: null, state: "ok",
      points: values.map((value, index) => ({ timestamp: 1_700_000_000_000 + index * 30_000, value })),
    }
  }

  it("builds one row per field and one column per calculation for series to rows", () => {
    const outcome = applyPanelTransformationsWithFrame(panel({ kind: "reduce", mode: "series_to_rows", calculations: ["min", "max"] }),
      result(series("A", [3, 1], "CPU"), series("B", [5, null], "Memory")))
    expect(outcome.frame?.kind).toBe("series_to_rows")
    expect(outcome.frame?.columns.map((column) => [column.key, column.labelKey, column.value])).toEqual([
      ["field", "workspace.frame_field", false],
      ["calculation_min", "workspace.calculation_min", true],
      ["calculation_max", "workspace.calculation_max", true],
    ])
    expect(outcome.frame?.rows.map((row) => [row.cells[0].text, row.cells[1].value, row.cells[2].value]))
      .toEqual([["CPU", 1, 3], ["Memory", 5, 5]])
    // 帧与序列形态同时产出：折线面板照旧按序列渲染。
    expect(outcome.result.series.map((value) => value.ref_id)).toEqual(["reduce_0_min", "reduce_0_max", "reduce_1_min", "reduce_1_max"])
  })

  it("aggregates the whole window per group for group by frames", () => {
    const outcome = applyPanelTransformationsWithFrame(panel({ kind: "group_by", group_by: "metric", calculations: ["mean", "count"] }),
      result(nodeSeries("node-a", "cpu", [1, 2]), nodeSeries("node-b", "cpu", [3, null]), nodeSeries("node-a", "mem", [10, 20])))
    expect(outcome.frame?.kind).toBe("group_by")
    expect(outcome.frame?.columns.map((column) => [column.key, column.labelKey, column.calculationKey])).toEqual([
      ["group_metric", "workspace.transformation_group_field_metric", undefined],
      ["calculation_mean", "workspace.transformation_group_field_metric", "workspace.calculation_mean"],
      ["calculation_count", "workspace.transformation_group_field_metric", "workspace.calculation_count"],
    ])
    // 一行一个分组：cpu 的均值是 (1+2+3)/3，count 只数有效样本（null 不计入）。
    expect(outcome.frame?.rows.map((row) => [row.cells[0].text, row.cells[1].value, row.cells[2].value]))
      .toEqual([["cpu", 2, 3], ["mem", 15, 2]])
    // 序列形态仍是按时间点聚合，图表因此不会退回「一条水平线」。
    expect(outcome.result.series[0].points.map((point) => point.value)).toEqual([2, 2])
  })

  it("keeps the alias as the group key of a frame row when it is set", () => {
    const outcome = applyPanelTransformationsWithFrame(panel({ kind: "group_by", group_by: "node_id", calculations: ["max"], alias: "节点" }),
      result(nodeSeries("node-a", "cpu", [1, 4])))
    expect(outcome.frame?.rows.map((row) => row.cells[0].text)).toEqual(["节点"])
  })

  it("drops the frame when a later step still rewrites the series list", () => {
    const outcome = applyPanelTransformationsWithFrame(panel({ kind: "reduce", mode: "series_to_rows", calculations: ["mean"] }, { kind: "rename_by_regex", pattern: "CPU", replacement: "处理器" }),
      result(series("A", [1], "CPU"), series("B", [2], "Memory")))
    expect(outcome.frame).toBeNull()
    expect(outcome.result.series.map((value) => value.label)).toEqual(["处理器 · mean", "Memory · mean"])
  })

  it("sorts frame rows by the calculation column named after the statistic", () => {
    const outcome = applyPanelTransformationsWithFrame(panel(
      { kind: "reduce", mode: "series_to_rows", calculations: ["min", "max"] },
      { kind: "sort_by", statistic: "max", order: "desc" },
    ), result(series("A", [3, 1], "CPU"), series("B", [5, 4], "Memory")))
    expect(outcome.frame?.kind).toBe("series_to_rows")
    // 一行一个字段，按 max 列降序；序列形态同步按同一依据重排，图表图例顺序一致。
    expect(outcome.frame?.rows.map((row) => [row.cells[0].text, row.cells[1].value, row.cells[2].value]))
      .toEqual([["Memory", 4, 5], ["CPU", 1, 3]])
    expect(outcome.result.series.map((value) => value.ref_id).slice(0, 2)).toEqual(["reduce_1_max", "reduce_1_min"])
  })

  it("keeps missing frame cells last and ties stable", () => {
    const outcome = applyPanelTransformationsWithFrame(panel(
      { kind: "reduce", mode: "series_to_rows", calculations: ["mean"] },
      { kind: "sort_by", statistic: "mean", order: "asc" },
    ), result(series("A", [2], "CPU"), series("B", [null], "Offline"), series("C", [2], "Memory")))
    expect(outcome.frame?.rows.map((row) => row.cells[0].text)).toEqual(["CPU", "Memory", "Offline"])
  })

  it("uses the only value column when the statistic names no column", () => {
    const outcome = applyPanelTransformationsWithFrame(panel(
      { kind: "reduce", mode: "series_to_rows", calculations: ["mean"] },
      { kind: "sort_by", statistic: "count", order: "desc" },
    ), result(series("A", [1], "CPU"), series("B", [7], "Memory")))
    expect(outcome.frame?.rows.map((row) => row.cells[0].text)).toEqual(["Memory", "CPU"])
  })

  it("keeps the produced order when several value columns match no statistic", () => {
    const outcome = applyPanelTransformationsWithFrame(panel(
      { kind: "reduce", mode: "series_to_rows", calculations: ["min", "max"] },
      { kind: "sort_by", statistic: "count", order: "desc" },
    ), result(series("A", [1, 9], "CPU"), series("B", [5, 6], "Memory")))
    expect(outcome.frame?.rows.map((row) => row.cells[0].text)).toEqual(["CPU", "Memory"])
  })

  it("sorts group by frames by the matching calculation column", () => {
    const outcome = applyPanelTransformationsWithFrame(panel(
      { kind: "group_by", group_by: "metric", calculations: ["mean", "count"] },
      { kind: "sort_by", statistic: "count", order: "desc" },
    ), result(nodeSeries("node-a", "cpu", [1, 2]), nodeSeries("node-b", "cpu", [3, null]), nodeSeries("node-a", "mem", [10, 20])))
    expect(outcome.frame?.rows.map((row) => [row.cells[0].text, row.cells[2].value])).toEqual([["cpu", 3], ["mem", 2]])
  })

  it("truncates frame rows and keeps the frame through row-only steps", () => {
    const limited = applyPanelTransformationsWithFrame(panel(
      { kind: "reduce", mode: "series_to_rows", calculations: ["mean"] },
      { kind: "limit", count: 1 },
    ), result(series("A", [1], "CPU"), series("B", [2], "Memory")))
    expect(limited.frame?.rows.map((row) => row.cells[0].text)).toEqual(["CPU"])
    expect(limited.result.series).toHaveLength(1)

    const chained = applyPanelTransformationsWithFrame(panel(
      { kind: "reduce", mode: "series_to_rows", calculations: ["mean"] },
      { kind: "sort_by", statistic: "mean", order: "desc" },
      { kind: "limit", count: 2 },
    ), result(series("A", [1], "CPU"), series("B", [3], "Memory"), series("C", [2], "Disk")))
    expect(chained.frame?.rows.map((row) => [row.cells[0].text, row.cells[1].value])).toEqual([["Memory", 3], ["Disk", 2]])
    expect(chained.result.series.map((value) => value.label)).toEqual(["Memory · mean", "Disk · mean"])
  })

  it("keeps the reduce default unframed and frames the historical single-calculation group by", () => {
    // reduce_fields 保留帧结构，只有 Series to rows 才有帧形态。
    expect(applyPanelTransformationsWithFrame(panel({ kind: "reduce", calculations: ["mean"] }), result(series("A", [1]))).frame).toBeNull()
    // Group by 无论写单个 calculation 还是多个 calculations，输出都是 Grafana 的一行一个分组。
    const historical = applyPanelTransformationsWithFrame(panel({ kind: "group_by", group_by: "metric", calculation: "sum" }), result(series("A", [1])))
    expect(historical.frame?.columns.map((column) => [column.key, column.calculationKey])).toEqual([
      ["group_metric", undefined], ["calculation_sum", "workspace.calculation_sum"],
    ])
    expect(historical.frame?.rows.map((row) => [row.cells[0].text, row.cells[1].value])).toEqual([["cpu", 1]])
  })
})
