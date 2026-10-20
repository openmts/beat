import { render, screen } from "@testing-library/react"
import type { ReactNode } from "react"
import { describe, expect, it, vi } from "vitest"
import { LocaleProvider } from "@/context/locale"
import { workspaceDashboard, workspaceResult } from "@/test/metric-workspace-fixtures"
import type { DashboardAnnotation } from "@/lib/metric-collaboration-api"
import { WorkspaceChart } from "./chart"

// 默认 jsdom 下 ResponsiveContainer 尺寸为 0，recharts 不渲染子节点，因此这里用轻量替身
// 断言注释标记确实按裁剪后的窗口传给了 ReferenceArea/ReferenceLine。
vi.mock("recharts", () => ({
  ResponsiveContainer: ({ children }: { children: ReactNode }) => <div>{children}</div>,
  LineChart: ({ children }: { children: ReactNode }) => <div>{children}</div>,
  ComposedChart: ({ children }: { children: ReactNode }) => <div data-testid="composed-chart">{children}</div>,
  Line: () => null, XAxis: () => null, YAxis: () => null, CartesianGrid: () => null, Tooltip: () => null,
  ReferenceArea: (props: Record<string, unknown>) => <div data-testid="reference-area" data-x1={String(props.x1)} data-x2={String(props.x2)} data-fill={String(props.fill)} />,
  ReferenceLine: (props: Record<string, unknown>) => <div data-testid="reference-line" data-x={String(props.x)} />,
}))

function annotation(overrides: Partial<DashboardAnnotation> = {}): DashboardAnnotation {
  return { id: "annotation-1", dashboard_id: "public-default", title: "Deploy 1.2", tags: [], color: "amber", visibility: "public",
    from: "2026-09-12T23:30:00.000Z", to: "2026-09-13T00:20:00.000Z",
    created_at: "2026-09-12T23:00:00.000Z", updated_at: "2026-09-12T23:00:00.000Z", ...overrides }
}

function chart(values: DashboardAnnotation[]) {
  const panel = { ...workspaceDashboard().definition.panels[0], thresholds: [] }
  return render(<LocaleProvider><WorkspaceChart panel={panel} result={workspaceResult()} annotations={values} onRange={vi.fn()} /></LocaleProvider>)
}

describe("workspace chart annotations", () => {
  it("draws a clipped band and a start line for each visible annotation", () => {
    chart([annotation()])
    const band = screen.getByTestId("reference-area")
    expect(band).toHaveAttribute("data-x1", String(Date.parse("2026-09-13T00:00:00.000Z")))
    expect(band).toHaveAttribute("data-x2", String(Date.parse("2026-09-13T00:20:00.000Z")))
    expect(band).toHaveAttribute("data-fill", "#d97706")
    expect(screen.getByTestId("reference-line")).toHaveAttribute("data-x", String(Date.parse("2026-09-13T00:00:00.000Z")))
  })

  it("omits annotations belonging to another panel", () => {
    chart([annotation({ panel_id: "disk" })])
    expect(screen.queryByTestId("reference-area")).not.toBeInTheDocument()
  })
})
