import type { ReactNode } from "react"
import { fireEvent, render, screen, waitFor } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { MemoryRouter } from "react-router"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import MetricsWorkspace from "@/pages/admin/metrics"
import { LocaleProvider } from "@/context/locale"
import { APIError } from "@/lib/api-error"
import * as workspaceAPI from "@/lib/metric-workspace-api"
import { workspaceDashboard, workspaceResult, workspaceSources } from "@/test/metric-workspace-fixtures"

vi.mock("@/lib/metric-workspace-api", () => ({ getWorkspaceSources: vi.fn(), queryWorkspaceMetrics: vi.fn(), createMetricDashboard: vi.fn(), saveMetricDashboard: vi.fn(), rollbackMetricDashboard: vi.fn(), deleteMetricDashboard: vi.fn(), listMetricDashboards: vi.fn(), getPublicDashboard: vi.fn(), queryWorkspaceLatest: vi.fn(async () => ({ series: [] })) }))
vi.mock("@/context/auth", () => ({ useAuth: () => ({ principal: { user: { role: "owner", totp_enabled_at: null }, session: { reauthenticated_until: "2099-01-01T00:00:00Z" } } }) }))
vi.mock("recharts", () => ({ ResponsiveContainer: ({ children }: { children: ReactNode }) => <div>{children}</div>, LineChart: ({ children }: { children: ReactNode }) => <div data-testid="metric-chart">{children}</div>, ComposedChart: ({ children }: { children: ReactNode }) => <div data-testid="metric-chart">{children}</div>, Line: () => null, XAxis: () => null, YAxis: () => null, CartesianGrid: () => null, Tooltip: () => null, ReferenceLine: () => null, ReferenceArea: () => null }))

function renderWorkspace() { return render(<LocaleProvider><MemoryRouter><MetricsWorkspace /></MemoryRouter></LocaleProvider>) }

beforeEach(() => {
  vi.resetAllMocks()
  vi.mocked(workspaceAPI.getWorkspaceSources).mockResolvedValue(workspaceSources())
  vi.mocked(workspaceAPI.queryWorkspaceMetrics).mockResolvedValue(workspaceResult())
  vi.mocked(workspaceAPI.saveMetricDashboard).mockImplementation(async (value) => ({ ...value, revision: value.revision + 1, has_previous: true }))
  vi.mocked(workspaceAPI.createMetricDashboard).mockImplementation(async (definition) => ({ ...workspaceDashboard(), id: "saved-personal", scope: "personal", revision: 1, definition }))
  vi.mocked(workspaceAPI.listMetricDashboards).mockResolvedValue(workspaceSources().dashboards)
})

afterEach(() => vi.unstubAllGlobals())

describe("metric workspace", () => {
  it("opens the panel editor when randomUUID is unavailable on an HTTP origin", async () => {
    vi.stubGlobal("crypto", {
      getRandomValues: (bytes: Uint8Array) => {
        bytes.fill(0)
        return bytes
      },
    })
    const user = userEvent.setup()
    renderWorkspace()
    await screen.findByText("CPU usage", { selector: "h3" })
    await user.click(screen.getByRole("button", { name: "Add panel" }))
    expect(await screen.findByRole("dialog")).toHaveTextContent("Edit panel")
  })

  it("loads usable node labels, queries and inspector data", async () => {
    renderWorkspace()
    await screen.findByText("CPU usage", { selector: "h3" })
    await waitFor(() => expect(workspaceAPI.queryWorkspaceMetrics).toHaveBeenCalled())
    expect(screen.getByRole("combobox", { name: "Node" })).toHaveTextContent("Production host")
    expect(screen.getByRole("combobox", { name: "Dashboard" })).toHaveTextContent("Node overview")
    fireEvent.click(screen.getByRole("button", { name: "Inspect data · CPU usage" }))
    expect(await screen.findByRole("button", { name: "Download CSV" })).toBeEnabled()
    expect(screen.getByText("40%")).toBeInTheDocument()
    expect(screen.getByText("—", { selector: "td" })).toBeInTheDocument()
  })

  it("edits a panel and saves the complete layout with its revision", async () => {
    const user = userEvent.setup()
    renderWorkspace()
    await screen.findByText("CPU usage", { selector: "h3" })
    await user.click(screen.getByRole("button", { name: "Edit panel · CPU usage" }))
    await user.click(await screen.findByRole("menuitem", { name: "Edit panel" }))
    const title = await screen.findByLabelText("Panel title")
    fireEvent.change(title, { target: { value: "Compute pressure" } })
    await user.click(screen.getByRole("button", { name: "Apply" }))
    expect(screen.getByText("Unsaved changes")).toBeInTheDocument()
    await user.click(screen.getByRole("button", { name: "Save dashboard" }))
    await waitFor(() => expect(workspaceAPI.saveMetricDashboard).toHaveBeenCalledWith(expect.objectContaining({ revision: 0, definition: expect.objectContaining({ panels: [expect.objectContaining({ title: "Compute pressure" })] }) })))
    await screen.findByText("Saved")
  })

  it("preserves edits on a conflict and reloads only after confirmation", async () => {
    vi.mocked(workspaceAPI.saveMetricDashboard).mockRejectedValue(new APIError("changed", 409))
    const user = userEvent.setup()
    renderWorkspace()
    await screen.findByText("CPU usage", { selector: "h3" })
    await user.click(screen.getByRole("button", { name: "Dashboard details" }))
    fireEvent.change(await screen.findByLabelText("Dashboard title"), { target: { value: "My draft" } })
    await user.click(screen.getByRole("button", { name: "Apply" }))
    await user.click(screen.getByRole("button", { name: "Save dashboard" }))
    await screen.findByText("A newer revision exists. Reload it before saving again.")
    expect(screen.getByText("Unsaved changes")).toBeInTheDocument()
    await user.click(screen.getByRole("button", { name: "Reload saved version" }))
    expect(await screen.findByText("Discard unsaved changes?")).toBeInTheDocument()
    await user.click(screen.getByRole("button", { name: "Continue" }))
    await waitFor(() => expect(workspaceAPI.listMetricDashboards).toHaveBeenCalledOnce())
    await screen.findByText("Built-in template")
  })

  it("changes the query window and retains good data on refresh failure", async () => {
    const user = userEvent.setup()
    renderWorkspace()
    await waitFor(() => expect(workspaceAPI.queryWorkspaceMetrics).toHaveBeenCalled())
    await user.click(screen.getByRole("combobox", { name: "Time range" }))
    await user.click(await screen.findByRole("option", { name: "Last 5 minutes" }))
    await waitFor(() => {
      const query = vi.mocked(workspaceAPI.queryWorkspaceMetrics).mock.lastCall?.[0]
      expect(Date.parse(query!.to) - Date.parse(query!.from)).toBe(300000)
    })
    vi.mocked(workspaceAPI.queryWorkspaceMetrics).mockRejectedValue(new APIError("Storage unavailable", 503))
    await user.click(screen.getByRole("button", { name: "Refresh now" }))
    expect(await screen.findByText("Storage unavailable")).toBeInTheDocument()
    expect(screen.getByTestId("metric-chart")).toBeInTheDocument()
    expect(screen.getByText("Showing the last successful result while the new query is pending or failed.")).toBeInTheDocument()
  })

  it("starts a new dashboard after confirming unsaved changes", async () => {
    const user = userEvent.setup()
    renderWorkspace()
    await screen.findByText("CPU usage", { selector: "h3" })
    await user.click(screen.getByRole("button", { name: "New dashboard" }))
    expect(await screen.findByText("Build your first panel")).toBeInTheDocument()
    await user.click(screen.getByRole("button", { name: "Save dashboard" }))
    await waitFor(() => expect(workspaceAPI.createMetricDashboard).toHaveBeenCalledWith(expect.objectContaining({ title: "Untitled dashboard", panels: [] })))
  })

  it("shows a deterministic source failure and supports retry", async () => {
    vi.mocked(workspaceAPI.getWorkspaceSources).mockRejectedValueOnce(new APIError("Workspace offline", 404))
    renderWorkspace()
    expect(await screen.findByRole("alert")).toHaveTextContent("Workspace offline")
    fireEvent.click(screen.getByRole("button", { name: "Retry" }))
    await screen.findByText("CPU usage", { selector: "h3" })
  })

  it("recovers from a transient source failure without a manual retry", async () => {
    vi.mocked(workspaceAPI.getWorkspaceSources).mockRejectedValueOnce(new APIError("bad gateway", 502))
    renderWorkspace()
    await screen.findByText("CPU usage", { selector: "h3" }, { timeout: 5_000 })
    expect(vi.mocked(workspaceAPI.getWorkspaceSources).mock.calls.length).toBeGreaterThan(1)
    expect(screen.queryByRole("alert")).not.toBeInTheDocument()
  })
})
