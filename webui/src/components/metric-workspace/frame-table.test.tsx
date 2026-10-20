import { render } from "@testing-library/react"
import { describe, expect, it, vi } from "vitest"
import { LocaleProvider } from "@/context/locale"
import { WorkspaceDataView } from "./data-view"
import type { DashboardPanel, MetricQueryResult, MetricResultSeries } from "@/types/metric-workspace"

function series(refId: string, node: string, metric: string, values: (number | null)[]): MetricResultSeries {
  return { ref_id: refId, node_id: node, metric, aggregation: "avg", label: `${metric} on ${node}`, unit: "percent",
    available_from: null, state: "ok", points: values.map((value, index) => ({ timestamp: 1_700_000_000_000 + index * 30_000, value })) }
}

function result(): MetricQueryResult {
  return { from: "2026-10-18T00:00:00.000Z", to: "2026-10-18T01:00:00.000Z", interval_ms: 30000,
    series: [series("A", "node-a", "cpu", [10, 20]), series("B", "node-b", "cpu", [30, 40]), series("C", "node-a", "memory", [5, 15])] }
}

function panel(patch: Partial<DashboardPanel> = {}): DashboardPanel {
  return { id: "fleet", title: "Fleet", type: "table", size: "medium", legend: false, thresholds: [],
    series: [{ ref_id: "A", node_id: "$node", metric: "cpu", aggregation: "avg", label: "cpu", color: "blue" }], ...patch }
}

function draw(patch: Partial<DashboardPanel>) {
  return render(<LocaleProvider><WorkspaceDataView panel={panel(patch)} result={result()} onRange={vi.fn()} /></LocaleProvider>)
}

function tableText(container: HTMLElement) {
  const headers = [...container.querySelectorAll("thead th")].map((node) => node.textContent)
  const rows = [...container.querySelectorAll("tbody tr")].map((row) => [...row.querySelectorAll("td")].map((cell) => cell.textContent))
  return { headers, rows }
}

describe("workspace result frame table", () => {
  it("renders series to rows as one row per field and one column per calculation", () => {
    const { container } = draw({ transformations: [{ kind: "reduce", mode: "series_to_rows", calculations: ["min", "max"] }] })
    const { headers, rows } = tableText(container)
    expect(headers).toEqual(["Field", "Minimum", "Maximum"])
    expect(rows).toEqual([["cpu on node-a", "10%", "20%"], ["cpu on node-b", "30%", "40%"], ["memory on node-a", "5%", "15%"]])
  })

  it("renders group by as one row per group with a column per calculation", () => {
    const { container } = draw({ transformations: [{ kind: "group_by", group_by: "metric", calculations: ["mean", "count"] }] })
    const { headers, rows } = tableText(container)
    expect(headers).toEqual(["Metric", "Metric (Average)", "Metric (Sample count)"])
    expect(rows).toEqual([["cpu", "25%", "4"], ["memory", "10%", "2"]])
  })

  it("falls back to the wide layout when a later step still rewrites the series list", () => {
    const { container } = draw({ table_orientation: "wide", transformations: [{ kind: "group_by", group_by: "metric", calculations: ["mean"] }, { kind: "rename_by_regex", pattern: "cpu", replacement: "processor" }] })
    const { headers } = tableText(container)
    expect(headers[0]).toBe("Timestamp (UTC)")
    expect(headers).toEqual(["Timestamp (UTC)", "processor", "memory"])
  })

  it("keeps the frame table after sorting and limiting the rows", () => {
    const { container } = draw({ transformations: [
      { kind: "reduce", mode: "series_to_rows", calculations: ["min", "max"] },
      { kind: "sort_by", statistic: "max", order: "desc" },
      { kind: "limit", count: 2 },
    ] })
    const { headers, rows } = tableText(container)
    expect(headers).toEqual(["Field", "Minimum", "Maximum"])
    expect(rows).toEqual([["cpu on node-b", "30%", "40%"], ["cpu on node-a", "10%", "20%"]])
  })
})
