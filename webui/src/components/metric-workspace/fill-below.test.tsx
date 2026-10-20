import { render } from "@testing-library/react"
import type { ReactNode } from "react"
import { describe, expect, it, vi } from "vitest"
import { LocaleProvider } from "@/context/locale"
import { fillBelowBaseKey, fillBelowDeltaKey, fillBelowStackId } from "@/lib/workspace-fill-below"
import type { DashboardPanel, MetricQueryResult } from "@/types/metric-workspace"
import { WorkspaceChart } from "./chart"

// recharts 在 jsdom 里没有尺寸，这里用替身断言填充带确实按派生键传到了渲染层。
vi.mock("recharts", () => ({
  ResponsiveContainer: ({ children }: { children: ReactNode }) => <div>{children}</div>,
  ComposedChart: ({ children, data }: { children: ReactNode; data: Record<string, unknown>[] }) =>
    <div data-testid="composed" data-rows={JSON.stringify(data)}>{children}</div>,
  XAxis: () => null, YAxis: () => null, CartesianGrid: () => null, Tooltip: () => null, ReferenceArea: () => null, ReferenceLine: () => null,
  Line: (props: Record<string, unknown>) => <div data-testid="line" data-key={String(props.dataKey)} data-stack={String(props.stackId ?? "")} />,
  Area: (props: Record<string, unknown>) => <div data-testid="area" data-key={String(props.dataKey)} data-stack={String(props.stackId ?? "")}
    data-tooltip={String(props.tooltipType ?? "")} data-fill={String(props.fillOpacity)} data-color={String(props.fill)} />,
}))

/** 两条查询：A 是 CPU 下限、B 是上限；填充带应当从 A 延伸到 B。 */
function result(): MetricQueryResult {
  const base = { node_id: "node-one", metric: "cpu", aggregation: "avg" as const, unit: "percent", available_from: null, state: "ok" as const }
  return { from: "2026-09-13T00:00:00.000Z", to: "2026-09-13T01:00:00.000Z", interval_ms: 60000, series: [
    { ...base, ref_id: "A", label: "CPU min", points: [{ timestamp: 1, value: 10 }, { timestamp: 2, value: null }, { timestamp: 3, value: 30 }] },
    { ...base, ref_id: "B", label: "CPU max", points: [{ timestamp: 1, value: 4 }, { timestamp: 2, value: 8 }, { timestamp: 3, value: 12 }] },
  ] }
}

function panel(patch: Partial<DashboardPanel> = {}): DashboardPanel {
  return { id: "cpu", title: "CPU", type: "timeseries", size: "medium", legend: false, thresholds: [],
    series: [
      { ref_id: "A", node_id: "$node", metric: "cpu", aggregation: "avg", label: "CPU min", color: "blue" },
      { ref_id: "B", node_id: "$node", metric: "cpu", aggregation: "min", label: "CPU max", color: "rose" },
    ], ...patch }
}

function draw(patch: Partial<DashboardPanel> = {}) {
  return render(<LocaleProvider><WorkspaceChart panel={panel(patch)} result={result()} onRange={vi.fn()} /></LocaleProvider>)
}

describe("workspace fill below to", () => {
  it("draws the band with a hidden base layer and the series as the delta layer", () => {
    const { container } = draw({ overrides: [{ matcher: { type: "by_name", value: "CPU min" }, fill_below_to: "CPU max" }] })
    const areas = [...container.querySelectorAll('[data-testid="area"]')]
    expect(areas.map((node) => node.getAttribute("data-key"))).toEqual([fillBelowBaseKey("A"), fillBelowDeltaKey("A")])
    expect(areas.map((node) => node.getAttribute("data-stack"))).toEqual([fillBelowStackId("A"), fillBelowStackId("A")])
    // 基线层不进 tooltip；两条派生键共享一个堆叠组，差值层的上沿就是 A 自己的折线。
    expect(areas[0]).toHaveAttribute("data-tooltip", "none")
    expect(areas[1]).toHaveAttribute("data-tooltip", "")
    expect(areas[1]).toHaveAttribute("data-color", "#3b82f6")
    // 未被覆盖的 B 仍然按普通折线绘制。
    expect([...container.querySelectorAll('[data-testid="line"]')].map((node) => node.getAttribute("data-key"))).toEqual(["B"])
  })

  it("writes both derived keys and blanks them where a sample is missing", () => {
    const { container } = draw({ overrides: [{ matcher: { type: "by_name", value: "CPU min" }, fill_below_to: "CPU max" }] })
    const rows = JSON.parse(container.querySelector('[data-testid="composed"]')!.getAttribute("data-rows")!) as Record<string, unknown>[]
    expect(rows.map((row) => [row[fillBelowBaseKey("A")], row[fillBelowDeltaKey("A")]])).toEqual([[4, 6], [null, null], [12, 18]])
  })

  it("falls back to a readable fill and keeps untouched panels on the plain line", () => {
    const plain = draw()
    expect(plain.container.querySelector('[data-testid="area"]')).toBeNull()
    expect([...plain.container.querySelectorAll('[data-testid="line"]')].map((node) => node.getAttribute("data-key"))).toEqual(["A", "B"])
    plain.unmount()
    const { container } = draw({ overrides: [{ matcher: { type: "by_name", value: "CPU min" }, fill_below_to: "CPU max" }] })
    expect([...container.querySelectorAll('[data-testid="area"]')][1]).toHaveAttribute("data-fill", "0.1")
  })
})
