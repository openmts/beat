import { render, screen } from "@testing-library/react"
import { describe, expect, it } from "vitest"
import { LocaleProvider } from "@/context/locale"
import { workspaceDashboard, workspaceResult } from "@/test/metric-workspace-fixtures"
import type { DashboardPanel, MetricQueryResult } from "@/types/metric-workspace"
import { WorkspacePieChart } from "./pie-chart"

function panelOf(overrides: Partial<DashboardPanel> = {}): DashboardPanel {
  return { ...workspaceDashboard().definition.panels[0], type: "piechart", ...overrides }
}

/** 两根序列的面板：A 保持蓝色，B 用琥珀色，便于核对每扇区取色。 */
function twoSeriesPanel(overrides: Partial<DashboardPanel> = {}): DashboardPanel {
  const panel = panelOf(overrides)
  return { ...panel, series: [...panel.series, { ...panel.series[0], ref_id: "B", color: "amber", label: "Memory usage" }] }
}

/** 两根序列的固定读数：A=40%、B=90%，占比为 4/13 与 9/13。 */
function twoSeries(): MetricQueryResult {
  const result = workspaceResult()
  const [first] = result.series
  const timestamps = first.points.map((point) => point.timestamp)
  return { ...result, series: [first, { ...first, ref_id: "B", label: "Memory usage", points: [
    { timestamp: timestamps[0], value: 90 }, { timestamp: timestamps[1], value: null },
  ] }] }
}

function slices(container: HTMLElement): Element[] {
  return Array.from(container.querySelectorAll("[data-slice]"))
}

describe("workspace pie chart", () => {
  it("draws one slice per series and labels each share", () => {
    const { container } = render(<LocaleProvider><WorkspacePieChart panel={twoSeriesPanel()} result={twoSeries()} /></LocaleProvider>)
    expect(slices(container)).toHaveLength(2)
    expect(screen.getByRole("img", { name: "CPU usage" })).toBeInTheDocument()
    expect(screen.getByRole("list", { name: "CPU usage · slice legend" })).toBeInTheDocument()
    expect(screen.getByText("CPU usage")).toBeInTheDocument()
    expect(screen.getByText("Memory usage")).toBeInTheDocument()
    expect(screen.getByText("30.8%")).toBeInTheDocument()
    expect(screen.getByText("69.2%")).toBeInTheDocument()
  })

  it("switches to value or combined labels without changing the shares", () => {
    const values = render(<LocaleProvider><WorkspacePieChart panel={twoSeriesPanel({ pie_label_mode: "value" })} result={twoSeries()} /></LocaleProvider>)
    expect(values.getByText("40%")).toBeInTheDocument()
    expect(values.getByText("90%")).toBeInTheDocument()
    values.unmount()
    render(<LocaleProvider><WorkspacePieChart panel={twoSeriesPanel({ pie_label_mode: "both" })} result={twoSeries()} /></LocaleProvider>)
    expect(screen.getByText("40% · 30.8%")).toBeInTheDocument()
    expect(screen.getByText("90% · 69.2%")).toBeInTheDocument()
  })

  it("closes the ring for a full-circle donut and keeps the wedge for a solid pie", () => {
    const donut = render(<LocaleProvider><WorkspacePieChart panel={panelOf({ pie_chart_type: "donut" })} result={workspaceResult()} /></LocaleProvider>)
    const ring = slices(donut.container)[0]
    expect(ring.tagName.toLowerCase()).toBe("circle")
    expect(ring).toHaveAttribute("fill", "none")
    expect(Number(ring.getAttribute("stroke-width"))).toBeGreaterThan(0)
    donut.unmount()
    const pie = render(<LocaleProvider><WorkspacePieChart panel={panelOf()} result={workspaceResult()} /></LocaleProvider>)
    expect(slices(pie.container)[0].tagName.toLowerCase()).toBe("circle")
    expect(slices(pie.container)[0]).toHaveAttribute("fill", "#3b82f6")
  })

  it("drops slices without a positive share but keeps their legend entries", () => {
    const result = workspaceResult()
    const zero = { ...result, series: [{ ...result.series[0], points: result.series[0].points.map((point) => ({ ...point, value: 0 })) }] }
    const { container } = render(<LocaleProvider><WorkspacePieChart panel={panelOf({ pie_label_mode: "both" })} result={zero} /></LocaleProvider>)
    expect(slices(container)).toHaveLength(0)
    expect(screen.getByText("0% · 0%")).toBeInTheDocument()
  })

  it("prefers value mappings for text and colour", () => {
    const panel = twoSeriesPanel({ pie_label_mode: "both", value_mappings: [{ type: "value", value: 40, text: "Healthy", color: "green" }] })
    const { container } = render(<LocaleProvider><WorkspacePieChart panel={panel} result={twoSeries()} /></LocaleProvider>)
    expect(screen.getByText("Healthy · 30.8%")).toBeInTheDocument()
    expect(slices(container)[0]).toHaveAttribute("fill", "#16a34a")
    expect(slices(container)[1]).toHaveAttribute("fill", "#d97706")
  })
})
