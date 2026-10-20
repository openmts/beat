import { render } from "@testing-library/react"
import { describe, expect, it } from "vitest"
import { LocaleProvider } from "@/context/locale"
import type { DashboardPanel, MetricQueryResult } from "@/types/metric-workspace"
import { WorkspaceWideTable } from "./wide-table"

/** 两条序列共用 2 号时间点，各自另有一个独有时间点：宽表应当并成三行两列。 */
function result(): MetricQueryResult {
  const base = { node_id: "node-one", aggregation: "avg" as const, available_from: null, state: "ok" as const }
  return { from: "2026-10-18T00:00:00.000Z", to: "2026-10-18T01:00:00.000Z", interval_ms: 60000, series: [
    { ...base, ref_id: "A", metric: "cpu", label: "CPU", unit: "percent", points: [{ timestamp: 1000, value: 10 }, { timestamp: 2000, value: 20 }] },
    { ...base, ref_id: "B", metric: "memory", label: "Memory", unit: "bytes", points: [{ timestamp: 2000, value: 30 }] },
  ] }
}

function panel(patch: Partial<DashboardPanel> = {}): DashboardPanel {
  return { id: "fleet", title: "Fleet", type: "table", size: "medium", legend: false, thresholds: [], table_orientation: "wide",
    series: [
      { ref_id: "A", node_id: "$node", metric: "cpu", aggregation: "avg", label: "CPU", color: "blue" },
      { ref_id: "B", node_id: "$node", metric: "memory", aggregation: "avg", label: "Memory", color: "rose" },
    ], ...patch }
}

function draw(patch: Partial<DashboardPanel> = {}) {
  return render(<LocaleProvider><WorkspaceWideTable panel={panel(patch)} result={result()} /></LocaleProvider>)
}

describe("workspace wide table", () => {
  it("draws one column per series and one row per timestamp", () => {
    const { container } = draw()
    expect([...container.querySelectorAll("thead th")].map((node) => node.textContent)).toEqual(["Timestamp (UTC)", "CPU", "Memory"])
    const rows = [...container.querySelectorAll("tbody tr")]
    expect(rows.map((row) => [...row.querySelectorAll("td")].map((cell) => cell.textContent)))
      .toEqual([["1970-01-01 00:00:01", "10%", "—"], ["1970-01-01 00:00:02", "20%", "30 B"]])
  })

  it("applies value mappings and per series threshold colors to the cells", () => {
    const { container } = draw({
      cell_display_mode: "color_background", value_mappings: [{ type: "value", value: 10, text: "idle", color: "green" }],
      thresholds: [{ value: 0, color: "green" }, { value: 25, color: "rose" }],
    })
    // 每行的第一格是时间轴，剩下的才是序列单元格。
    const cells = [...container.querySelectorAll("tbody tr")].map((row) => [...row.querySelectorAll("td")].slice(1))
    expect(cells.map((row) => row.map((cell) => cell.textContent))).toEqual([["idle", "—"], ["20%", "30 B"]])
    expect(cells[0][0].getAttribute("style")).toContain("rgb(22, 163, 74)")
    expect(cells[1][1].getAttribute("style")).toContain("rgb(225, 29, 72)")
  })

  it("exports the rendered rows with the localized header", () => {
    const { container } = draw()
    expect(container.textContent).toContain("Download table CSV")
  })
})
