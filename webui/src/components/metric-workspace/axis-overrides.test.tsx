import { render, screen } from "@testing-library/react"
import type { ReactNode } from "react"
import { describe, expect, it, vi } from "vitest"
import { LocaleProvider } from "@/context/locale"
import { workspaceDashboard, workspaceResult } from "@/test/metric-workspace-fixtures"
import { WorkspaceChart } from "./chart"
import type { DashboardPanel, MetricQueryResult } from "@/types/metric-workspace"

// recharts 在 jsdom 里没有尺寸，这里用替身把值轴的 domain 与标题还原成可断言的属性。
vi.mock("recharts", () => ({
  ResponsiveContainer: ({ children }: { children: ReactNode }) => <div>{children}</div>,
  ComposedChart: ({ children }: { children: ReactNode }) => <div>{children}</div>,
  XAxis: () => null, CartesianGrid: () => null, Tooltip: () => null, ReferenceArea: () => null, ReferenceLine: () => null,
  Line: (props: Record<string, unknown>) => <div data-testid="line" data-key={String(props.dataKey)} data-axis={String(props.yAxisId)} />,
  Area: () => null,
  YAxis: (props: Record<string, unknown>) => {
    const domain = props.domain as [unknown, unknown] | undefined
    // 柔性上下限是 (dataMin|dataMax) => number 的函数，用样本值 5 还原成端点。
    const resolve = (value: unknown) => typeof value === "function" ? String((value as (sample: number) => number)(5)) : String(value)
    return <div data-testid={`axis-${String(props.yAxisId)}`} data-min={resolve(domain?.[0])} data-max={resolve(domain?.[1])}
      data-label={String((props.label as { value?: string } | undefined)?.value ?? "")} />
  },
}))

/** 两份查询：A 是 CPU（左轴），B 是内存（被覆盖挪到右轴），用来验证逐序列轴选项落在各自那根轴上。 */
function dualResult(): MetricQueryResult {
  const base = workspaceResult()
  return { ...base, series: [base.series[0], {
    ...base.series[0], ref_id: "B", metric: "memory", label: "Memory usage", unit: "bytes",
    points: base.series[0].points.map((point) => ({ timestamp: point.timestamp, value: 80 })),
  }] }
}

function draw(change: Partial<DashboardPanel>) {
  const base = workspaceDashboard().definition.panels[0]
  const panel: DashboardPanel = { ...base, thresholds: [], series: [
    { ...base.series[0], label: "CPU" },
    { ref_id: "B", node_id: "$node", metric: "memory", aggregation: "avg", label: "Memory", color: "violet" },
  ], ...change }
  return render(<LocaleProvider><WorkspaceChart panel={panel} result={dualResult()} onRange={vi.fn()} /></LocaleProvider>)
}

describe("per series axis options", () => {
  it("labels each value axis and applies its soft limits", () => {
    draw({ overrides: [
      { matcher: { type: "by_name", value: "CPU usage" }, axis_label: "CPU %", axis_soft_min: 0, axis_soft_max: 100 },
      { matcher: { type: "by_name", value: "Memory usage" }, axis_placement: "right", axis_label: "Memory", axis_soft_min: 0, axis_soft_max: 1000 },
    ] })
    expect(screen.getByTestId("axis-left")).toHaveAttribute("data-label", "CPU %")
    expect(screen.getByTestId("axis-right")).toHaveAttribute("data-label", "Memory")
    expect(screen.getByTestId("axis-left")).toHaveAttribute("data-min", "0")
    expect(screen.getByTestId("axis-left")).toHaveAttribute("data-max", "100")
    expect(screen.getByTestId("axis-right")).toHaveAttribute("data-max", "1000")
  })

  it("keeps the other axis untouched when an override only matches one side", () => {
    draw({ overrides: [{ matcher: { type: "by_name", value: "Memory usage" }, axis_placement: "right", axis_soft_max: 1000 }] })
    expect(screen.getByTestId("axis-right")).toHaveAttribute("data-max", "1000")
    // 左轴没有覆盖命中：百分比单位缺省仍是 0..100，而不是被右轴的值带走。
    expect(screen.getByTestId("axis-left")).toHaveAttribute("data-max", "100")
    expect(screen.getByTestId("axis-left")).toHaveAttribute("data-label", "")
  })
})
