import { render, screen } from "@testing-library/react"
import { describe, expect, it } from "vitest"
import { LocaleProvider } from "@/context/locale"
import { workspaceDashboard, workspaceResult } from "@/test/metric-workspace-fixtures"
import type { DashboardPanel, MetricQueryResult } from "@/types/metric-workspace"
import { WorkspaceHistogram } from "./histogram"

function panelOf(overrides: Partial<DashboardPanel> = {}): DashboardPanel {
  return { ...workspaceDashboard().definition.panels[0], type: "histogram", ...overrides }
}

/** 固定读数：0..100 之间四个样本，配 4 个分桶正好每桶一个。 */
function countedResult(): MetricQueryResult {
  const result = workspaceResult()
  const [first] = result.series
  const start = first.points[0].timestamp
  return { ...result, series: [{ ...first, unit: "percent", points: [10, 35, 60, 85].map((value, index) => ({ timestamp: start + index * 6000, value })) }] }
}

function bars(container: HTMLElement): Element[] {
  return Array.from(container.querySelectorAll("[data-histogram-bar]"))
}

describe("workspace histogram", () => {
  it("draws one bucket column per value range and one bar per series", () => {
    const { container } = render(<LocaleProvider><WorkspaceHistogram panel={panelOf({ histogram_buckets: 4, min: 0, max: 100 })} result={countedResult()} /></LocaleProvider>)
    expect(screen.getByRole("img", { name: "CPU usage" })).toBeInTheDocument()
    expect(container.querySelectorAll("[data-histogram-bucket]")).toHaveLength(4)
    expect(bars(container)).toHaveLength(4)
    expect(screen.getByText("Samples · 4")).toBeInTheDocument()
  })

  it("merges every series into a single distribution when combine series is on", () => {
    const result = countedResult()
    const merged: MetricQueryResult = { ...result, series: [result.series[0], { ...result.series[0], ref_id: "B", label: "memory", points: result.series[0].points.map((point) => ({ ...point, value: 5 })) }] }
    const { container } = render(<LocaleProvider><WorkspaceHistogram panel={panelOf({ histogram_buckets: 2, min: 0, max: 100, histogram_merge: true })} result={merged} /></LocaleProvider>)
    expect(bars(container)).toHaveLength(2)
    expect(screen.getByText("Samples · 8")).toBeInTheDocument()
  })

  it("shows the localized empty state when no sample is finite", () => {
    const result = workspaceResult()
    const empty: MetricQueryResult = { ...result, series: [{ ...result.series[0], points: result.series[0].points.map((point) => ({ ...point, value: null })) }] }
    const { container } = render(<LocaleProvider><WorkspaceHistogram panel={panelOf()} result={empty} /></LocaleProvider>)
    expect(bars(container)).toHaveLength(0)
    expect(screen.getByText("No samples in this range")).toBeInTheDocument()
  })

  it("hides the legend when it is turned off", () => {
    const { container } = render(<LocaleProvider><WorkspaceHistogram panel={panelOf({ legend: false })} result={countedResult()} /></LocaleProvider>)
    expect(container.textContent).not.toContain("Samples")
  })
})
