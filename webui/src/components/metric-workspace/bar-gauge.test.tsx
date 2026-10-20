import { render, screen } from "@testing-library/react"
import { describe, expect, it } from "vitest"
import { LocaleProvider } from "@/context/locale"
import { barGaugeSegments } from "@/lib/workspace-bar-gauge"
import { workspaceDashboard, workspaceResult } from "@/test/metric-workspace-fixtures"
import type { DashboardPanel, MetricQueryResult } from "@/types/metric-workspace"
import { WorkspaceBarGauge } from "./bar-gauge"

function panelOf(overrides: Partial<DashboardPanel> = {}): DashboardPanel {
  return { ...workspaceDashboard().definition.panels[0], type: "bargauge", ...overrides }
}

/** 两根序列的固定读数：A=40%、B=90%，便于断言条长与阈值取色。 */
function twoSeries(): MetricQueryResult {
  const result = workspaceResult()
  const [first] = result.series
  const timestamps = first.points.map((point) => point.timestamp)
  return { ...result, series: [first, { ...first, ref_id: "B", label: "Memory usage", points: [
    { timestamp: timestamps[0], value: 90 }, { timestamp: timestamps[1], value: null },
  ] }] }
}

describe("workspace bar gauge", () => {
  it("draws one meter per series with the resolved label and value", () => {
    render(<LocaleProvider><WorkspaceBarGauge panel={panelOf()} result={twoSeries()} /></LocaleProvider>)
    const meters = screen.getAllByRole("meter")
    expect(meters).toHaveLength(2)
    expect(meters[0]).toHaveAttribute("aria-label", "CPU usage")
    expect(meters[0]).toHaveAttribute("aria-valuenow", "40")
    expect(meters[0]).toHaveAttribute("aria-valuemin", "0")
    expect(meters[0]).toHaveAttribute("aria-valuemax", "100")
    expect(screen.getByText("40%")).toBeInTheDocument()
    expect(screen.getByText("90%")).toBeInTheDocument()
  })

  it("scales percent panels to 100 and other units to the largest sample", () => {
    const percent = render(<LocaleProvider><WorkspaceBarGauge panel={panelOf()} result={workspaceResult()} /></LocaleProvider>)
    expect(percent.getByRole("meter")).toHaveAttribute("aria-valuemax", "100")
    percent.unmount()
    const counts = workspaceResult()
    render(<LocaleProvider><WorkspaceBarGauge panel={panelOf()} result={{ ...counts, series: counts.series.map((series) => ({ ...series, unit: "count" })) }} /></LocaleProvider>)
    expect(screen.getByRole("meter")).toHaveAttribute("aria-valuemax", "40")
  })

  it("lights one LCD cell per tenth of the filled range", () => {
    const { container } = render(<LocaleProvider><WorkspaceBarGauge panel={panelOf({ bar_display: "lcd" })} result={workspaceResult()} /></LocaleProvider>)
    const meter = screen.getByRole("meter")
    expect(meter).toHaveClass("flex-col-reverse")
    const cells = Array.from(meter.querySelectorAll("span"))
    expect(cells).toHaveLength(barGaugeSegments)
    const lit = cells.filter((cell) => (cell as HTMLElement).style.backgroundColor !== "transparent")
    expect(lit).toHaveLength(4)
    expect(container.textContent).toContain("40%")
  })

  it("drops the unfilled track and honours an explicit scale", () => {
    render(<LocaleProvider><WorkspaceBarGauge panel={panelOf({ bar_unfilled: false, min: 0, max: 200 })} result={workspaceResult()} /></LocaleProvider>)
    const meter = screen.getByRole("meter")
    expect(meter).toHaveAttribute("aria-valuemax", "200")
    expect(meter.className).not.toContain("bg-muted")
  })

  it("colours each meter from its own threshold band", () => {
    const panel = panelOf({ thresholds: [{ value: 0, color: "green" }], overrides: [
      { matcher: { type: "by_ref_id", value: "B" }, thresholds: [{ value: 0, color: "blue" }, { value: 85, color: "rose" }] },
    ] })
    render(<LocaleProvider><WorkspaceBarGauge panel={panel} result={twoSeries()} /></LocaleProvider>)
    // A=40% 落在面板阈值里，B=90% 用覆盖自己的色带。
    expect(screen.getByText("40%")).toHaveStyle({ color: "#16a34a" })
    expect(screen.getByText("90%")).toHaveStyle({ color: "#e11d48" })
  })

  it("lays multiple series out horizontally and applies value mappings", () => {
    const panel = panelOf({ bar_orientation: "auto", value_mappings: [{ type: "value", value: 40, text: "Healthy", color: "green" }] })
    render(<LocaleProvider><WorkspaceBarGauge panel={panel} result={twoSeries()} /></LocaleProvider>)
    expect(screen.getByText("Healthy")).toBeInTheDocument()
    const meters = screen.getAllByRole("meter")
    expect(meters[0].className).toContain("h-4")
    expect(meters[0].className).not.toContain("h-40")
  })
})
