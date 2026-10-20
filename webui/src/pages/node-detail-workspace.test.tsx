import { fireEvent, render, screen, waitFor } from "@testing-library/react"
import { MemoryRouter } from "react-router"
import userEvent from "@testing-library/user-event"
import { beforeEach, describe, expect, it, vi } from "vitest"
import { NodeDetailView } from "@/pages/node-detail-view"
import { PublicThemeProvider } from "@/components/public-theme-provider"
import { LocaleProvider } from "@/context/locale"
import { buildThemeSnapshot } from "@/lib/theme-snapshot"
import { defaultSiteSettings } from "@/context/site-settings"
import { workspaceDashboard, workspaceNode, workspaceQuery, workspaceResult, workspaceSources } from "@/test/metric-workspace-fixtures"
import { THEME_PALETTE_TOKENS, type ThemeDefinition } from "@/types/theme-definition"
import type { PublicMetricWorkspace } from "@/hooks/use-public-metric-workspace"

const siteState = vi.hoisted(() => ({ showAccelerators: true }))
vi.mock("@/components/header", () => ({ Header: () => <header>Header</header> }))
vi.mock("@/components/public-footer", () => ({ PublicFooter: () => <footer>Footer</footer> }))
vi.mock("@/components/accelerator-detail", () => ({ AcceleratorDetail: () => <section>GPU accelerator section</section> }))
vi.mock("@/context/site-settings", async (original) => {
  const actual = await original<typeof import("@/context/site-settings")>()
  return {
    ...actual,
    useSiteSettings: () => ({
      settings: { ...actual.defaultSiteSettings, show_accelerators: siteState.showAccelerators },
      loading: false,
      error: null,
      applySettings: vi.fn(),
      reload: vi.fn(),
      reportInteraction: vi.fn(),
    }),
  }
})
vi.mock("@/components/metric-workspace/data-view", () => ({ WorkspaceDataView: ({ result }: { result: unknown }) => <pre data-testid="data-view">{JSON.stringify(result)}</pre>, WorkspaceDataTable: () => <p>Data table</p> }))
function workspace(): PublicMetricWorkspace {
  return { node: { nodeId: "node-one", data: workspaceNode(), loading: false, error: null, refresh: vi.fn() }, source: { nodeId: "node-one", data: { ...workspaceSources(), dashboard: { ...workspaceDashboard(), scope: "public" } }, loading: false, error: null, reload: vi.fn() },
    definition: workspaceDashboard().definition, queries: { dashboardId: "public", domain: { from: workspaceQuery().from, to: workspaceQuery().to }, panels: { cpu: { loading: false, result: workspaceResult(), resultQuery: workspaceQuery(), updatedAt: 1 } }, refresh: vi.fn(), retryPanel: vi.fn() },
    range: { mode: "relative", seconds: 3600 }, refreshSeconds: 5, values: { node: ["node-one"] }, history: undefined, historyMetricNames: [], metricsState: "ready",
    setRange: vi.fn(), setRefreshSeconds: vi.fn(), setVariable: vi.fn(), toggleRow: vi.fn(), refresh: vi.fn() }
}
function show(state: PublicMetricWorkspace, detail: ThemeDefinition["detail"] = "balanced") {
  const palette = Object.fromEntries(THEME_PALETTE_TOKENS.map((key) => [key, "#172033"])) as ThemeDefinition["light"]
  const definition: ThemeDefinition = { schema_version: 1, overview: "cards", detail, density: "comfortable", radius: 6, light: palette, dark: palette }
  const snapshot = buildThemeSnapshot({ route: { kind: "node", node_id: "node-one" }, site: defaultSiteSettings, groups: [], nodes: [workspaceNode()], locale: "en", theme: "light" })
  return render(<LocaleProvider><MemoryRouter><PublicThemeProvider definition={definition} snapshot={snapshot}><NodeDetailView workspace={state} /></PublicThemeProvider></MemoryRouter></LocaleProvider>)
}

describe("public node workspace view", () => {
  beforeEach(() => { vi.restoreAllMocks(); siteState.showAccelerators = true })
  it.each(["balanced", "compact", "spotlight"] as const)("keeps %s layout and exposes read-only inspection", (detail) => {
    show(workspace(), detail)
    expect(document.querySelector(`[data-theme-detail-layout='${detail}']`)).toBeInTheDocument()
    expect(screen.queryByRole("button", { name: /Edit panel/ })).not.toBeInTheDocument()
    expect(document.querySelector('[draggable="true"]')).toBeNull()
    fireEvent.click(screen.getByRole("button", { name: /Inspect.*CPU usage/ }))
    expect(screen.getByRole("button", { name: /CSV/ })).toBeInTheDocument()
    expect(screen.getByRole("link", { name: /Focus this node/ })).toHaveAttribute("href", "/node/node-one")
  })
  it("applies the global visibility override before the node switch", () => {
    const visibleState = workspace()
    if (visibleState.node.data) visibleState.node.data.accelerator_visible = true
    let view = show(visibleState)
    expect(screen.getByText("GPU accelerator section")).toBeInTheDocument()
    view.unmount()

    if (visibleState.node.data) visibleState.node.data.accelerator_visible = false
    view = show(visibleState)
    expect(screen.queryByText("GPU accelerator section")).not.toBeInTheDocument()
    view.unmount()

    if (visibleState.node.data) visibleState.node.data.accelerator_visible = true
    siteState.showAccelerators = false
    show(visibleState)
    expect(screen.queryByText("GPU accelerator section")).not.toBeInTheDocument()
  })
  it("supports public range, pause and refresh controls", async () => {
    const state = workspace(), user = userEvent.setup()
    show(state)
    fireEvent.click(screen.getByRole("button", { name: "Pause refresh" }))
    expect(state.setRefreshSeconds).toHaveBeenCalledWith(0)
    fireEvent.click(screen.getByRole("button", { name: "Refresh now" }))
    expect(state.refresh).toHaveBeenCalledOnce()
    await user.click(screen.getByRole("combobox", { name: "Time range" }))
    await user.click(await screen.findByRole("option", { name: "Last 6 hours" }))
    expect(state.setRange).toHaveBeenCalledWith({ mode: "relative", seconds: 21600 })
  })
  it("shows loading/errors and preserves the actual range of retained results", async () => {
    const state = workspace()
    state.queries.panels.cpu.loading = true
    state.queries.panels.cpu.query = { ...workspaceQuery(), from: "2026-09-13T05:00:00Z", to: "2026-09-13T06:00:00Z" }
    const view = show(state)
    expect(screen.getByTestId("data-view")).toHaveTextContent(workspaceQuery().from)
    view.unmount()
    state.source.error = "workspace.load_failed"
    state.node.error = "workspace.node_refresh_failed"
    show(state)
    fireEvent.click(screen.getByRole("button", { name: "Retry" }))
    await waitFor(() => expect(state.source.reload).toHaveBeenCalledOnce())
  })
})
