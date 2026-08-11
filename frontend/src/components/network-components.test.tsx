import { createContext, useContext, type ReactNode } from "react"
import { fireEvent, render, screen, waitFor } from "@testing-library/react"
import { beforeEach, describe, expect, it, vi } from "vitest"

import { NetworkHistoryDialog, NetworkHistoryChart, formatHistoryTime } from "@/components/network-history-dialog"
import { NetworkQualityBand } from "@/components/network-quality-band"
import { NetworkTaskCard } from "@/components/network-task-card"
import { NetworkTaskDialog } from "@/components/network-task-dialog"
import { LocaleProvider } from "@/context/locale"
import * as networkHooks from "@/hooks/use-network"
import type { NetworkTaskView, Node } from "@/types"

vi.mock("@/hooks/use-network")
vi.mock("@/components/ui/dialog", async () => {
  const mocks = await vi.importActual<typeof import("@/test/admin-ui-mocks")>("@/test/admin-ui-mocks")
  return {
    Dialog: mocks.MockDialog,
    DialogContent: mocks.PassThrough,
    DialogHeader: mocks.PassThrough,
    DialogTitle: mocks.PassThrough,
    DialogDescription: mocks.PassThrough,
    DialogFooter: mocks.PassThrough,
  }
})
vi.mock("@/components/ui/select", async () => {
  const mocks = await vi.importActual<typeof import("@/test/admin-ui-mocks")>("@/test/admin-ui-mocks")
  return {
    Select: mocks.MockSelect,
    SelectContent: mocks.PassThrough,
    SelectGroup: mocks.PassThrough,
    SelectItem: mocks.MockSelectItem,
    SelectTrigger: () => null,
    SelectValue: () => null,
  }
})
vi.mock("@/components/ui/toggle-group", () => {
  const ToggleContext = createContext<{ value: string[]; change?: (value: string[]) => void }>({ value: [] })
  return {
    ToggleGroup: ({ children, value = [], onValueChange }: {
      children: ReactNode
      value?: string[]
      onValueChange?: (value: string[]) => void
    }) => <ToggleContext.Provider value={{ value, change: onValueChange }}>{children}</ToggleContext.Provider>,
    ToggleGroupItem: ({ children, value, ...props }: { children: ReactNode; value: string }) => {
      const context = useContext(ToggleContext)
      return <button {...props} aria-pressed={context.value.includes(value)} onClick={() => context.change?.([value])}>{children}</button>
    },
  }
})
vi.mock("@/components/ui/switch", () => ({
  Switch: ({ checked, onCheckedChange, ...props }: {
    checked: boolean
    onCheckedChange?: (checked: boolean) => void
  }) => <button role="switch" aria-checked={checked} onClick={() => onCheckedChange?.(!checked)} {...props}>toggle</button>,
}))
vi.mock("@/components/ui/checkbox", () => ({
  Checkbox: ({ checked, onCheckedChange, ...props }: {
    checked: boolean
    onCheckedChange?: (checked: boolean) => void
  }) => <input type="checkbox" checked={checked} onChange={(event) => onCheckedChange?.(event.target.checked)} {...props} />,
}))
vi.mock("@/components/ui/scroll-area", () => ({ ScrollArea: ({ children }: { children: ReactNode }) => <div>{children}</div> }))
vi.mock("recharts", () => ({
  ResponsiveContainer: ({ children }: { children: ReactNode }) => <div>{children}</div>,
  LineChart: ({ children }: { children: ReactNode }) => <div>{children}</div>,
  Line: () => <span>line</span>,
  CartesianGrid: () => <span>grid</span>,
  XAxis: ({ domain, tickFormatter }: { domain: number[]; tickFormatter: (value: number) => string }) => (
    <span data-testid="network-x-axis" data-domain={domain.join(":")}>{tickFormatter(domain[0])}</span>
  ),
  YAxis: ({ tickFormatter }: { tickFormatter: (value: number) => string }) => (
    <span data-testid="network-y-axis">{tickFormatter(1500)}</span>
  ),
  Tooltip: ({ formatter, labelFormatter }: {
    formatter: (value: number) => [string, string]
    labelFormatter: (value: number) => string
  }) => <span>{formatter(1500)[0]} {labelFormatter(1)}</span>,
}))

const view = networkView()
const nodes: Node[] = [
  {
    id: "node-secret-id",
    name: "node-one",
    alias: "Edge",
    group_id: "",
    host: "::1",
    port: 22,
    status: "online",
    ssh_public_key: "",
    cpu_model: "",
    os: "",
    platform: "",
    os_version: "",
    kernel: "",
    arch: "",
    virtualization: "",
    agent_version: "",
    sort_order: 0,
    tags: [],
    is_public: true,
    public_remark: "",
    last_seen: "",
    created_at: "",
    updated_at: "",
  },
]

function providers(children: ReactNode) {
  return render(<LocaleProvider>{children}</LocaleProvider>)
}

describe("network public components", () => {
  beforeEach(() => {
    vi.resetAllMocks()
    vi.mocked(networkHooks.usePublicNetworkQuality).mockReturnValue({
      data: [view], loading: false, error: null, refresh: vi.fn(),
    } as never)
    vi.mocked(networkHooks.useNetworkHistory).mockReturnValue({
      data: {
        task_id: "task-id",
        node_id: "node-secret-id",
        from: "",
        to: "",
        points: [{ timestamp: "2026-07-29T12:00:00Z", average_latency_ms: 12, success_percent: 100, sample_count: 1 }],
      },
      loading: false,
      error: null,
      from: 100,
      to: 200,
      refresh: vi.fn(),
    })
  })

  it("shows task and node labels without exposing IDs, then opens history", () => {
    providers(<NetworkQualityBand />)
    expect(screen.getByText("Primary API")).toBeInTheDocument()
    expect(screen.getByText("Edge (node-one)")).toBeInTheDocument()
    expect(screen.queryByText("node-secret-id")).not.toBeInTheDocument()
    fireEvent.click(screen.getByRole("button", { name: "Edge (node-one) History" }))
    expect(screen.getByRole("dialog")).toBeInTheDocument()
    expect(screen.getByTestId("network-x-axis")).toHaveAttribute("data-domain", "100:200")
    expect(screen.getByTestId("network-y-axis")).toHaveTextContent("1.5 s")
  })

  it("renders loading, error, empty, and unassigned states", () => {
    vi.mocked(networkHooks.usePublicNetworkQuality).mockReturnValue({ data: null, loading: true, error: null } as never)
    const rendered = providers(<NetworkQualityBand />)
    expect(document.querySelectorAll("[data-slot=skeleton]").length).toBeGreaterThan(0)
    vi.mocked(networkHooks.usePublicNetworkQuality).mockReturnValue({ data: null, loading: false, error: "quality failed" } as never)
    rendered.rerender(<LocaleProvider><NetworkQualityBand /></LocaleProvider>)
    expect(screen.getByText("quality failed")).toBeInTheDocument()
    vi.mocked(networkHooks.usePublicNetworkQuality).mockReturnValue({ data: [], loading: false, error: null } as never)
    rendered.rerender(<LocaleProvider><NetworkQualityBand /></LocaleProvider>)
    expect(screen.getByText("No public network tasks")).toBeInTheDocument()
    vi.mocked(networkHooks.usePublicNetworkQuality).mockReturnValue({
      data: [{ ...view, nodes: [] }], loading: false, error: null,
    } as never)
    rendered.rerender(<LocaleProvider><NetworkQualityBand /></LocaleProvider>)
    expect(screen.getByText("No assigned nodes")).toBeInTheDocument()
  })

  it("switches history node and range", () => {
    const onOpenChange = vi.fn()
    providers(<NetworkHistoryDialog open view={view} initialNodeId="node-secret-id" onOpenChange={onOpenChange} />)
    const selects = screen.getAllByTestId("select")
    expect(selects[0]).toHaveAttribute("data-selected-label", "Edge (node-one)")
    fireEvent.click(screen.getByRole("button", { name: "7d" }))
    expect(networkHooks.useNetworkHistory).toHaveBeenLastCalledWith(expect.objectContaining({ rangeHours: 168 }))

    expect(formatHistoryTime(1, 168)).not.toBe("")
    expect(formatHistoryTime(1, 1)).not.toBe("")
  })

  it("renders history loading, error, and empty states", () => {
    vi.mocked(networkHooks.useNetworkHistory).mockReturnValue({ loading: true, error: null, data: null, from: 0, to: 1 } as never)
    const rendered = providers(<NetworkHistoryDialog open view={view} onOpenChange={vi.fn()} />)
    expect(document.querySelector("[data-slot=skeleton]")).toBeInTheDocument()
    vi.mocked(networkHooks.useNetworkHistory).mockReturnValue({ loading: false, error: "history failed", data: null, from: 0, to: 1 } as never)
    rendered.rerender(<LocaleProvider><NetworkHistoryDialog open view={view} onOpenChange={vi.fn()} /></LocaleProvider>)
    expect(screen.getByText("history failed")).toBeInTheDocument()
    vi.mocked(networkHooks.useNetworkHistory).mockReturnValue({ loading: false, error: null, data: { points: [] }, from: 0, to: 1 } as never)
    rendered.rerender(<LocaleProvider><NetworkHistoryDialog open view={view} onOpenChange={vi.fn()} /></LocaleProvider>)
    expect(screen.getByText("No samples in this time range")).toBeInTheDocument()
  })

  it("renders chart fixed domain directly", () => {
    providers(<NetworkHistoryChart data={[{ timestamp: 1, latency: 1500, success: 100 }]} from={1} to={2} rangeHours={1} latencyLabel="Latency" />)
    expect(screen.getByTestId("network-x-axis")).toHaveAttribute("data-domain", "1:2")
    expect(screen.getAllByText(/1.5 s/)).toHaveLength(2)
  })
})

describe("network admin components", () => {
  it("renders task facts and invokes every card action", () => {
    const actions = Array.from({ length: 5 }, () => vi.fn())
    providers(
      <NetworkTaskCard
        view={view}
        first={false}
        last={false}
        onMoveUp={actions[0]}
        onMoveDown={actions[1]}
        onHistory={actions[2]}
        onEdit={actions[3]}
        onDelete={actions[4]}
      />,
    )
    expect(screen.getByText("100%")).toBeInTheDocument()
    ;["Move up", "Move down", "History", "Edit", "Delete"].forEach((name) => fireEvent.click(screen.getByRole("button", { name })))
    actions.forEach((action) => expect(action).toHaveBeenCalled())
  })

  it("uses human labels in the task form and saves assignment state", async () => {
    const onSave = vi.fn().mockResolvedValue(undefined)
    providers(
      <NetworkTaskDialog
        open
        view={null}
        nodes={nodes}
        loading={false}
        error={null}
        nextSortOrder={3}
        onOpenChange={vi.fn()}
        onSave={onSave}
      />,
    )
    const selects = screen.getAllByTestId("select")
    expect(selects[0]).toHaveAttribute("data-selected-label", "ICMP")
    expect(selects[1]).toHaveAttribute("data-selected-label", "Automatic")
    expect(screen.queryByText("node-secret-id")).not.toBeInTheDocument()
    fireEvent.change(screen.getByLabelText("Task name"), { target: { value: "Probe" } })
    fireEvent.change(screen.getByLabelText("Target"), { target: { value: "example.com" } })
    fireEvent.click(screen.getByRole("button", { name: "Selected nodes" }))
    fireEvent.click(screen.getByLabelText("Edge (node-one)"))
    const switches = screen.getAllByRole("switch")
    fireEvent.click(switches[0])
    fireEvent.click(switches[1])
    fireEvent.click(screen.getByRole("button", { name: "Save" }))
    await waitFor(() => expect(onSave).toHaveBeenCalledWith(expect.objectContaining({
      name: "Probe",
      target: "example.com",
      all_nodes: false,
      enabled: false,
      is_public: false,
      sort_order: 3,
      node_ids: ["node-secret-id"],
    })))
  })

  it("validates required and timing fields", () => {
    providers(
      <NetworkTaskDialog open view={null} nodes={[]} loading={false} error="server failed" nextSortOrder={0} onOpenChange={vi.fn()} onSave={vi.fn()} />,
    )
    expect(screen.getByText("server failed")).toBeInTheDocument()
    fireEvent.click(screen.getByRole("button", { name: "Save" }))
    expect(screen.getByText("Task name and target are required.")).toBeInTheDocument()
  })

  it("loads edit labels, filters nodes, and validates every timing boundary", async () => {
    const onSave = vi.fn().mockResolvedValue(undefined)
    const editView: NetworkTaskView = {
      ...view,
      task: {
        ...view.task,
        type: "tcp",
        target: "example.com:443",
        ip_family: "ipv6",
        all_nodes: false,
        enabled: false,
        is_public: false,
        nodes: [{ id: nodes[0].id, name: nodes[0].name, alias: nodes[0].alias }],
      },
    }
    providers(<NetworkTaskDialog open view={editView} nodes={nodes} loading={false} error={null} nextSortOrder={9} onOpenChange={vi.fn()} onSave={onSave} />)
    const selects = screen.getAllByTestId("select")
    expect(selects[0]).toHaveAttribute("data-selected-label", "TCP")
    expect(selects[1]).toHaveAttribute("data-selected-label", "IPv6")
    expect(screen.getByLabelText("Edge (node-one)")).toBeChecked()
    fireEvent.change(screen.getByPlaceholderText("Search"), { target: { value: "missing" } })
    expect(screen.queryByLabelText("Edge (node-one)")).not.toBeInTheDocument()
    fireEvent.change(screen.getByPlaceholderText("Search"), { target: { value: "Edge" } })
    fireEvent.click(screen.getByLabelText("Edge (node-one)"))

    const interval = screen.getByLabelText("Interval (seconds)")
    const timeout = screen.getByLabelText("Timeout (ms)")
    fireEvent.change(interval, { target: { value: "5" } })
    fireEvent.click(screen.getByRole("button", { name: "Save" }))
    expect(screen.getByText("Interval must be between 10 and 86400 seconds.")).toBeInTheDocument()
    fireEvent.change(interval, { target: { value: "10" } })
    fireEvent.change(timeout, { target: { value: "30001" } })
    fireEvent.click(screen.getByRole("button", { name: "Save" }))
    expect(screen.getByText("Timeout must be between 100 and 30000 milliseconds.")).toBeInTheDocument()
    fireEvent.change(timeout, { target: { value: "15000" } })
    fireEvent.click(screen.getByRole("button", { name: "Save" }))
    expect(screen.getByText("Timeout cannot exceed the interval.")).toBeInTheDocument()
    fireEvent.change(timeout, { target: { value: "10000" } })
    fireEvent.click(screen.getByRole("button", { name: "Save" }))
    await waitFor(() => expect(onSave).toHaveBeenCalledWith(expect.objectContaining({ node_ids: [] })))
  })
})

function networkView(): NetworkTaskView {
  return {
    task: {
      id: "task-id",
      name: "Primary API",
      type: "http",
      target: "https://example.com/health",
      ip_family: "auto",
      interval_seconds: 60,
      timeout_milliseconds: 3000,
      all_nodes: true,
      enabled: true,
      is_public: true,
      sort_order: 0,
      nodes: [],
      created_at: "",
      updated_at: "",
    },
    nodes: [{
      node: { id: "node-secret-id", name: "node-one", alias: "Edge" },
      latest: { timestamp: "", latency_ms: 18, success: true, status_code: 204, error_code: "none" },
    }],
  }
}
