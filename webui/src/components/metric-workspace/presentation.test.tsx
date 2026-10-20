import { render, screen } from "@testing-library/react"
import { describe, expect, it, vi } from "vitest"
import { LocaleProvider } from "@/context/locale"
import { workspaceDashboard, workspaceResult } from "@/test/metric-workspace-fixtures"
import type { DashboardAnnotation } from "@/lib/metric-collaboration-api"
import type { DashboardPanel, MetricQueryResult } from "@/types/metric-workspace"
import { seriesOverride } from "@/lib/workspace-overrides"
import { WorkspaceChart } from "./chart"
import { WorkspaceDataView } from "./data-view"
import { WorkspacePanel } from "./panel"

// 设计契约：长指标/标签可以截断，但必须保留完整 tooltip。
const longLabel = "Production host · CPU usage (average across the selected window)"

function panelOf(type: DashboardPanel["type"], legend = true): DashboardPanel {
  const panel = workspaceDashboard().definition.panels[0]
  return { ...panel, type, legend }
}

function longResult(): MetricQueryResult {
  const result = workspaceResult()
  return { ...result, series: [{ ...result.series[0], label: longLabel, state: "ok" }] }
}

describe("workspace label presentation", () => {
  it("keeps the full name on truncated legend entries", () => {
    render(<LocaleProvider><WorkspaceChart panel={panelOf("timeseries")} result={longResult()} onRange={vi.fn()} /></LocaleProvider>)
    const legend = screen.getByRole("button", { name: longLabel })
    expect(legend).toHaveAttribute("title", longLabel)
    expect(legend.className).toContain("truncate")
  })

  it("keeps the full name on truncated stat and table labels", () => {
    const stat = render(<LocaleProvider><WorkspaceDataView panel={panelOf("stat")} result={longResult()} onRange={vi.fn()} /></LocaleProvider>)
    expect(stat.getByText(longLabel)).toHaveAttribute("title", longLabel)
    stat.unmount()

    render(<LocaleProvider><WorkspaceDataView panel={panelOf("table")} result={longResult()} onRange={vi.fn()} /></LocaleProvider>)
    expect(screen.getAllByText(longLabel)[0]).toHaveAttribute("title", longLabel)
  })

  it("applies panel transformations before rendering", () => {
    const result = longResult()
    const three = { ...result, series: [
      { ...result.series[0], ref_id: "A", label: "node a" },
      { ...result.series[0], ref_id: "B", label: "node b" },
      { ...result.series[0], ref_id: "C", label: "node c" },
    ] }
    const renamed = { ...panelOf("table"), transformations: [
      { kind: "rename_by_regex" as const, pattern: "^node", replacement: "beat" },
      { kind: "limit" as const, count: 2 },
    ] }
    render(<LocaleProvider><WorkspaceDataView panel={renamed} result={three} onRange={vi.fn()} /></LocaleProvider>)
    expect(screen.queryByText("node a")).not.toBeInTheDocument()
    expect(screen.getAllByText("beat a").length).toBeGreaterThan(0)
    expect(screen.queryByText("beat c")).not.toBeInTheDocument()
  })

  it("hides overridden series from the legend while the panel keeps drawing them", () => {
    const result = longResult()
    const two = { ...result, series: [
      { ...result.series[0], ref_id: "A", label: "node a" },
      { ...result.series[0], ref_id: "B", label: "node b" },
    ] }
    const overridden = { ...panelOf("timeseries"), overrides: [
      { matcher: { type: "by_name" as const, value: "node b" }, hide_legend: true },
    ] }
    render(<LocaleProvider><WorkspaceChart panel={overridden} result={two} onRange={vi.fn()} /></LocaleProvider>)
    expect(screen.getByRole("button", { name: "node a" })).toBeInTheDocument()
    expect(screen.queryByRole("button", { name: "node b" })).not.toBeInTheDocument()
    // 覆盖只改显示：序列本身没有被过滤掉，图例之外的渲染仍然拿到两条序列。
    expect(seriesOverride(overridden, two.series[1])).toEqual({ hideLegend: true })
  })

  it("shows the empty state when a filter removes every series", () => {
    const filtered = { ...panelOf("table"), transformations: [{ kind: "filter_by_value" as const, comparison: "gt" as const, value: 9_999 }] }
    render(<LocaleProvider><WorkspaceDataView panel={filtered} result={longResult()} onRange={vi.fn()} /></LocaleProvider>)
    expect(screen.getByText("No samples in this range")).toBeInTheDocument()
  })

  it("routes bar gauge panels to the bar renderer", () => {
    render(<LocaleProvider><WorkspaceDataView panel={panelOf("bargauge")} result={longResult()} onRange={vi.fn()} /></LocaleProvider>)
    expect(screen.getByRole("meter")).toHaveAttribute("aria-label", longLabel)
  })

  it("routes pie chart panels to the slice renderer", () => {
    const { container } = render(<LocaleProvider><WorkspaceDataView panel={panelOf("piechart")} result={longResult()} onRange={vi.fn()} /></LocaleProvider>)
    expect(container.querySelectorAll("[data-slice]")).toHaveLength(1)
    expect(screen.getByRole("img", { name: "CPU usage" })).toBeInTheDocument()
    expect(screen.getByText(longLabel)).toBeInTheDocument()
  })

  it("routes state timeline panels to the state renderer", () => {
    const { container } = render(<LocaleProvider><WorkspaceDataView panel={panelOf("statetimeline")} result={longResult()} onRange={vi.fn()} /></LocaleProvider>)
    expect(screen.getByRole("img", { name: "CPU usage" })).toBeInTheDocument()
    expect(container.querySelectorAll("[data-timeline-row]")).toHaveLength(1)
    expect(container.querySelectorAll("[data-timeline-segment]").length).toBeGreaterThan(0)
    expect(screen.getByText(longLabel)).toBeInTheDocument()
  })

  it("routes histogram panels to the distribution renderer", () => {
    const { container } = render(<LocaleProvider><WorkspaceDataView panel={panelOf("histogram")} result={longResult()} onRange={vi.fn()} /></LocaleProvider>)
    expect(screen.getByRole("img", { name: "CPU usage" })).toBeInTheDocument()
    expect(container.querySelectorAll("[data-histogram-bucket]").length).toBeGreaterThan(0)
    expect(container.querySelectorAll("[data-histogram-bar]").length).toBeGreaterThan(0)
    expect(screen.getByText(longLabel)).toBeInTheDocument()
  })

  it("replaces stat and table values with the configured mappings", () => {
    const stat = { ...panelOf("stat"), thresholds: [{ value: 0, color: "blue" as const }],
      value_mappings: [{ type: "value" as const, value: 40, text: "Healthy", color: "green" as const }] }
    const first = render(<LocaleProvider><WorkspaceDataView panel={stat} result={longResult()} onRange={vi.fn()} /></LocaleProvider>)
    expect(first.getByText("Healthy")).toBeInTheDocument()
    expect(first.queryByText("%")).not.toBeInTheDocument()
    first.unmount()

    // 面板视图会丢掉全空的时间点，因此给同一个时间点补一条有值的序列，让空值映射仍然可见。
    const shared = longResult()
    const gapTimestamp = shared.series[0]!.points[1]!.timestamp
    shared.series = [...shared.series, { ...shared.series[0]!, ref_id: "B", points: [{ timestamp: gapTimestamp, value: 20 }] }]
    const table = { ...panelOf("table"), value_mappings: [{ type: "special" as const, match: "null" as const, text: "no data", color: "slate" as const }] }
    render(<LocaleProvider><WorkspaceDataView panel={table} result={shared} onRange={vi.fn()} /></LocaleProvider>)
    expect(screen.getByText("no data")).toBeInTheDocument()
  })

  it("colours a stat value from the threshold band of the matched series", () => {
    const stat = { ...panelOf("stat"), thresholds: [{ value: 0, color: "blue" as const }] }
    const plain = render(<LocaleProvider><WorkspaceDataView panel={stat} result={longResult()} onRange={vi.fn()} /></LocaleProvider>)
    expect(plain.getByText("40%")).toHaveStyle({ color: "#3b82f6" })
    plain.unmount()
    // 命中的序列用自己的色带取色：40 越过 30 的琥珀，但仍然低于 60 的玫瑰。
    const overridden = { ...stat, overrides: [
      { matcher: { type: "by_ref_id" as const, value: "A" }, thresholds: [{ value: 30, color: "amber" as const }, { value: 60, color: "rose" as const }] },
    ] }
    render(<LocaleProvider><WorkspaceDataView panel={overridden} result={longResult()} onRange={vi.fn()} /></LocaleProvider>)
    expect(screen.getByText("40%")).toHaveStyle({ color: "#d97706" })
  })

  it("marks in-range annotations on the panel timeline", () => {
    const marker = (overrides: Partial<DashboardAnnotation> = {}): DashboardAnnotation => ({
      id: "annotation-1", dashboard_id: "public-default", title: "Deploy 1.2", tags: [], color: "blue", visibility: "public",
      from: "2026-09-13T00:10:00.000Z", to: "2026-09-13T00:20:00.000Z",
      created_at: "2026-09-13T00:00:00.000Z", updated_at: "2026-09-13T00:00:00.000Z", ...overrides,
    })
    const other = marker({ id: "annotation-other", title: "Disk event", panel_id: "disk" })
    render(<LocaleProvider><WorkspaceChart panel={panelOf("timeseries")} result={longResult()}
      annotations={[marker(), other]} onRange={vi.fn()} /></LocaleProvider>)
    const list = screen.getByRole("list", { name: "CPU usage · annotations" })
    expect(list).toHaveTextContent("Deploy 1.2")
    expect(list).not.toHaveTextContent("Disk event")
  })

  it("keeps the full title on truncated panel headers", () => {
    const panel = { ...panelOf("timeseries"), title: longLabel }
    render(<LocaleProvider><WorkspacePanel panel={panel} state={{ loading: false }} inspect={vi.fn()} onRange={vi.fn()} readOnly /></LocaleProvider>)
    expect(screen.getByRole("heading", { name: longLabel })).toHaveAttribute("title", longLabel)
  })
})
