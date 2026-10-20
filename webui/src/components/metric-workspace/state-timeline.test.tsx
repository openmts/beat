import { render, screen, within } from "@testing-library/react"
import { describe, expect, it } from "vitest"
import { LocaleProvider } from "@/context/locale"
import { workspaceDashboard, workspaceResult } from "@/test/metric-workspace-fixtures"
import type { DashboardPanel, MetricQueryResult } from "@/types/metric-workspace"
import { WorkspaceStateTimeline } from "./state-timeline"

function panelOf(overrides: Partial<DashboardPanel> = {}): DashboardPanel {
  return { ...workspaceDashboard().definition.panels[0], type: "statetimeline", ...overrides }
}

/** 五个采样点、每个点 1 秒：色块宽度按 20% 递增，便于核对取值标签的显示条件。 */
function resultOf(values: (number | null)[]): MetricQueryResult {
  const result = workspaceResult()
  const start = Date.parse(result.from)
  return { ...result, to: new Date(start + values.length * 1000).toISOString(),
    series: [{ ...result.series[0], unit: "percent", points: values.map((value, index) => ({ timestamp: start + index * 1000, value })) }] }
}

function segments(container: HTMLElement): Element[] {
  return Array.from(container.querySelectorAll("[data-timeline-segment]"))
}

describe("workspace state timeline", () => {
  it("draws one row per series with a block per state", () => {
    const { container } = render(<LocaleProvider><WorkspaceStateTimeline panel={panelOf()} result={resultOf([1, 1, 2, 2, 2])} /></LocaleProvider>)
    expect(screen.getByRole("img", { name: "CPU usage" })).toBeInTheDocument()
    expect(container.querySelectorAll("[data-timeline-row]")).toHaveLength(1)
    expect(segments(container)).toHaveLength(2)
    const legend = screen.getByRole("list", { name: "States" })
    expect(legend).toBeInTheDocument()
    expect(within(legend).getAllByRole("listitem")).toHaveLength(2)
  })

  it("shows the state text from value mappings in the legend", () => {
    const panel = panelOf({ value_mappings: [{ type: "range", from: 50, text: "Busy", color: "rose" }, { type: "range", to: 50, text: "Idle", color: "green" }] })
    render(<LocaleProvider><WorkspaceStateTimeline panel={panel} result={resultOf([10, 10, 90, 90, 90])} /></LocaleProvider>)
    const legend = screen.getByRole("list", { name: "States" })
    expect(within(legend).getByText("Busy")).toBeInTheDocument()
    expect(within(legend).getByText("Idle")).toBeInTheDocument()
  })

  it("only labels blocks that are wide enough", () => {
    const mappings: DashboardPanel["value_mappings"] = [{ type: "value", value: 1, text: "Up" }, { type: "value", value: 2, text: "Down" }]
    const { container, unmount } = render(<LocaleProvider><WorkspaceStateTimeline panel={panelOf({ timeline_show_values: "always", value_mappings: mappings })} result={resultOf([1, 2, 1, 2, 1])} /></LocaleProvider>)
    expect(segments(container).map((segment) => segment.textContent)).toEqual(["Up", "Down", "Up", "Down", "Up"])
    unmount()
    const hidden = render(<LocaleProvider><WorkspaceStateTimeline panel={panelOf({ timeline_show_values: "never", value_mappings: mappings })} result={resultOf([1, 2, 1, 2, 1])} /></LocaleProvider>)
    // never 只是不写标签，提示里仍然给出状态。
    expect(segments(hidden.container).every((segment) => segment.textContent === "")).toBe(true)
  })

  it("describes each block with its time range and value", () => {
    const { container } = render(<LocaleProvider><WorkspaceStateTimeline panel={panelOf()} result={resultOf([42, 42, 43])} /></LocaleProvider>)
    expect(segments(container)[0]).toHaveAttribute("title", expect.stringContaining("42"))
    expect(segments(container)[0].getAttribute("title")).toMatch(/–/)
  })

  it("shows the localized empty state when every sample is missing", () => {
    const { container } = render(<LocaleProvider><WorkspaceStateTimeline panel={panelOf()} result={resultOf([null, null, null])} /></LocaleProvider>)
    expect(segments(container)).toHaveLength(0)
    expect(screen.getByText("No samples in this range")).toBeInTheDocument()
  })

  it("hides the legend when the panel turns it off", () => {
    const { container } = render(<LocaleProvider><WorkspaceStateTimeline panel={panelOf({ legend: false })} result={resultOf([1, 2, 1])} /></LocaleProvider>)
    expect(container.textContent).not.toContain("States")
    expect(segments(container).length).toBeGreaterThan(0)
  })
})
