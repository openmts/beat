import { render, screen } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { describe, expect, it, vi } from "vitest"
import { LocaleProvider } from "@/context/locale"
import { workspaceDashboard, workspaceResult } from "@/test/metric-workspace-fixtures"
import type { DashboardPanel, LegendSort, LegendValue, MetricQueryResult, MetricResultSeries } from "@/types/metric-workspace"
import { WorkspaceChart } from "./chart"

function panelWith(values: LegendValue[], sort: LegendSort = "none"): DashboardPanel {
  return { ...workspaceDashboard().definition.panels[0], legend_values: values, legend_sort: sort }
}

/** 每条序列追加一个已结束的采样点，让 last/mean 稳定可断言。 */
function series(label: string, refId: string, value: number): MetricResultSeries {
  const base = workspaceResult().series[0]
  return { ...base, ref_id: refId, label, points: [base.points[0], { timestamp: base.points[1].timestamp, value }] }
}

function resultOf(...values: [string, string, number][]): MetricQueryResult {
  return { ...workspaceResult(), series: values.map(([label, refId, value]) => series(label, refId, value)) }
}

function chart(panel: DashboardPanel, result: MetricQueryResult) {
  return render(<LocaleProvider><WorkspaceChart panel={panel} result={result} onRange={vi.fn()} /></LocaleProvider>)
}

describe("workspace legend placement", () => {
  it("keeps the legend under the plot by default", () => {
    const { container } = chart(panelWith([]), resultOf(["High", "A", 90]))
    expect((container.firstElementChild as HTMLElement).className).not.toContain("items-stretch")
    expect(container.querySelector(".flex-wrap")).toBeTruthy()
  })

  it("renders the legend as a right-hand column when the panel asks for it", () => {
    const { container } = chart({ ...panelWith([]), legend_placement: "right", legend: true }, resultOf(["High", "A", 90]))
    expect((container.firstElementChild as HTMLElement).className).toContain("items-stretch")
    const column = container.querySelector(".flex-col")
    expect(column?.className).toContain("w-40")
    expect(column?.textContent).toContain("High")
  })
})

describe("workspace legend values", () => {
  it("renders the collected statistics as a legend table", () => {
    chart(panelWith(["last", "mean"]), resultOf(["High", "A", 90]))
    expect(screen.getByRole("columnheader", { name: "Series" })).toBeInTheDocument()
    expect(screen.getByRole("columnheader", { name: "Last" })).toBeInTheDocument()
    expect(screen.getByRole("columnheader", { name: "Mean" })).toBeInTheDocument()
    const row = screen.getByRole("row", { name: /High/ })
    expect(row).toHaveTextContent("90%")
    expect(row).toHaveTextContent("65%")
  })

  it("keeps the inline legend when the panel collects no values", () => {
    chart(panelWith([]), resultOf(["High", "A", 90]))
    expect(screen.queryByRole("table")).not.toBeInTheDocument()
    expect(screen.getByRole("button", { name: "High" })).toBeInTheDocument()
  })

  it("orders legend rows by the first collected value", () => {
    chart(panelWith(["last"], "asc"), resultOf(["High", "A", 90], ["Low", "B", 10]))
    expect(screen.getAllByRole("row").slice(1).map((row) => row.textContent)).toEqual([
      expect.stringContaining("Low"), expect.stringContaining("High"),
    ])
  })

  it("hides and restores a series from the legend table", async () => {
    const user = userEvent.setup()
    chart(panelWith(["last"]), resultOf(["High", "A", 90], ["Low", "B", 10]))
    const entry = screen.getByRole("button", { name: "High" })
    expect(entry).toHaveAttribute("aria-pressed", "true")
    await user.click(entry)
    expect(entry).toHaveAttribute("aria-pressed", "false")
    await user.click(entry)
    expect(entry).toHaveAttribute("aria-pressed", "true")
  })
})
