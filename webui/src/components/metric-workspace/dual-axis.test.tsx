import { render, screen } from "@testing-library/react"
import type { ReactNode } from "react"
import { describe, expect, it, vi } from "vitest"
import { LocaleProvider } from "@/context/locale"
import { workspaceDashboard, workspaceResult } from "@/test/metric-workspace-fixtures"
import { WorkspaceBarChart } from "./bar-chart"
import { WorkspaceChart } from "./chart"
import type { DashboardOverride, DashboardPanel, MetricQueryResult } from "@/types/metric-workspace"

// jsdom 里 recharts 不画图，这里用替身断言每根轴与每条序列绑定的轴、堆叠分组。
vi.mock("recharts", () => ({
  ResponsiveContainer: ({ children }: { children: ReactNode }) => <div>{children}</div>,
  ComposedChart: ({ children }: { children: ReactNode }) => <div>{children}</div>,
  BarChart: ({ children }: { children: ReactNode }) => <div>{children}</div>,
  XAxis: () => null, CartesianGrid: () => null, Tooltip: () => null, ReferenceArea: () => null, ReferenceLine: () => null, Rectangle: () => null,
  Line: (props: Record<string, unknown>) => <div data-testid="series" data-key={String(props.dataKey)} data-axis={String(props.yAxisId)} data-stack={String(props.stackId ?? "")} />,
  Area: (props: Record<string, unknown>) => <div data-testid="series" data-key={String(props.dataKey)} data-axis={String(props.yAxisId)} data-stack={String(props.stackId ?? "")} />,
  Bar: (props: Record<string, unknown>) => <div data-testid="series" data-key={String(props.dataKey)} data-axis={String(props.yAxisId)} data-stack={String(props.stackId ?? "")} />,
  YAxis: (props: Record<string, unknown>) => <div data-testid="axis" data-side={String(props.orientation)} data-axis={String(props.yAxisId)}
    data-width={String(props.width)} data-label={String((props.label as { value?: string } | undefined)?.value ?? "")} />,
}))

/** 两条序列：CPU usage 在左轴，Memory usage 由覆盖挪到右轴。 */
function twoSeries(): MetricQueryResult {
  const base = workspaceResult()
  return { ...base, series: [...base.series,
    { ...base.series[0], ref_id: "B", label: "Memory usage", unit: "bytes" }] }
}

const memoryToRight: DashboardOverride = { matcher: { type: "by_name", value: "Memory usage" }, axis_placement: "right" }
const everythingToLeft: DashboardOverride = { matcher: { type: "by_regex", value: "." }, axis_placement: "left" }
const cpuToLeft: DashboardOverride = { matcher: { type: "by_name", value: "CPU usage" }, axis_placement: "left" }

function draw(change: Partial<DashboardPanel>, result = twoSeries()) {
  const panel = { ...workspaceDashboard().definition.panels[0], thresholds: [], ...change }
  render(<LocaleProvider><WorkspaceChart panel={panel} result={result} onRange={vi.fn()} /></LocaleProvider>)
}

function drawnAxes() {
  return screen.getAllByTestId("axis").map((axis) => [axis.getAttribute("data-side"), axis.getAttribute("data-label")])
}

function drawnSeries() {
  return screen.getAllByTestId("series").map((entry) => [entry.getAttribute("data-key"), entry.getAttribute("data-axis"), entry.getAttribute("data-stack")])
}

describe("workspace series axis placement", () => {
  it("keeps one left axis and every series on it until an override says otherwise", () => {
    draw({})
    expect(drawnAxes()).toEqual([["left", ""]])
    expect(drawnSeries().map((entry) => entry[1])).toEqual(["left", "left"])
  })

  it("draws a second axis on the right and binds only the moved series to it", () => {
    draw({ overrides: [memoryToRight] })
    expect(drawnAxes().map((entry) => entry[0])).toEqual(["left", "right"])
    expect(drawnSeries()).toEqual([["A", "left", ""], ["B", "right", ""]])
  })

  it("keeps the panel axis label on the side the panel declares", () => {
    draw({ axis_label: "Requests", overrides: [memoryToRight] })
    expect(drawnAxes()).toEqual([["left", "Requests"], ["right", ""]])
    // 面板声明右轴、只有显式覆盖到左轴的序列留在左轴时，轴标签仍然跟着面板声明的右侧走。
    draw({ axis_label: "Requests", axis_placement: "right", overrides: [cpuToLeft] })
    expect(drawnAxes().slice(-2)).toEqual([["left", ""], ["right", "Requests"]])
  })

  it("falls back to a single axis when every series is moved to the other side", () => {
    draw({ axis_placement: "right", overrides: [everythingToLeft] })
    expect(drawnAxes()).toEqual([["left", ""]])
    expect(drawnSeries().map((entry) => entry[1])).toEqual(["left", "left"])
  })

  it("stacks each axis on its own so the two sides never share a stack", () => {
    draw({ stack: "normal", overrides: [memoryToRight] })
    expect(drawnSeries().map((entry) => entry[2])).toEqual(["beat-stack-left", "beat-stack-right"])
  })

  it("applies the same split to bar chart panels", () => {
    const panel = { ...workspaceDashboard().definition.panels[0], type: "barchart" as const, thresholds: [], overrides: [memoryToRight] }
    render(<LocaleProvider><WorkspaceBarChart panel={panel} result={twoSeries()} onRange={vi.fn()} /></LocaleProvider>)
    expect(drawnAxes().map((entry) => entry[0])).toEqual(["left", "right"])
    expect(drawnSeries().map((entry) => entry[1])).toEqual(["left", "right"])
  })
})
