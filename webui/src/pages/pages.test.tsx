import type { ReactNode } from "react"
import { act, fireEvent, render, screen } from "@testing-library/react"
import { MemoryRouter, Route, Routes } from "react-router"
import { beforeEach, describe, expect, it, vi } from "vitest"
import { LocaleProvider } from "@/context/locale"
import { ThemeProvider } from "@/context/theme"
import Dashboard from "@/pages/dashboard"
import NodeDetail from "@/pages/node-detail"
import NotFound from "@/pages/not-found"
import * as hooks from "@/hooks/use-api"

const site = vi.hoisted(() => ({ showNetworkQuality: true }))

vi.mock("@/hooks/use-api")
vi.mock("@/context/site-settings", () => ({
  useSiteSettings: () => ({
    settings: {
      site_title: "Beat Monitor",
      logo_url: "",
      show_network_quality: site.showNetworkQuality,
    },
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
    vi.mocked(hooks.useNodes).mockReturnValue(hookState() as never)
    vi.mocked(hooks.useLiveNodes).mockReturnValue(hookState() as never)
    vi.mocked(hooks.useGroups).mockReturnValue(hookState() as never)
    vi.mocked(hooks.useNode).mockReturnValue(hookState({ data: nodeWithTraffic() }) as never)
    vi.mocked(hooks.useNodeMetrics).mockReturnValue(hookState({ data: {} }) as never)
    site.showNetworkQuality = true
  })

  it("renders dashboard loading, errors, empty, and nodes", () => {
    vi.mocked(hooks.useLiveNodes).mockReturnValue(hookState({ loading: true, error: "nodes failed" }) as never)
    vi.mocked(hooks.useGroups).mockReturnValue(hookState({ loading: true, error: "groups failed" }) as never)
    const { rerender } = render(<Providers><Dashboard /></Providers>)
    expect(screen.getByText("nodes failed")).toBeInTheDocument()
    vi.mocked(hooks.useLiveNodes).mockReturnValue(hookState() as never)
    vi.mocked(hooks.useGroups).mockReturnValue(hookState() as never)
    rerender(<Providers><Dashboard /></Providers>)
    expect(screen.getByText("No nodes yet")).toBeInTheDocument()
    vi.mocked(hooks.useLiveNodes).mockReturnValue(hookState({ data: [{ id: "n", name: "Node", alias: "", status: "online", metrics: {} }] }) as never)
    rerender(<Providers><Dashboard /></Providers>)
    expect(screen.getByText("Node")).toBeInTheDocument()
  })

  it("refreshes dashboard on its timer", () => {
    vi.useFakeTimers()
    const refresh = vi.fn()
    vi.mocked(hooks.useLiveNodes).mockReturnValue(hookState({ refresh }) as never)
    render(<Providers><Dashboard /></Providers>)
    act(() => vi.advanceTimersByTime(30_000))
    expect(refresh).toHaveBeenCalledWith({ silent: true })
    vi.useRealTimers()
  })

  it("hides public network quality when disabled by site settings", () => {
    site.showNetworkQuality = false
    render(<Providers><Dashboard /></Providers>)
    expect(screen.queryByText("network-quality-band")).not.toBeInTheDocument()
  })

  it("renders node detail states and changes range", () => {
    vi.mocked(hooks.useNode).mockReturnValue(hookState({ data: null, loading: true, error: "node failed" }) as never)
    vi.mocked(hooks.useNodeMetrics).mockReturnValue(hookState({ data: {}, loading: true, error: "metrics failed" }) as never)
    const { rerender } = render(<Providers><Routes><Route path="/node/:id" element={<NodeDetail />} /></Routes></Providers>)
    expect(screen.getByText("Loading node...")).toBeInTheDocument()
    expect(screen.getByText("metrics failed")).toBeInTheDocument()
    vi.mocked(hooks.useNode).mockReturnValue(hookState({ data: { ...nodeWithTraffic(), alias: "Alias" } }) as never)
    vi.mocked(hooks.useNodeMetrics).mockReturnValue(hookState({ data: {} }) as never)
    rerender(<Providers><Routes><Route path="/node/:id" element={<NodeDetail />} /></Routes></Providers>)
    expect(screen.getByText("Alias")).toBeInTheDocument()
    expect(screen.getByRole("link", { name: "Back" })).toHaveAttribute("href", "/")
    expect(screen.getByText("25 GiB / 100 GiB")).toBeInTheDocument()
    fireEvent.click(screen.getByText("6h"))
    expect(hooks.useNodeMetrics).toHaveBeenLastCalledWith("node", undefined, 6)
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
    vi.mocked(hooks.useNode).mockReturnValue(hookState({ data: richNode }) as never)
    vi.mocked(hooks.useNodeMetrics).mockReturnValue(hookState({ data: {} }) as never)
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
