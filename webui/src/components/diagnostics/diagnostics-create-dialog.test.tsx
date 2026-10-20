import { Children, createContext, isValidElement, useContext, type ReactNode } from "react"
import { fireEvent, render, screen, waitFor } from "@testing-library/react"
import { beforeEach, describe, expect, it, vi } from "vitest"

import { DiagnosticsCreateDialog } from "@/components/diagnostics/diagnostics-create-dialog"
import { LocaleProvider } from "@/context/locale"
import * as diagnosticsApi from "@/lib/diagnostics-api"
import type { DiagnosticCapability, DiagnosticPolicy, DiagnosticTarget } from "@/lib/diagnostics-api"

vi.mock("@/lib/diagnostics-api")

const ToggleContext = createContext<{ value: string; change?: (value: string) => void }>({ value: "" })

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
    SelectItem: mocks.MockSelectItem,
    SelectTrigger: mocks.PassThrough,
    SelectValue: mocks.PassThrough,
  }
})
vi.mock("@/components/ui/toggle-group", () => ({
  ToggleGroup: ({ children, value, onValueChange }: { children: ReactNode; value: string[]; onValueChange: (value: string[]) => void }) => {
    const items: ReactNode[] = []
    Children.forEach(children, (child) => {
      if (!isValidElement<{ value?: string; disabled?: boolean; children?: ReactNode; "aria-label"?: string }>(child)) return
      items.push(
        <button
          key={child.props.value ?? items.length}
          disabled={child.props.disabled}
          aria-pressed={child.props.value === value[0]}
          aria-label={child.props["aria-label"]}
          onClick={() => onValueChange([String(child.props.value)])}
        >
          {child.props.children}
        </button>,
      )
    })
    return <ToggleContext.Provider value={{ value: value[0], change: (next) => onValueChange([next]) }}>{items}</ToggleContext.Provider>
  },
  ToggleGroupItem: ({ children }: { children: ReactNode }) => {
    const context = useContext(ToggleContext)
    return <span data-value={context.value}>{children}</span>
  },
}))
vi.mock("@/components/ui/checkbox", () => ({
  Checkbox: ({ id, checked, onCheckedChange }: { id: string; checked?: boolean; onCheckedChange?: (checked: boolean) => void }) => (
    <input id={id} type="checkbox" checked={Boolean(checked)} onChange={(event) => onCheckedChange?.(event.target.checked)} />
  ),
}))

function policy(overrides: Partial<DiagnosticPolicy> = {}): DiagnosticPolicy {
  return {
    id: "policy-1", enabled: true, allowed_kinds: ["mtr", "iperf3"], allow_udp: false, allow_reverse: false,
    allow_private_targets: false, maximum_active_jobs: 4, maximum_active_jobs_per_node: 1,
    daily_byte_budget: 1_000_000, maximum_iperf_duration_seconds: 30, maximum_iperf_bitrate_mbps: 50,
    revision: 1, updated_by: "u1", updated_at: "2026-10-15T00:00:00Z", ...overrides,
  }
}

function target(overrides: Partial<DiagnosticTarget> = {}): DiagnosticTarget {
  return {
    id: "target-1", name: "Zurich edge", purpose: "latency", kinds: ["mtr", "iperf3"], address: "203.0.113.10",
    port: 5201, ip_family: "ipv4", protocol: "tcp", direction: "forward", private_address: false,
    allowed_addresses: [], resolved_addresses: [], resolved_at: null, maximum_bitrate_mbps: 40,
    maximum_duration_seconds: 20, maximum_parallel_streams: 2, daily_byte_budget: 1_000_000,
    enabled: true, expires_at: null, revision: 1, created_by: "u1", updated_by: "u1",
    created_at: "2026-10-15T00:00:00Z", updated_at: "2026-10-15T00:00:00Z", ...overrides,
  }
}

function capability(nodeId: string, kinds: DiagnosticCapability["kinds"]): DiagnosticCapability {
  return { node_id: nodeId, node_name: nodeId, digest: "digest", seen_at: "2026-10-15T00:00:00Z", state: "ready", kinds }
}

const nodes = [{ id: "node-1", name: "Tokyo" }, { id: "node-2", name: "Osaka" }]
const onCreated = vi.fn()

function view(element: ReactNode) {
  return render(<LocaleProvider>{element}</LocaleProvider>)
}

function selectByIndex(index: number): HTMLSelectElement {
  return screen.getAllByTestId("select")[index] as HTMLSelectElement
}

const createJob = vi.mocked(diagnosticsApi.createDiagnosticJob)

describe("diagnostics create dialog", () => {
  beforeEach(() => {
    vi.resetAllMocks()
    createJob.mockResolvedValue({ id: "job-new" } as never)
    onCreated.mockResolvedValue(undefined)
  })

  it("offers only ready nodes and approved targets and never a free-form destination", () => {
    view(
      <DiagnosticsCreateDialog open onOpenChange={() => undefined} policy={policy()} targets={[target()]}
        capabilities={[capability("node-1", ["mtr"]), capability("node-2", ["iperf3"])]} nodes={nodes} onCreated={onCreated} />,
    )
    expect(selectByIndex(0).querySelectorAll("option")).toHaveLength(1)
    expect(selectByIndex(0)).toHaveTextContent("Tokyo")
    expect(selectByIndex(1)).toHaveTextContent("Zurich edge")
    expect(screen.queryByLabelText(/address/i)).toBeNull()
    expect(screen.queryByPlaceholderText(/example|https?:/i)).toBeNull()
  })

  it("clamps values to policy and target maxima and submits the diagnostic", async () => {
    view(
      <DiagnosticsCreateDialog open onOpenChange={() => undefined} policy={policy()} targets={[target()]}
        capabilities={[capability("node-1", ["mtr"])]} nodes={nodes} onCreated={onCreated} />,
    )
    // 目标带宽上限低于表单默认值时仍然可以提交 MTR：带宽上限只约束 iperf3。
    expect(screen.queryByLabelText("Bitrate (Mbps)")).toBeNull()
    fireEvent.change(screen.getByLabelText("Probes"), { target: { value: "999" } })
    fireEvent.click(screen.getByRole("button", { name: "Queue job" }))
    await waitFor(() => expect(createJob).toHaveBeenCalledTimes(1))
    expect(createJob.mock.calls[0][0]).toMatchObject({ source_node_id: "node-1", target_id: "target-1", kind: "mtr", probes: 20, hop_limit: 32, protocol: "icmp" })
    await waitFor(() => expect(onCreated).toHaveBeenCalled())
  })

  it("requires the bandwidth confirmation before an iperf3 job can be queued", async () => {
    view(
      <DiagnosticsCreateDialog open onOpenChange={() => undefined} policy={policy()} targets={[target()]}
        capabilities={[capability("node-1", ["iperf3"])]} nodes={nodes} onCreated={onCreated} />,
    )
    fireEvent.click(screen.getByRole("button", { name: /iperf3/ }))
    const submit = screen.getByRole("button", { name: "Queue job" })
    expect(submit).toBeDisabled()
    fireEvent.click(screen.getByLabelText(/I understand this job generates bandwidth/))
    expect(submit).toBeEnabled()
    fireEvent.change(screen.getByLabelText("Bitrate (Mbps)"), { target: { value: "9999" } })
    expect(submit).toBeDisabled()
    fireEvent.change(screen.getByLabelText("Bitrate (Mbps)"), { target: { value: "30" } })
    expect(submit).toBeEnabled()
    fireEvent.click(submit)
    await waitFor(() => expect(createJob).toHaveBeenCalledTimes(1))
    expect(createJob.mock.calls[0][0]).toMatchObject({ kind: "iperf3", bitrate_mbps: 30, duration_seconds: 10, parallel_streams: 1, confirmation: "bandwidth-impact-accepted" })
  })

  it("hides the UDP protocol and reverse direction until the policy allows them", () => {
    const rendered = view(
      <DiagnosticsCreateDialog open onOpenChange={() => undefined} policy={policy({ allow_udp: true })} targets={[target()]}
        capabilities={[capability("node-1", ["iperf3"])]} nodes={nodes} onCreated={onCreated} />,
    )
    fireEvent.click(screen.getByRole("button", { name: /iperf3/ }))
    expect(screen.getByRole("button", { name: "udp" })).toBeInTheDocument()
    expect(screen.queryByRole("button", { name: "reverse" })).toBeNull()

    rendered.rerender(
      <LocaleProvider>
        <DiagnosticsCreateDialog open onOpenChange={() => undefined} policy={policy({ allow_udp: true, allow_reverse: true })} targets={[target()]}
          capabilities={[capability("node-1", ["iperf3"])]} nodes={nodes} onCreated={onCreated} />
      </LocaleProvider>,
    )
    expect(screen.getByRole("button", { name: "reverse" })).toBeInTheDocument()
  })

  it("reports a rejected submission and stays open", async () => {
    createJob.mockRejectedValueOnce(new Error("capability revoked"))
    const onOpenChange = vi.fn()
    view(
      <DiagnosticsCreateDialog open onOpenChange={onOpenChange} policy={policy()} targets={[target()]}
        capabilities={[capability("node-1", ["mtr"])]} nodes={nodes} onCreated={onCreated} />,
    )
    fireEvent.click(screen.getByRole("button", { name: "Queue job" }))
    expect(await screen.findByText("capability revoked")).toBeInTheDocument()
    expect(onOpenChange).not.toHaveBeenCalled()
    expect(onCreated).not.toHaveBeenCalled()
  })

  it("blocks submission when no approved target exists", () => {
    view(
      <DiagnosticsCreateDialog open onOpenChange={() => undefined} policy={policy()} targets={[]}
        capabilities={[capability("node-1", ["mtr"])]} nodes={nodes} onCreated={onCreated} />,
    )
    expect(screen.getByText(/No enabled target is approved/)).toBeInTheDocument()
    expect(screen.getByRole("button", { name: "Queue job" })).toBeDisabled()
  })
})
