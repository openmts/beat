import { render, screen } from "@testing-library/react"
import type { ReactNode } from "react"
import { describe, expect, it, vi } from "vitest"
import { LocaleProvider } from "@/context/locale"
import { workspaceDashboard, workspaceResult } from "@/test/metric-workspace-fixtures"
import { WorkspaceChart } from "./chart"
import type { DashboardPanel } from "@/types/metric-workspace"

// recharts 在 jsdom 中尺寸为 0 不渲染子节点，这里用替身断言折线样式确实传到了渲染层。
vi.mock("recharts", () => ({
  ResponsiveContainer: ({ children }: { children: ReactNode }) => <div>{children}</div>,
  ComposedChart: ({ children }: { children: ReactNode }) => <div data-testid="composed">{children}</div>,
  // recharts 3 只在 AreaChart/ComposedChart 里渲染 Area；折线面板若退回 LineChart，「填充不透明度」会整条消失。
  LineChart: () => { throw new Error("time series panels must render inside ComposedChart") },
  XAxis: () => null, YAxis: () => null, CartesianGrid: () => null, Tooltip: () => null, ReferenceArea: () => null, ReferenceLine: () => null,
  Line: (props: Record<string, unknown>) => <div data-testid="line" data-curve={String(props.type)} data-width={String(props.strokeWidth)} data-dot={String((props.dot as { r?: number } | false) === false ? "none" : (props.dot as { r?: number }).r)} />,
  Area: (props: Record<string, unknown>) => <div data-testid="area" data-curve={String(props.type)} data-fill={String(props.fillOpacity)} data-color={String(props.fill)} data-stack={String(props.stackId ?? "")} data-dot={String((props.dot as { r?: number } | false) === false ? "none" : (props.dot as { r?: number }).r)} />,
}))

function draw(change: Partial<DashboardPanel>) {
  const panel = { ...workspaceDashboard().definition.panels[0], thresholds: [], ...change }
  return render(<LocaleProvider><WorkspaceChart panel={panel} result={workspaceResult()} onRange={vi.fn()} /></LocaleProvider>)
}

describe("workspace chart line style", () => {
  it("keeps the plain line and 1.8px points by default", () => {
    draw({})
    expect(screen.queryByTestId("area")).not.toBeInTheDocument()
    expect(screen.getByTestId("line")).toHaveAttribute("data-width", "2")
    expect(screen.getByTestId("line")).toHaveAttribute("data-dot", "1.8")
  })

  it("applies the configured width and point mode", () => {
    draw({ line_width: 4, show_points: "never" })
    expect(screen.getByTestId("line")).toHaveAttribute("data-width", "4")
    expect(screen.getByTestId("line")).toHaveAttribute("data-dot", "none")
    draw({ show_points: "always" })
    expect(screen.getAllByTestId("line")[1]).toHaveAttribute("data-dot", "2.6")
  })

  it("uses the configured interpolation curve for lines and areas", () => {
    // 用 container 而不是屏幕级查询：render 的查询绑定在 document.body 上，会串到同一文件里先前渲染的节点。
    const curves = (change: Partial<DashboardPanel>, testId: string) =>
      [...draw(change).container.querySelectorAll(`[data-testid="${testId}"]`)].map((node) => node.getAttribute("data-curve"))
    expect(curves({}, "line")).toEqual(["linear"])
    expect(curves({ line_interpolation: "smooth" }, "line")).toEqual(["monotone"])
    expect(curves({ line_interpolation: "step_before", fill_opacity: 20 }, "area")).toEqual(["stepBefore"])
    expect(curves({ line_interpolation: "step_after" }, "line")).toEqual(["stepAfter"])
  })

  it("draws the series inside a composed chart so the area fill can render", () => {
    draw({ fill_opacity: 20 })
    expect(screen.getByTestId("composed")).toBeInTheDocument()
  })

  it("fills and stacks the series when the panel asks for it", () => {
    const { container } = draw({ fill_opacity: 30 })
    expect(screen.getByTestId("area")).toHaveAttribute("data-fill", "0.3")
    expect(screen.getByTestId("area")).toHaveAttribute("data-color", "#3b82f6")
    expect(container.querySelector("defs")).toBeNull()
    draw({ stack: "normal", fill_opacity: 10 })
    // 堆叠按值轴分组：单轴面板的序列都落在左轴，堆叠组名带上侧别，双 Y 轴因此可以各自堆叠。
    expect(screen.getAllByTestId("area")[1]).toHaveAttribute("data-stack", "beat-stack-left")
  })

  it("applies the per series line width and fill opacity overrides", () => {
    const matcher = { type: "by_name" as const, value: "CPU usage" }
    const wider = draw({ line_width: 1, overrides: [{ matcher, line_width: 4 }] })
    expect(wider.container.querySelector('[data-testid="line"]')?.getAttribute("data-width")).toBe("4")
    // 覆盖把填充降到 0 后该序列退回纯折线，与面板级 fill_opacity=0 的行为一致。
    const flattened = draw({ fill_opacity: 30, overrides: [{ matcher: { type: "by_regex", value: "." }, fill_opacity: 0 }] })
    expect(flattened.container.querySelectorAll('[data-testid="area"]')).toHaveLength(0)
    expect(flattened.container.querySelectorAll('[data-testid="line"]')).toHaveLength(1)
    // 覆盖改过的填充同样用在渐变停靠点上：该序列从 10% 开始渐隐，而不是面板的 30%。
    const faded = draw({ fill_opacity: 30, gradient_mode: "opacity", overrides: [{ matcher, fill_opacity: 10 }] })
    expect([...faded.container.querySelectorAll("stop")].map((stop) => stop.getAttribute("stop-opacity"))).toEqual(["0.1", "0"])
  })

  it("paints the area with a per series gradient when the panel asks for it", () => {
    const { container } = draw({ fill_opacity: 30, gradient_mode: "hue" })
    const color = screen.getByTestId("area").getAttribute("data-color") ?? ""
    expect(color.startsWith("url(#beat-gradient-")).toBe(true)
    // 渐变模式把不透明度写进停靠点，整层填充保持不透明，否则渐变会被整体压暗。
    expect(screen.getByTestId("area")).toHaveAttribute("data-fill", "1")
    const stops = [...container.querySelectorAll("stop")].map((stop) => stop.getAttribute("stop-color"))
    expect(stops).toHaveLength(2)
    expect(stops[0]).toBe("#3b82f6")
    expect(stops[1]).not.toBe("#3b82f6")
  })
})
