import type { ComponentType, ReactNode } from "react"
import { fireEvent, render, screen } from "@testing-library/react"
import { MemoryRouter, Route, Routes } from "react-router"
import { describe, expect, it, vi } from "vitest"
import { LocaleProvider } from "@/context/locale"
import { ThemeProvider } from "@/context/theme"
import { NodeCard } from "@/components/node-card"
import { MetricsChart } from "@/components/metrics-chart"
import { GroupTabs } from "@/components/group-tabs"
import { Header } from "@/components/header"
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select"
import { cn } from "@/lib/utils"
import { formatBytes, formatDuration, formatMetricValue } from "@/lib/metric-format"
import type { Node } from "@/types"

vi.mock("recharts", () => ({
  ResponsiveContainer: ({ children }: { children: ReactNode }) => <div>{children}</div>,
  LineChart: ({ children }: { children: ReactNode }) => <div>{children}</div>,
  Line: () => <span>line</span>,
  CartesianGrid: () => <span>grid</span>,
  XAxis: ({ tickFormatter, domain }: { tickFormatter: (value: number | string) => string; domain: number[] }) => (
    <span data-testid="x-axis" data-domain={domain.join(":")}>{tickFormatter(domain[0])}</span>
  ),
  YAxis: ({ tickFormatter }: { tickFormatter: (value: number) => string }) => (
    <span data-testid="y-axis">{tickFormatter(2048)}</span>
  ),
  Tooltip: ({
    labelFormatter, formatter,
  }: {
    labelFormatter: (value: number) => string
    formatter: (value: number) => [string, string]
  }) => <span data-testid="tooltip">{labelFormatter(1)}|{formatter(2048)[0]}</span>,
}))

const baseNode: Node = {
  id: "node-one", name: "node", alias: "alias", group_id: "group", host: "127.0.0.1",
  port: 22, status: "online", ssh_public_key: "", last_seen: "", created_at: "", updated_at: "",
  cpu_model: "Test CPU", os: "linux", platform: "ubuntu", os_version: "24.04",
  kernel: "6.8.0", arch: "x86_64", virtualization: "kvm guest", agent_version: "1.2.3",
  sort_order: 0, tags: ["edge", "prod"], is_public: true,
  public_remark: "Customer-facing edge node",
  traffic_limit: 100 * 1024 ** 3, traffic_limit_type: "sum", traffic_reset_day: 1,
  traffic: {
    sent: 10 * 1024 ** 3, received: 15 * 1024 ** 3, used: 25 * 1024 ** 3,
    limit: 100 * 1024 ** 3, remaining: 75 * 1024 ** 3, percentage: 25,
    limit_type: "sum", reset_day: 1, period_start: "2026-07-01T00:00:00Z",
    next_reset: "2026-08-01T00:00:00Z", tracked_since: "2026-07-01T00:00:00Z",
    status: "normal",
  },
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

function Providers({ children }: { children: ReactNode }) {
  return <ThemeProvider><LocaleProvider><MemoryRouter>{children}</MemoryRouter></LocaleProvider></ThemeProvider>
}

describe("shared components", () => {
  it("renders node metrics and navigates", () => {
    render(
      <ThemeProvider><LocaleProvider><MemoryRouter>
        <Routes>
          <Route path="/" element={<NodeCard node={baseNode} />} />
          <Route path="/node/:id" element={<p>detail</p>} />
        </Routes>
      </MemoryRouter></LocaleProvider></ThemeProvider>,
    )
    expect(screen.getByText("12.3%")).toBeInTheDocument()
    expect(screen.getByText("40 GiB / 100 GiB")).toBeInTheDocument()
    expect(screen.getByText("1 KiB/s")).toBeInTheDocument()
    expect(screen.getByText("2 KiB/s")).toBeInTheDocument()
    expect(screen.getByText("1h 0m")).toBeInTheDocument()
    expect(screen.getByText("25 GiB / 100 GiB")).toBeInTheDocument()
    expect(screen.getByText("edge")).toBeInTheDocument()
    expect(screen.getByText("prod")).toBeInTheDocument()
    expect(screen.getByText("Customer-facing edge node")).toBeInTheDocument()
    fireEvent.click(screen.getByText("alias"))
    expect(screen.getByText("detail")).toBeInTheDocument()
  })

  it("covers node skeleton, offline, missing, and MiB metrics", () => {
    const node = { ...baseNode, alias: "", status: "offline" as const, metrics: { net_recv: 2 * 1024 * 1024 } }
    const { rerender } = render(<Providers><NodeCard loading /></Providers>)
    rerender(<Providers><NodeCard node={node} /></Providers>)
    expect(screen.getByText("Offline")).toBeInTheDocument()
    expect(screen.getByText("2 MiB/s")).toBeInTheDocument()
    expect(screen.getAllByText("--").length).toBeGreaterThan(0)
  })

  it("renders empty and populated metric charts", () => {
    const { rerender } = render(
      <Providers>
        <MetricsChart title="CPU" color="#000" valueFormat="percent" from={0} to={3600} rangeHours={1} />
      </Providers>,
    )
    expect(screen.getByText("No measurements for this period")).toBeInTheDocument()
    rerender(
      <Providers>
        <MetricsChart
          title="Network"
          color="#000"
          valueFormat="bytes-per-second"
          from={0}
          to={604800}
          rangeHours={168}
          data={[{ timestamp: 1, value: 2048 }]}
        />
      </Providers>,
    )
    expect(screen.getByText("line")).toBeInTheDocument()
    expect(screen.getByTestId("x-axis")).toHaveAttribute("data-domain", "0:604800")
    expect(screen.getByTestId("y-axis")).toHaveTextContent("2 KiB/s")
    expect(screen.getByTestId("tooltip")).toHaveTextContent("2 KiB/s")
    expect(formatMetricValue(85.25, "percent")).toBe("85.3%")
    expect(formatMetricValue(2.25, "number")).toBe("2.3")
    expect(formatMetricValue(2 * 1024 * 1024, "bytes-per-second")).toBe("2 MiB/s")
    expect(formatBytes(0)).toBe("0 B")
    expect(formatBytes(1024)).toBe("1 KiB")
    expect(formatBytes(2 * 1024 ** 2)).toBe("2 MiB")
    expect(formatBytes(40 * 1024 ** 3)).toBe("40 GiB")
    expect(formatBytes(1.5 * 1024 ** 4)).toBe("1.5 TiB")
    expect(formatDuration(90061)).toBe("1d 1h")
  })

  it("changes groups", () => {
    const onChange = vi.fn()
    render(<Providers><GroupTabs groups={[{ id: "g", name: "Ops" } as never]} value="" onChange={onChange} /></Providers>)
    fireEvent.click(screen.getByText("Ops"))
    expect(onChange).toHaveBeenCalled()
    expect(onChange.mock.calls[0][0]).toBe("g")
  })

  it("renders a select label instead of its internal value", () => {
    const options = [{ label: "Visible name", value: "internal-id" }]
    render(
      <Select items={options} value="internal-id">
        <SelectTrigger><SelectValue /></SelectTrigger>
        <SelectContent>
          <SelectGroup>
            <SelectItem value="internal-id">Visible name</SelectItem>
          </SelectGroup>
        </SelectContent>
      </Select>,
    )
    expect(screen.getByText("Visible name")).toBeInTheDocument()
    expect(screen.queryByText("internal-id")).not.toBeInTheDocument()
  })

  it("never exposes a select internal value when its label is unavailable", () => {
    render(
      <Select items={[]} value="internal-id">
        <SelectTrigger><SelectValue placeholder="Select an option" /></SelectTrigger>
        <SelectContent />
      </Select>,
    )
    expect(screen.getByText("Select an option")).toBeInTheDocument()
    expect(screen.queryByText("internal-id")).not.toBeInTheDocument()
  })

  it("does not allow custom value content to expose an internal value", () => {
    const UnsafeSelectValue = SelectValue as unknown as ComponentType<{
      children: () => string
    }>
    const options = [{ label: "Visible name", value: "internal-id" }]
    render(
      <Select items={options} value="internal-id">
        <SelectTrigger><UnsafeSelectValue>{() => "internal-id"}</UnsafeSelectValue></SelectTrigger>
        <SelectContent />
      </Select>,
    )
    expect(screen.getByText("Visible name")).toBeInTheDocument()
    expect(screen.queryByText("internal-id")).not.toBeInTheDocument()
  })

  it("resolves a selected label after asynchronous options arrive", () => {
    const view = (items: Array<{ label: string; value: string }>) => (
      <Select items={items} value="internal-id">
        <SelectTrigger><SelectValue placeholder="Loading options" /></SelectTrigger>
        <SelectContent />
      </Select>
    )
    const rendered = render(view([]))
    expect(screen.getByText("Loading options")).toBeInTheDocument()
    rendered.rerender(view([{ label: "Loaded name", value: "internal-id" }]))
    expect(screen.getByText("Loaded name")).toBeInTheDocument()
    expect(screen.queryByText("internal-id")).not.toBeInTheDocument()
  })

  it("toggles header theme and locale", () => {
    render(<Providers><Header /></Providers>)
    expect(screen.getByTitle("Admin").closest("a")).toHaveAttribute("href", "/admin")
    fireEvent.click(screen.getByTitle("Theme"))
    expect(document.documentElement).toHaveClass("dark")
    fireEvent.click(screen.getByTitle("Language"))
    expect(screen.getByText("EN")).toBeInTheDocument()
  })

  it("merges utility classes", () => {
    const hidden = false
    expect(cn("p-2", hidden && "hidden", "p-4")).toContain("p-4")
  })
})
