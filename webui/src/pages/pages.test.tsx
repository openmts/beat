import type { ReactNode } from "react"
import type { PublicMetricWorkspace } from "@/hooks/use-public-metric-workspace"
import { workspaceDashboard, workspaceQuery, workspaceSources, workspaceNode } from "@/test/metric-workspace-fixtures"
import { fireEvent, render, screen } from "@testing-library/react"
import { MemoryRouter, Route, Routes } from "react-router"
import { beforeEach, describe, expect, it, vi } from "vitest"
import { LocaleProvider } from "@/context/locale"
import { ThemeProvider } from "@/context/theme"
import Dashboard from "@/pages/dashboard"
import NodeDetail from "@/pages/node-detail"
import NotFound from "@/pages/not-found"
import * as hooks from "@/hooks/use-api"

const fleet = vi.hoisted(() => ({ nodes: [] as unknown[], groups: [], loading: false, error: "", offline: false, connected: true, stale: false, receivedAt: null, serverTime: null, refresh: vi.fn() }))
vi.mock("@/hooks/use-public-fleet", () => ({ usePublicFleet: () => fleet }))
const site = vi.hoisted(() => ({ showNetworkQuality: true }))
const publicWorkspace = vi.hoisted(() => ({ value: null as unknown }))
vi.mock("@/hooks/use-public-metric-workspace", () => ({ usePublicMetricWorkspace: () => publicWorkspace.value }))
vi.mock("@/components/metric-workspace/data-view", () => ({ WorkspaceDataView: ({ panel }: { panel: { title: string } }) => <div data-testid={`chart-${panel.title}`}>{panel.title}</div>, WorkspaceDataTable: () => <p>Data table</p> }))

vi.mock("@/hooks/use-api")
vi.mock("@/context/site-settings", () => ({
  useSiteSettings: () => ({
    settings: {
      site_title: "Beat Monitor",
      logo_url: "",
      show_network_quality: site.showNetworkQuality,
    },
    reportInteraction: vi.fn(),
  }),
}))
vi.mock("@/components/network-quality-band", () => ({
  NetworkQualityBand: () => <div>network-quality-band</div>,
}))
vi.mock("@/components/metrics-chart", () => ({
  MetricsChart: ({ title, from, to }: { title: string; from: number; to: number }) => (
    <div data-testid={`chart-${title}`} data-domain={`${from}:${to}`}>{title}</div>
  ),
}))

function Providers({ children }: { children: ReactNode }) {
  return <ThemeProvider><LocaleProvider><MemoryRouter initialEntries={["/node/node"]}>{children}</MemoryRouter></LocaleProvider></ThemeProvider>
}

const hookState = (overrides = {}) => ({ data: [], loading: false, error: null, refresh: vi.fn(), ...overrides })

describe("public pages", () => {
  beforeEach(() => {
    vi.resetAllMocks()
    fleet.nodes = []; fleet.groups = []; fleet.loading = false; fleet.error = ""
    vi.mocked(hooks.useNodes).mockReturnValue(hookState() as never)
    vi.mocked(hooks.useLiveNodes).mockReturnValue(hookState() as never)
    vi.mocked(hooks.useGroups).mockReturnValue(hookState() as never)
    vi.mocked(hooks.useNode).mockReturnValue(hookState({ data: nodeWithTraffic() }) as never)
    vi.mocked(hooks.useNodeMetrics).mockReturnValue(hookState({ data: {} }) as never)
    vi.mocked(hooks.useNodeCustomMetrics).mockReturnValue(hookState({ data: [] }) as never)
    publicWorkspace.value = pageWorkspace()
    site.showNetworkQuality = true
  })

  it("renders dashboard loading, errors, empty, and nodes", () => {
    fleet.loading = true; fleet.error = "unavailable"
    const { rerender } = render(<Providers><Dashboard /></Providers>)
    expect(screen.getByRole("alert")).toBeInTheDocument()
    fleet.loading = false; fleet.error = ""
    rerender(<Providers><Dashboard /></Providers>)
    expect(screen.getByText("No matching public nodes")).toBeInTheDocument()
    fleet.nodes = [{ ...workspaceNode("n"), name: "Node" }]
    rerender(<Providers><Dashboard /></Providers>)
    expect(screen.getByText("Node")).toBeInTheDocument()
  })

  it("delegates explicit retry to the shared live controller", () => {
    fleet.error = "unavailable"
    render(<Providers><Dashboard /></Providers>)
    fireEvent.click(screen.getByRole("button", { name: "Retry" }))
    expect(fleet.refresh).toHaveBeenCalledOnce()
  })

  it("hides public network quality when disabled by site settings", () => {
    site.showNetworkQuality = false
    render(<Providers><Dashboard /></Providers>)
    expect(screen.queryByText("network-quality-band")).not.toBeInTheDocument()
  })

  it("renders node detail loading/error states with the public workspace", () => {
    const workspace = pageWorkspace()
    workspace.node = { ...workspace.node, data: null, loading: true, error: "node failed" }
    workspace.source.error = "workspace.load_failed"
    publicWorkspace.value = workspace
    const view = render(<Providers><Routes><Route path="/node/:id" element={<NodeDetail />} /></Routes></Providers>)
    expect(screen.getByText("Loading node...")).toBeInTheDocument()
    expect(screen.getByText("The metric workspace could not be loaded.")).toBeInTheDocument()
    workspace.node = { ...workspace.node, data: { ...nodeWithTraffic(), alias: "Alias" } as never, loading: false, error: null }
    workspace.source.error = null
    view.rerender(<Providers><Routes><Route path="/node/:id" element={<NodeDetail />} /></Routes></Providers>)
    expect(screen.getByText("Alias")).toBeInTheDocument()
    expect(screen.getByRole("link", { name: "Back" })).toHaveAttribute("href", "/")
    expect(screen.getByText("25 GiB / 100 GiB")).toBeInTheDocument()
    fireEvent.click(screen.getByRole("button", { name: "Refresh now" }))
    expect(workspace.refresh).toHaveBeenCalledOnce()
  })

  it("renders only template-defined panels without invoking legacy metric collectors", () => {
    const workspace = pageWorkspace()
    workspace.definition.panels.push({ ...workspace.definition.panels[0], id: "custom", title: "Public latency", series: [{ ...workspace.definition.panels[0].series[0], metric: "custom_http_latency" }] })
    publicWorkspace.value = workspace
    render(<Providers><Routes><Route path="/node/:id" element={<NodeDetail />} /></Routes></Providers>)
    expect(screen.getByRole("region", { name: "Public latency" })).toBeInTheDocument()
    expect(screen.getByRole("region", { name: "CPU usage" })).toBeInTheDocument()
    expect(hooks.useNodeMetrics).not.toHaveBeenCalled()
    expect(hooks.useNodeCustomMetrics).not.toHaveBeenCalled()
  })

  it("renders node resource cards, runtime stats, and tags", () => {
    const richNode = {
      ...nodeWithTraffic(),
      alias: "Rich",
      tags: ["edge", "prod"],
      public_remark: "Primary node",
      kernel: "6.8.0",
      cpu_model: "Test CPU",
      virtualization: "kvm guest",
      agent_version: "1.2.3",
      last_seen: "2026-07-30T00:00:00Z",
      metrics: {
        cpu: 12.34, cpu_used: 1, cpu_total: 8,
        memory: 50, memory_used: 4096, memory_total: 8192,
        disk: 40, disk_used: 40 * 1024 ** 3, disk_total: 100 * 1024 ** 3,
        swap: 25, swap_used: 1024, swap_total: 4096,
        net_sent: 1024, net_recv: 2048, net_sent_total: 4096, net_recv_total: 8192,
        load1: 1, load5: 2, load15: 3, uptime: 3600, processes: 42,
        tcp_connections: 7, udp_connections: 3,
      },
    }
    ;(publicWorkspace.value as PublicMetricWorkspace).node.data = richNode as never
    render(<Providers><Routes><Route path="/node/:id" element={<NodeDetail />} /></Routes></Providers>)
    expect(screen.getByText("12.3%")).toBeInTheDocument()
    expect(screen.getByText("1 / 8 cores")).toBeInTheDocument()
    expect(screen.getByText("50.0%")).toBeInTheDocument()
    expect(screen.getByText("40.0%")).toBeInTheDocument()
    expect(screen.getByText("25.0%")).toBeInTheDocument()
    expect(screen.getByText("1h 0m")).toBeInTheDocument()
    expect(screen.getByText("1 / 2 / 3")).toBeInTheDocument()
    expect(screen.getByText("42")).toBeInTheDocument()
    expect(screen.getByText("7 TCP / 3 UDP")).toBeInTheDocument()
    expect(screen.getByText("edge")).toBeInTheDocument()
    expect(screen.getByText("prod")).toBeInTheDocument()
    expect(screen.getByText("Primary node")).toBeInTheDocument()
    expect(screen.getByText("Test CPU")).toBeInTheDocument()
  })

  it("renders the not found page", () => {
    render(<Providers><NotFound /></Providers>)
    expect(screen.getByText("404")).toBeInTheDocument()
    expect(screen.getByText("Page not found")).toBeInTheDocument()
  })
})

function nodeWithTraffic() {
  return {
    id: "node", name: "Node", alias: "", status: "online", metrics: {},
    traffic: {
      sent: 10 * 1024 ** 3, received: 15 * 1024 ** 3, used: 25 * 1024 ** 3,
      limit: 100 * 1024 ** 3, remaining: 75 * 1024 ** 3, percentage: 25,
      limit_type: "sum", reset_day: 1, period_start: "2026-07-01T00:00:00Z",
      next_reset: "2026-08-01T00:00:00Z", tracked_since: "2026-07-01T00:00:00Z",
      status: "normal",
    },
  }
}

function pageWorkspace(): PublicMetricWorkspace {
  const definition = workspaceDashboard().definition
  return { node: { nodeId: "node", data: nodeWithTraffic() as never, loading: false, error: null, refresh: vi.fn() },
    source: { nodeId: "node", data: { ...workspaceSources(), dashboard: { ...workspaceDashboard(), scope: "public" } }, loading: false, error: null, reload: vi.fn() },
    definition, queries: { dashboardId: "public", panels: {}, domain: { from: workspaceQuery().from, to: workspaceQuery().to }, refresh: vi.fn(), retryPanel: vi.fn() },
    values: { node: ["node"] }, range: { mode: "relative", seconds: 3600 }, refreshSeconds: 5, history: undefined, historyMetricNames: [], metricsState: "ready",
    setRange: vi.fn(), setRefreshSeconds: vi.fn(), setVariable: vi.fn(), toggleRow: vi.fn(), refresh: vi.fn() }
}
