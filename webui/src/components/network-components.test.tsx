import { createContext, useContext, useState, type ReactNode } from "react"
import { fireEvent, render, screen, waitFor } from "@testing-library/react"
import { beforeEach, describe, expect, it, vi } from "vitest"

import { NetworkHistoryDialog, NetworkHistoryChart, buildChartData, formatHistoryTime } from "@/components/network-history-dialog"
import { NetworkQualityBand } from "@/components/network-quality-band"
import { NetworkTaskCard } from "@/components/network-task-card"
import { NetworkTaskDialog } from "@/components/network-task-dialog"
import { NetworkTargetEditor } from "@/components/network-task-targets"
import { LocaleProvider, useLocale } from "@/context/locale"
import * as networkHooks from "@/hooks/use-network"
import type { NetworkHistorySeries, NetworkTarget, NetworkTaskView, Node } from "@/types"

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
  Line: ({ name, stroke }: { name: string; stroke: string }) => (
    <span data-testid="network-line" data-name={name} data-stroke={stroke}>line</span>
  ),
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
  Legend: () => <span data-testid="network-legend">legend</span>,
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
        series: [{
          target_id: "target-id",
          label: "移动",
          carrier: "mobile",
          region: "",
          visible_address: false,
          points: [{ timestamp: "2026-07-29T12:00:00Z", average_latency_ms: 12, success_percent: 100, sample_count: 1 }],
        }],
      },
      loading: false,
      error: null,
      from: 100,
      to: 200,
      refresh: vi.fn(),
    })
  })

  it("shows task and node labels without exposing IDs, then opens history", async () => {
    providers(<NetworkQualityBand />)
    expect(screen.getAllByText("Primary API").length).toBeGreaterThan(0)
    expect(screen.getByText("Edge (node-one)")).toBeInTheDocument()
    expect(screen.queryByText("node-secret-id")).not.toBeInTheDocument()
    fireEvent.click(screen.getByRole("button", { name: "Edge (node-one) History" }))
    expect(await screen.findByRole("dialog")).toBeInTheDocument()
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
    providers(<NetworkHistoryChart
      data={[{ timestamp: 1, "latency-0": 1500, "quality-0": 100 }]}
      series={[{ key: "0", label: "移动", carrier: "mobile", color: "#2563eb" }]}
      metric="latency"
      from={1}
      to={2}
      rangeHours={1}
      latencyLabel="Latency"
      qualityLabel="Success"
    />)
    expect(screen.getByTestId("network-x-axis")).toHaveAttribute("data-domain", "1:2")
    expect(screen.getByText("1.5 s")).toBeInTheDocument()
  })

  it("renders three independent carrier lines and breaks failed samples", () => {
    const history: NetworkHistorySeries[] = [
      {
        target_id: "mobile", label: "移动", carrier: "mobile", region: "上海", visible_address: false,
        points: [
          { timestamp: "2026-07-29T12:00:00Z", average_latency_ms: 20, success_percent: 100, sample_count: 1 },
          { timestamp: "2026-07-29T12:01:00Z", average_latency_ms: 99, success_percent: 0, sample_count: 1 },
        ],
      },
      {
        target_id: "unicom", label: "联通", carrier: "unicom", region: "北京", visible_address: false,
        points: [{ timestamp: "2026-07-29T12:00:00Z", average_latency_ms: 30, success_percent: 100, sample_count: 1 }],
      },
      {
        target_id: "telecom", label: "电信", carrier: "telecom", region: "广州", visible_address: false,
        points: [{ timestamp: "2026-07-29T12:00:00Z", average_latency_ms: 40, success_percent: 100, sample_count: 1 }],
      },
    ]
    vi.mocked(networkHooks.useNetworkHistory).mockReturnValue({
      data: { task_id: "task-id", node_id: "node-secret-id", from: "", to: "", series: [...history] },
      loading: false, error: null, from: 100, to: 200, refresh: vi.fn(),
    } as never)
    providers(<NetworkHistoryDialog open view={view} initialNodeId="node-secret-id" onOpenChange={vi.fn()} />)
    const lines = screen.getAllByTestId("network-line")
    expect(lines).toHaveLength(3)
    expect(lines.map((line) => line.getAttribute("data-name"))).toEqual(["移动", "联通", "电信"])
    expect(lines.map((line) => line.getAttribute("data-stroke"))).toEqual(["#2563eb", "#dc2626", "#16a34a"])
    const chart = buildChartData([...history], "latency", "success")
    expect(chart.data.find((row) => row.timestamp === Date.parse("2026-07-29T12:01:00Z") / 1000)?.["latency-0"]).toBeNull()
  })
})

describe("network admin components", () => {
  it("edits, toggles, removes, and adds target group entries", () => {
    providers(<TargetEditorHarness />)
    const value = () => JSON.parse(screen.getByTestId("targets").textContent ?? "[]") as NetworkTarget[]

    fireEvent.change(screen.getByLabelText("Target label"), { target: { value: "上海移动" } })
    fireEvent.change(screen.getByLabelText("Region"), { target: { value: "Shanghai" } })
    fireEvent.change(screen.getByLabelText("Target"), { target: { value: "127.0.0.1" } })
    fireEvent.change(screen.getByLabelText("ICMP packets"), { target: { value: "7" } })
    const selects = screen.getAllByTestId("select")
    fireEvent.change(selects[0], { target: { value: "telecom" } })
    fireEvent.change(selects[1], { target: { value: "ipv6" } })
    const switches = screen.getAllByRole("switch")
    fireEvent.click(switches[0])
    fireEvent.click(switches[1])
    expect(value()[0]).toMatchObject({ label: "上海移动", carrier: "telecom", region: "Shanghai", target: "127.0.0.1", ip_family: "ipv6", packet_count: 7, visible_address: true, enabled: false })

    fireEvent.click(screen.getByRole("button", { name: "Remove target" }))
    expect(screen.getByText(/No targets yet/)).toBeInTheDocument()
    fireEvent.click(screen.getByRole("button", { name: "Add custom target" }))
    expect(value()).toHaveLength(1)
    fireEvent.click(screen.getByRole("button", { name: "Add three carriers" }))
    expect(value().map((target) => target.carrier)).toEqual(["mobile", "unicom", "telecom"])
  })

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
    expect(screen.getByText("Agent → target")).toBeInTheDocument()
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
    expect(selects[0]).toHaveAttribute("data-selected-label", "Agent → target")
    expect(selects[1]).toHaveAttribute("data-selected-label", "ICMP")
    expect(selects[2]).toHaveAttribute("data-selected-label", "Automatic")
    expect(screen.queryByText("node-secret-id")).not.toBeInTheDocument()
    expect(screen.getByText("Selected Agents probe the external target, measuring their egress path.")).toBeInTheDocument()
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
      direction: "agent_to_target",
      target: "example.com",
      packet_count: 5,
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

  it("supports server-side targets and validates ICMP packet counts", async () => {
    const onSave = vi.fn().mockResolvedValue(undefined)
    providers(<NetworkTaskDialog open view={null} nodes={[]} loading={false} error={null} nextSortOrder={0} onOpenChange={vi.fn()} onSave={onSave} />)
    fireEvent.change(screen.getByLabelText("Task name"), { target: { value: "Server probe" } })
    const selects = screen.getAllByTestId("select")
    fireEvent.change(selects[0], { target: { value: "server_to_agent" } })
    expect(screen.getByText("The server probes each node's registered address; node placeholders are generated automatically.")).toBeInTheDocument()
    expect(screen.getByLabelText("Target")).toHaveValue("{{host}}")
    fireEvent.change(screen.getByLabelText("ICMP packets"), { target: { value: "21" } })
    fireEvent.click(screen.getByRole("button", { name: "Save" }))
    expect(screen.getByText("ICMP packet count must be between 1 and 20.")).toBeInTheDocument()
    fireEvent.change(screen.getByLabelText("ICMP packets"), { target: { value: "3" } })
    fireEvent.click(screen.getByRole("button", { name: "Save" }))
    await waitFor(() => expect(onSave).toHaveBeenCalledWith(expect.objectContaining({
      direction: "server_to_agent", target: "{{host}}", packet_count: 3,
    })))
  })

  it("normalizes a three-carrier HTTP task before saving it as TCP", async () => {
    const onSave = vi.fn().mockResolvedValue(undefined)
    const httpView: NetworkTaskView = {
      ...view,
      task: {
        ...view.task,
        type: "http",
        target_mode: "group",
        target: "http://gd-cm-v4.ip.zstaticcdn.com:80",
        targets: [
          { id: "mobile", label: "移动", carrier: "mobile", region: "", target: "http://gd-cm-v4.ip.zstaticcdn.com:80", ip_family: "auto", packet_count: 3, visible_address: false, enabled: true, sort_order: 0 },
          { id: "unicom", label: "联通", carrier: "unicom", region: "", target: "http://gd-cu-v4.ip.zstaticcdn.com:80", ip_family: "auto", packet_count: 3, visible_address: false, enabled: true, sort_order: 1 },
          { id: "telecom", label: "电信", carrier: "telecom", region: "", target: "http://gd-ct-v4.ip.zstaticcdn.com:80", ip_family: "auto", packet_count: 3, visible_address: false, enabled: true, sort_order: 2 },
        ],
      },
    }
    providers(<NetworkTaskDialog open view={httpView} nodes={[]} loading={false} error={null} nextSortOrder={1} onOpenChange={vi.fn()} onSave={onSave} />)

    const selects = screen.getAllByTestId("select")
    fireEvent.change(selects[1], { target: { value: "tcp" } })
    expect(screen.getAllByLabelText("Target").map((input) => input.getAttribute("value"))).toEqual([
      "gd-cm-v4.ip.zstaticcdn.com:80",
      "gd-cu-v4.ip.zstaticcdn.com:80",
      "gd-ct-v4.ip.zstaticcdn.com:80",
    ])
    fireEvent.click(screen.getByRole("button", { name: "Save" }))

    await waitFor(() => expect(onSave).toHaveBeenCalledWith(expect.objectContaining({
      type: "tcp",
      targets: expect.arrayContaining([
        expect.objectContaining({ carrier: "mobile", target: "gd-cm-v4.ip.zstaticcdn.com:80" }),
        expect.objectContaining({ carrier: "unicom", target: "gd-cu-v4.ip.zstaticcdn.com:80" }),
        expect.objectContaining({ carrier: "telecom", target: "gd-ct-v4.ip.zstaticcdn.com:80" }),
      ]),
    })))
  })

  it("normalizes a full-width TCP colon before saving", async () => {
    const onSave = vi.fn().mockResolvedValue(undefined)
    providers(<NetworkTaskDialog open view={null} nodes={[]} loading={false} error={null} nextSortOrder={0} onOpenChange={vi.fn()} onSave={onSave} />)
    fireEvent.change(screen.getByLabelText("Task name"), { target: { value: "TCP probe" } })
    fireEvent.change(screen.getAllByTestId("select")[1], { target: { value: "tcp" } })
    fireEvent.change(screen.getByLabelText("Target"), { target: { value: "example.test：443" } })
    fireEvent.click(screen.getByRole("button", { name: "Save" }))

    await waitFor(() => expect(onSave).toHaveBeenCalledWith(expect.objectContaining({
      type: "tcp",
      target: "example.test:443",
    })))
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
    expect(selects[0]).toHaveAttribute("data-selected-label", "Agent → target")
    expect(selects[1]).toHaveAttribute("data-selected-label", "TCP")
    expect(selects[2]).toHaveAttribute("data-selected-label", "IPv6")
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

  it("creates three carrier targets with hidden addresses and keeps them independent", async () => {
    const onSave = vi.fn().mockResolvedValue(undefined)
    providers(<NetworkTaskDialog open view={null} nodes={[]} loading={false} error={null} nextSortOrder={0} onOpenChange={vi.fn()} onSave={onSave} />)
    fireEvent.change(screen.getByLabelText("Task name"), { target: { value: "三网质量" } })
    fireEvent.click(screen.getByRole("button", { name: "Three-carrier group" }))
    expect(screen.getAllByLabelText("Target")).toHaveLength(3)
    for (let index = 0; index < 3; index += 1) {
      fireEvent.change(screen.getByLabelText(`Target`, { selector: `#target-address-${index}` }), { target: { value: "127.0.0.1" } })
    }
    fireEvent.click(screen.getByRole("button", { name: "Save" }))
    await waitFor(() => expect(onSave).toHaveBeenCalledWith(expect.objectContaining({
      target_mode: "group",
      targets: expect.arrayContaining([
        expect.objectContaining({ carrier: "mobile", target: "127.0.0.1", visible_address: false }),
        expect.objectContaining({ carrier: "unicom", target: "127.0.0.1", visible_address: false }),
        expect.objectContaining({ carrier: "telecom", target: "127.0.0.1", visible_address: false }),
      ]),
    })))
  })
})

function TargetEditorHarness() {
  const { t } = useLocale()
  const [targets, setTargets] = useState<NetworkTarget[]>([{
    id: "", label: "Primary", carrier: "mobile", region: "", target: "example.com",
    ip_family: "auto", packet_count: 5, visible_address: false, enabled: true, sort_order: 0,
  }])
  return (
    <>
      <NetworkTargetEditor targets={targets} type="icmp" t={t} onChange={setTargets} />
      <output data-testid="targets">{JSON.stringify(targets)}</output>
    </>
  )
}

function networkView(): NetworkTaskView {
  return {
    task: {
      id: "task-id",
      name: "Primary API",
      direction: "agent_to_target",
      type: "http",
      target_mode: "single",
      target: "https://example.com/health",
      targets: [{
        id: "target-id", label: "Primary API", carrier: "custom", region: "",
        target: "https://example.com/health", ip_family: "auto", packet_count: 1,
        visible_address: false, enabled: true, sort_order: 0,
      }],
      ip_family: "auto",
      interval_seconds: 60,
      timeout_milliseconds: 3000,
      packet_count: 1,
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
      targets: [{ target_id: "target-id", latest: { timestamp: "", latency_ms: 18, success: true, status_code: 204, error_code: "none" } }],
    }],
  }
}
