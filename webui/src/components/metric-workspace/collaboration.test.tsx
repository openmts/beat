import { render, screen, waitFor, within } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { MemoryRouter } from "react-router"
import { beforeEach, describe, expect, it, vi } from "vitest"
import { LocaleProvider } from "@/context/locale"
import * as alertsApi from "@/lib/alert-evaluation-api"
import * as api from "@/lib/metric-collaboration-api"
import { evaluationRuleFixture } from "@/test/alert-evaluation-fixtures"
import { WorkspaceAnnotations } from "./annotations"
import { PanelAlertBadge, PanelAlertDialog } from "./panel-alerts"
import { WorkspacePanelLibrary } from "./panel-library"
import { WorkspaceSnapshots } from "./snapshots"
import { alertStateKey } from "./collaboration-text"

vi.mock("@/lib/metric-collaboration-api")
vi.mock("@/lib/alert-evaluation-api")

const panel = { id: "cpu", title: "CPU usage", type: "timeseries", size: "medium", legend: true, thresholds: [], series: [] } as never

function annotation(overrides: Partial<api.DashboardAnnotation> = {}): api.DashboardAnnotation {
  return { id: "annotation-1", dashboard_id: "public-default", title: "Deploy 1.2", tags: ["deploy"], color: "blue",
    visibility: "public", from: "2026-10-14T00:00:00Z", to: "2026-10-14T01:00:00Z",
    created_at: "2026-10-14T00:00:00Z", updated_at: "2026-10-14T00:00:00Z", ...overrides }
}

function panelAlert(overrides: Partial<api.DashboardPanelAlert> = {}): api.DashboardPanelAlert {
  return { dashboard_id: "dash-1", panel_id: "cpu", rule_id: "rule-1", rule_name: "CPU high", metric: "cpu",
    severity: "warning", enabled: true, created_at: "2026-10-14T00:00:00Z",
    state: { phase: "firing", status: "ok", firing: 2, pending: 0, no_data: 0, error: 0, normal: 1 }, ...overrides }
}

beforeEach(() => { vi.resetAllMocks(); localStorage.setItem("locale", "en") })

describe("panel alert badges", () => {
  it("maps evaluation phases to readable states", () => {
    expect(alertStateKey("firing", "ok")).toBe("alert_state_firing")
    expect(alertStateKey("pending", "ok")).toBe("alert_state_pending")
    expect(alertStateKey("recovering", "ok")).toBe("alert_state_recovering")
    expect(alertStateKey("normal", "no_data")).toBe("alert_state_no_data")
    expect(alertStateKey("normal", "error")).toBe("alert_state_error")
    expect(alertStateKey("normal", "ok")).toBe("alert_state_normal")
  })

  it("renders the worst linked state and links to its history", () => {
    render(<MemoryRouter><LocaleProvider><PanelAlertBadge context="CPU usage" alerts={[
      panelAlert(), panelAlert({ rule_id: "rule-2", state: { phase: "normal", status: "ok", firing: 0, pending: 0, no_data: 0, error: 0, normal: 3 } })]} /></LocaleProvider></MemoryRouter>)
    const link = screen.getByRole("link")
    expect(link).toHaveTextContent("Firing")
    expect(link).toHaveAttribute("href", "/admin/alerts?tab=evaluation-history&rule_id=rule-1")
  })

  it("renders nothing without linked rules", () => {
    const { container } = render(<MemoryRouter><LocaleProvider><PanelAlertBadge context="CPU usage" alerts={[]} /></LocaleProvider></MemoryRouter>)
    expect(container).toBeEmptyDOMElement()
  })
})

describe("workspace annotations", () => {
  it("lists annotations, creates one and deletes it", async () => {
    const user = userEvent.setup()
    vi.mocked(api.listAnnotations).mockResolvedValue([annotation()])
    vi.mocked(api.createAnnotation).mockResolvedValue(annotation({ id: "annotation-2", title: "Maintenance" }))
    vi.mocked(api.deleteAnnotation).mockResolvedValue()
    render(<MemoryRouter><LocaleProvider><WorkspaceAnnotations dashboardId="public-default" from="2026-10-14T00:00:00Z" to="2026-10-14T02:00:00Z" editable reload={0} onError={vi.fn()} onChanged={vi.fn()} /></LocaleProvider></MemoryRouter>)
    expect(await screen.findByText("Deploy 1.2")).toBeInTheDocument()
    await user.click(screen.getByRole("button", { name: "Add annotation" }))
    await user.type(screen.getByLabelText("Title"), "Maintenance")
    await user.click(within(screen.getByRole("dialog")).getByRole("button", { name: "Save" }))
    await waitFor(() => expect(api.createAnnotation).toHaveBeenCalledWith(expect.objectContaining({ title: "Maintenance", visibility: "private" })))
    await user.click(screen.getByRole("button", { name: "Delete annotation · Deploy 1.2" }))
    await waitFor(() => expect(api.deleteAnnotation).toHaveBeenCalledWith("annotation-1"))
  })

  it("reports loaded annotations and creates a panel-scoped one", async () => {
    const user = userEvent.setup()
    const onLoaded = vi.fn()
    vi.mocked(api.listAnnotations).mockResolvedValue([annotation({ id: "annotation-scoped", panel_id: "cpu" })])
    vi.mocked(api.createAnnotation).mockResolvedValue(annotation({ id: "annotation-3", panel_id: "cpu" }))
    render(<MemoryRouter><LocaleProvider><WorkspaceAnnotations dashboardId="public-default" from="2026-10-14T00:00:00Z" to="2026-10-14T02:00:00Z" editable reload={0}
      onError={vi.fn()} onChanged={vi.fn()} onLoaded={onLoaded} panels={[{ id: "cpu", title: "CPU usage" }]} /></LocaleProvider></MemoryRouter>)
    await waitFor(() => expect(onLoaded).toHaveBeenCalledWith([expect.objectContaining({ id: "annotation-scoped" })]))
    expect(await screen.findByText("CPU usage")).toBeInTheDocument()
    await user.click(screen.getByRole("button", { name: "Add annotation" }))
    await user.type(screen.getByLabelText("Title"), "Scoped")
    await user.click(screen.getByRole("combobox", { name: "Scope" }))
    await user.click(await screen.findByRole("option", { name: "CPU usage" }))
    await user.click(within(screen.getByRole("dialog")).getByRole("button", { name: "Save" }))
    await waitFor(() => expect(api.createAnnotation).toHaveBeenCalledWith(expect.objectContaining({ title: "Scoped", panel_id: "cpu" })))
  })

  it("reports the remaining annotations after a delete", async () => {
    const user = userEvent.setup()
    const onLoaded = vi.fn()
    vi.mocked(api.listAnnotations).mockResolvedValue([annotation()])
    vi.mocked(api.deleteAnnotation).mockResolvedValue()
    render(<MemoryRouter><LocaleProvider><WorkspaceAnnotations dashboardId="public-default" from="2026-10-14T00:00:00Z" to="2026-10-14T02:00:00Z" editable reload={0}
      onError={vi.fn()} onChanged={vi.fn()} onLoaded={onLoaded} /></LocaleProvider></MemoryRouter>)
    await screen.findByText("Deploy 1.2")
    await user.click(screen.getByRole("button", { name: "Delete annotation · Deploy 1.2" }))
    await waitFor(() => expect(onLoaded).toHaveBeenLastCalledWith([]))
  })

  it("surfaces load failures and hides editing for read-only dashboards", async () => {
    const onError = vi.fn()
    vi.mocked(api.listAnnotations).mockRejectedValue(new Error("offline"))
    render(<MemoryRouter><LocaleProvider><WorkspaceAnnotations dashboardId="public-default" from="2026-10-14T00:00:00Z" to="2026-10-14T02:00:00Z" editable={false} reload={0} onError={onError} onChanged={vi.fn()} /></LocaleProvider></MemoryRouter>)
    await waitFor(() => expect(onError).toHaveBeenCalledWith("Collaboration data could not be loaded."))
    expect(screen.queryByRole("button", { name: "Add annotation" })).not.toBeInTheDocument()
  })
})

describe("workspace snapshots", () => {
  it("creates a share link and revokes it", async () => {
    const user = userEvent.setup()
    const snapshot = { id: "snap-1", token: "a".repeat(64), name: "Freeze", dashboard_id: "dash-1", revision: 2,
      definition: { version: 1, title: "Node", description: "", time_range_seconds: 3600, refresh_seconds: 15, variables: [], panels: [] },
      created_at: "2026-10-14T00:00:00Z" } as api.DashboardSnapshot
    vi.mocked(api.listSnapshots).mockResolvedValue([])
    vi.mocked(api.createSnapshot).mockResolvedValue(snapshot)
    vi.mocked(api.revokeSnapshot).mockResolvedValue()
    render(<MemoryRouter><LocaleProvider><WorkspaceSnapshots dashboardId="dash-1" dashboardName="Freeze" close={vi.fn()} onError={vi.fn()} /></LocaleProvider></MemoryRouter>)
    expect(await screen.findByText("No snapshots yet")).toBeInTheDocument()
    await user.click(screen.getByRole("button", { name: "Create snapshot link" }))
    expect(await screen.findByLabelText("Read-only link created")).toHaveValue(`http://localhost:3000/metrics/snapshots/${"a".repeat(64)}`)
    await user.click(screen.getByRole("button", { name: "Revoke link" }))
    await waitFor(() => expect(api.revokeSnapshot).toHaveBeenCalledWith("snap-1"))
  })
})

describe("panel library", () => {
  it("saves a panel, inserts a stored one and deletes it", async () => {
    const user = userEvent.setup()
    const stored = { id: "lib-1", name: "Saved CPU", panel, created_at: "2026-10-14T00:00:00Z", updated_at: "2026-10-14T00:00:00Z" }
    vi.mocked(api.listLibraryPanels).mockResolvedValue([stored])
    vi.mocked(api.createLibraryPanel).mockResolvedValue({ ...stored, id: "lib-2", name: "CPU usage" })
    vi.mocked(api.deleteLibraryPanel).mockResolvedValue()
    const insert = vi.fn()
    render(<MemoryRouter><LocaleProvider><WorkspacePanelLibrary panel={panel} insert={insert} close={vi.fn()} onError={vi.fn()} /></LocaleProvider></MemoryRouter>)
    expect(await screen.findByText("Saved CPU")).toBeInTheDocument()
    await user.click(screen.getByRole("button", { name: "Save to panel library" }))
    await waitFor(() => expect(api.createLibraryPanel).toHaveBeenCalledWith(expect.objectContaining({ panel })))
    await user.click(within(screen.getByText("Saved CPU").closest("li") as HTMLElement).getByRole("button", { name: "Insert" }))
    expect(insert).toHaveBeenCalledWith(expect.objectContaining({ title: "CPU usage" }))
    await user.click(screen.getByRole("button", { name: "Delete · Saved CPU" }))
    await waitFor(() => expect(api.deleteLibraryPanel).toHaveBeenCalledWith("lib-1"))
  })
  it("filters stored panels by name and panel title", async () => {
    const user = userEvent.setup()
    const stored = [
      { id: "lib-1", name: "Saved CPU", panel, created_at: "2026-10-14T00:00:00Z", updated_at: "2026-10-14T00:00:00Z" },
      { id: "lib-2", name: "Disk", panel: { ...(panel as object), id: "disk", title: "Disk usage" }, created_at: "2026-10-14T00:00:00Z", updated_at: "2026-10-14T00:00:00Z" },
    ]
    vi.mocked(api.listLibraryPanels).mockResolvedValue(stored as never)
    render(<MemoryRouter><LocaleProvider><WorkspacePanelLibrary close={vi.fn()} onError={vi.fn()} /></LocaleProvider></MemoryRouter>)
    expect(await screen.findByText("Saved CPU")).toBeInTheDocument()
    await user.type(screen.getByLabelText("Search"), "disk")
    expect(screen.queryByText("Saved CPU")).not.toBeInTheDocument()
    expect(screen.getByText("Disk")).toBeInTheDocument()
    await user.clear(screen.getByLabelText("Search"))
    await user.type(screen.getByLabelText("Search"), "missing")
    expect(screen.getByText("No saved panel matches this search.")).toBeInTheDocument()
  })
})

describe("panel alert dialog", () => {
  it("replaces the linked rules with the checked ones", async () => {
    const user = userEvent.setup()
    vi.mocked(alertsApi.evaluationRules).mockResolvedValue([evaluationRuleFixture()])
    vi.mocked(api.replacePanelAlerts).mockResolvedValue([panelAlert()])
    const applied = vi.fn()
    render(<MemoryRouter><LocaleProvider><PanelAlertDialog dashboardId="dash-1" panel={panel}
      alerts={[panelAlert({ rule_id: "rule-one" })]} close={vi.fn()} applied={applied} onError={vi.fn()} /></LocaleProvider></MemoryRouter>)
    expect(await screen.findByText("CPU high")).toBeInTheDocument()
    expect(screen.getByRole("checkbox")).toBeChecked()
    await user.click(screen.getByRole("button", { name: "Save" }))
    await waitFor(() => expect(api.replacePanelAlerts).toHaveBeenCalledWith("dash-1", "cpu", ["rule-one"]))
    expect(applied).toHaveBeenCalled()
  })
})
