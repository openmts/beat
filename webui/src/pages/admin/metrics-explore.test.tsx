import type { ReactNode } from "react"
import { fireEvent, render, screen, waitFor } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { MemoryRouter } from "react-router"
import { beforeEach, describe, expect, it, vi } from "vitest"
import MetricsExplore from "@/pages/admin/metrics-explore"
import { LocaleProvider } from "@/context/locale"
import * as workspaceAPI from "@/lib/metric-workspace-api"
import { workspaceResult, workspaceSources } from "@/test/metric-workspace-fixtures"

vi.mock("@/lib/metric-workspace-api", () => ({ getWorkspaceSources: vi.fn(), queryWorkspaceMetrics: vi.fn(), createMetricDashboard: vi.fn(), saveMetricDashboard: vi.fn(), rollbackMetricDashboard: vi.fn(), deleteMetricDashboard: vi.fn(), listMetricDashboards: vi.fn(), getPublicDashboard: vi.fn(), queryWorkspaceLatest: vi.fn(async () => ({ series: [] })) }))
vi.mock("recharts", () => ({ ResponsiveContainer: ({ children }: { children: ReactNode }) => <div>{children}</div>, LineChart: ({ children }: { children: ReactNode }) => <div data-testid="metric-chart">{children}</div>, ComposedChart: ({ children }: { children: ReactNode }) => <div data-testid="metric-chart">{children}</div>, Line: () => null, XAxis: () => null, YAxis: () => null, CartesianGrid: () => null, Tooltip: () => null, ReferenceLine: () => null, ReferenceArea: () => null }))

const writeText = vi.fn()
beforeEach(() => {
  vi.resetAllMocks()
  writeText.mockResolvedValue(undefined)
  Object.defineProperty(navigator, "clipboard", { value: { writeText }, configurable: true })
  vi.mocked(workspaceAPI.getWorkspaceSources).mockResolvedValue(workspaceSources())
  vi.mocked(workspaceAPI.queryWorkspaceMetrics).mockResolvedValue(workspaceResult())
  window.localStorage.clear()
})

function view() {
  return render(<LocaleProvider><MemoryRouter initialEntries={["/admin/metrics/explore"]}><MetricsExplore /></MemoryRouter></LocaleProvider>)
}

describe("metrics explore page", () => {
  it("runs an ad-hoc query and renders the chart and table", async () => {
    const user = userEvent.setup()
    view()
    expect(await screen.findByRole("heading", { name: "Metric explorer" })).toBeInTheDocument()
    await screen.findByRole("combobox", { name: "Aggregation" })
    await user.click(screen.getByRole("button", { name: "Run query" }))
    await waitFor(() => expect(workspaceAPI.queryWorkspaceMetrics).toHaveBeenCalled())
    expect(await screen.findByTestId("metric-chart")).toBeInTheDocument()
    await user.click(screen.getByRole("button", { name: "Table" }))
    expect(screen.getAllByText(/CPU usage/, { selector: "td" }).length).toBeGreaterThan(0)
  })

  it("runs an instant query and renders the newest raw sample", async () => {
    const user = userEvent.setup()
    vi.mocked(workspaceAPI.queryWorkspaceLatest).mockResolvedValue({ series: [
      { ref_id: "A", node_id: "node-one", metric: "cpu", label: "CPU usage", unit: "percent", state: "ok", value: 41.5, sample_at: "2026-10-17T02:00:00.000Z" },
    ] })
    view()
    await screen.findByRole("combobox", { name: "Aggregation" })
    await user.click(screen.getByRole("combobox", { name: "Query type" }))
    await user.click(await screen.findByRole("option", { name: "Instant" }))
    await waitFor(() => expect(workspaceAPI.queryWorkspaceMetrics).toHaveBeenCalled())
    const rangeCalls = vi.mocked(workspaceAPI.queryWorkspaceMetrics).mock.calls.length
    await user.click(screen.getByRole("button", { name: "Run query" }))
    await waitFor(() => expect(workspaceAPI.queryWorkspaceLatest).toHaveBeenCalledWith({ series: [{ ref_id: "A", node_id: "node-one", metric: "cpu" }] }, expect.anything()))
    const table = await screen.findByRole("region", { name: "Instant results" })
    expect(table.textContent).toContain("41.5%")
    expect(table.textContent).toContain("CPU usage")
    // 切换到即时后不再发区间查询，避免同时打两份真实查询。
    expect(vi.mocked(workspaceAPI.queryWorkspaceMetrics).mock.calls.length).toBe(rangeCalls)
    expect(screen.getByText(/Instant returns the newest raw sample/)).toBeInTheDocument()
  })

  it("copies the share link and keeps the query in the URL", async () => {
    view()
    await screen.findByRole("combobox", { name: "Aggregation" })
    fireEvent.click(screen.getByRole("button", { name: "Copy link" }))
    await waitFor(() => expect(writeText).toHaveBeenCalled())
    expect(String(writeText.mock.calls[0][0])).toContain("/admin/metrics/explore?")
    expect(String(writeText.mock.calls[0][0])).toContain("cpu")
  })

  it("rejects a row without a node before running", async () => {
    const user = userEvent.setup()
    view()
    await screen.findByRole("combobox", { name: "Aggregation" })
    fireEvent.click(screen.getByRole("button", { name: /Remove query/ }))
    expect(screen.getByText("Keep at least one visible, complete query before running.")).toBeInTheDocument()
    await user.click(screen.getByRole("button", { name: "Add query" }))
    expect(screen.getByRole("combobox", { name: "Aggregation" })).toBeInTheDocument()
  })

  it("records query history, keeps starred entries and restores a query", async () => {
    const user = userEvent.setup()
    view()
    await screen.findByRole("combobox", { name: "Aggregation" })
    await user.click(screen.getByRole("button", { name: "Run query" }))
    await waitFor(() => expect(workspaceAPI.queryWorkspaceMetrics).toHaveBeenCalled())
    expect(window.localStorage.getItem("beat.explore.history.v1")).toContain("cpu")

    await user.click(screen.getByRole("combobox", { name: "Aggregation" }))
    await user.click(await screen.findByRole("option", { name: "Maximum" }))
    await user.click(screen.getByRole("button", { name: "Run query" }))
    await user.click(screen.getByRole("button", { name: "Query history" }))
    const dialog = await screen.findByRole("dialog")
    expect(dialog.textContent).toContain("cpu · max")
    expect(dialog.textContent).toContain("cpu · avg")

    // 星标第二条（cpu · avg），清理只应移除未加星标的记录。
    await user.click(screen.getAllByRole("button", { name: "Star query" })[1]!)
    await user.click(screen.getByRole("button", { name: "Clear history" }))
    await user.click(screen.getByRole("button", { name: "Clear history" }))
    expect(await screen.findByText(/Cleared 1 unstarred queries/)).toBeInTheDocument()
    expect(dialog.textContent).toContain("cpu · avg")
    expect(dialog.textContent).not.toContain("cpu · max")

    const calls = vi.mocked(workspaceAPI.queryWorkspaceMetrics).mock.calls.length
    await user.click(screen.getByRole("button", { name: "Open" }))
    await waitFor(() => expect(vi.mocked(workspaceAPI.queryWorkspaceMetrics).mock.calls.length).toBeGreaterThan(calls))
  })
})
