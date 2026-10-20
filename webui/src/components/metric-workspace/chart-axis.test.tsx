import { render, screen } from "@testing-library/react"
import type { ReactNode } from "react"
import { describe, expect, it, vi } from "vitest"
import { LocaleProvider } from "@/context/locale"
import { workspaceDashboard, workspaceResult } from "@/test/metric-workspace-fixtures"
import { WorkspaceChart } from "./chart"
import type { DashboardPanel } from "@/types/metric-workspace"

// 记录 YAxis 收到的坐标轴选项：jsdom 里 recharts 不画轴，只能断言传下去的配置。
vi.mock("recharts", () => ({
  ResponsiveContainer: ({ children }: { children: ReactNode }) => <div>{children}</div>,
  ComposedChart: ({ children }: { children: ReactNode }) => <div>{children}</div>,
  XAxis: () => null, Tooltip: () => null, ReferenceArea: () => null, ReferenceLine: () => null, Line: () => null, Area: () => null,
  CartesianGrid: () => <div data-testid="grid" />,
  YAxis: (props: Record<string, unknown>) => <div data-testid="y-axis" data-scale={String(props.scale)} data-orientation={String(props.orientation)}
    data-label={String((props.label as { value?: string } | undefined)?.value ?? "")} data-width={String(props.width)}
    data-domain={JSON.stringify(props.domain)} />,
}))

function draw(change: Partial<DashboardPanel>) {
  const panel = { ...workspaceDashboard().definition.panels[0], thresholds: [], ...change }
  render(<LocaleProvider><WorkspaceChart panel={panel} result={workspaceResult()} onRange={vi.fn()} /></LocaleProvider>)
  return screen.getAllByTestId("y-axis").at(-1)!
}

describe("workspace axis options", () => {
  it("keeps the linear scale and the percentage width by default", () => {
    const axis = draw({})
    expect(axis).toHaveAttribute("data-scale", "linear")
    expect(axis).toHaveAttribute("data-label", "")
    expect(axis).toHaveAttribute("data-width", "52")
  })

  it("renders the logarithmic scale and reserves room for the axis label", () => {
    const axis = draw({ scale: "log", axis_label: "CPU usage" })
    expect(axis).toHaveAttribute("data-scale", "log")
    expect(axis).toHaveAttribute("data-label", "CPU usage")
    expect(axis).toHaveAttribute("data-width", "70")
  })

  it("centers the axis domain around zero when asked", () => {
    const axis = draw({ centered_zero: true })
    const domain = JSON.parse(axis.getAttribute("data-domain") ?? "[]") as number[]
    expect(domain[0]).toBeLessThan(0)
    expect(domain[1]).toBe(-domain[0])
  })

  it("draws the grid and the left axis by default", () => {
    draw({})
    expect(screen.getAllByTestId("grid").length).toBeGreaterThan(0)
    expect(screen.getAllByTestId("y-axis").at(-1)).toHaveAttribute("data-orientation", "left")
  })

  it("drops the grid and moves the axis to the right when configured", () => {
    const axis = draw({ grid: false, axis_placement: "right" })
    expect(screen.queryAllByTestId("grid")).toHaveLength(0)
    expect(axis).toHaveAttribute("data-orientation", "right")
  })
})
