import { render, screen } from "@testing-library/react"
import { describe, expect, it } from "vitest"
import { LocaleProvider } from "@/context/locale"
import { workspaceDashboard, workspaceResult } from "@/test/metric-workspace-fixtures"
import type { DashboardPanel, MetricQueryResult } from "@/types/metric-workspace"
import { WorkspaceHeatmap } from "./heatmap"

function panelOf(overrides: Partial<DashboardPanel> = {}): DashboardPanel {
  return { ...workspaceDashboard().definition.panels[0], type: "heatmap", ...overrides }
}

/** 固定读数：同一时间列里落两个不同取值区间，方便核对单元格数量与色标。 */
function countedResult(): MetricQueryResult {
  const result = workspaceResult()
  const [first] = result.series
  const start = first.points[0].timestamp
  // 两列、每列两个取值区间各一个样本：单元格式 2×2，图例统计到 4 个样本。
  return { ...result, series: [{ ...first, unit: "percent", points: [
    { timestamp: start, value: 10 }, { timestamp: start, value: 90 },
    { timestamp: start + 6000, value: 10 }, { timestamp: start + 6000, value: 90 },
  ] }] }
}

function cells(container: HTMLElement): Element[] {
  return Array.from(container.querySelectorAll("[data-heatmap-cell]"))
}

describe("workspace heatmap", () => {
  it("draws one cell per time column and value bucket", () => {
    const { container } = render(<LocaleProvider><WorkspaceHeatmap panel={panelOf({ heatmap_buckets: 2, min: 0, max: 100 })} result={countedResult()} /></LocaleProvider>)
    expect(screen.getByRole("img", { name: "CPU usage" })).toBeInTheDocument()
    expect(cells(container)).toHaveLength(2 * 2)
    expect(screen.getByText("Samples · 4")).toBeInTheDocument()
  })

  it("merges time columns down to the cell budget for long ranges", () => {
    const result = workspaceResult()
    const long: MetricQueryResult = { ...result, series: [{ ...result.series[0], points: Array.from({ length: 400 }, (_, index) => ({ timestamp: 1_000_000 + index * 1000, value: (index % 40) + 1 })) }] }
    const { container } = render(<LocaleProvider><WorkspaceHeatmap panel={panelOf({ heatmap_buckets: 64 })} result={long} /></LocaleProvider>)
    expect(cells(container).length).toBeLessThanOrEqual(3000)
  })

  it("shows the localized empty state when no sample is finite", () => {
    const result = workspaceResult()
    const empty: MetricQueryResult = { ...result, series: [{ ...result.series[0], points: result.series[0].points.map((point) => ({ ...point, value: null })) }] }
    const { container } = render(<LocaleProvider><WorkspaceHeatmap panel={panelOf()} result={empty} /></LocaleProvider>)
    expect(cells(container)).toHaveLength(0)
    expect(screen.getByText("No samples in this range")).toBeInTheDocument()
  })

  it("hides the colour scale when the legend is off", () => {
    const { container } = render(<LocaleProvider><WorkspaceHeatmap panel={panelOf({ legend: false })} result={countedResult()} /></LocaleProvider>)
    expect(container.textContent).not.toContain("Samples")
  })
})
