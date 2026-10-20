import type { ReactNode } from "react"
import { fireEvent, render, screen, waitFor } from "@testing-library/react"
import { beforeEach, describe, expect, it, vi } from "vitest"

import { DiagnosticsPanel } from "@/components/diagnostics/diagnostics-panel"
import { LocaleProvider } from "@/context/locale"
import * as apiHooks from "@/hooks/use-api"
import * as diagnosticHooks from "@/hooks/use-diagnostics"
import * as diagnosticsApi from "@/lib/diagnostics-api"
import type { DiagnosticCapability, DiagnosticJob, DiagnosticPolicy, DiagnosticTarget } from "@/lib/diagnostics-api"

vi.mock("@/hooks/use-api")
vi.mock("@/hooks/use-diagnostics")
vi.mock("@/lib/diagnostics-api")
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
vi.mock("@/components/diagnostics/diagnostics-create-dialog", () => ({
  DiagnosticsCreateDialog: (props: {
    open: boolean
    nodes: { id: string; name: string }[]
    targets: DiagnosticTarget[]
    capabilities: DiagnosticCapability[]
    onCreated: (job: DiagnosticJob) => Promise<void> | void
  }) => props.open ? (
    <div data-testid="create-dialog">
      <span data-testid="create-nodes">{props.nodes.map((node) => node.name).join("|")}</span>
      <span data-testid="create-targets">{props.targets.map((target) => target.name).join("|")}</span>
      <span data-testid="create-capabilities">{props.capabilities.length}</span>
      <button onClick={() => void props.onCreated(job({ id: "created" }))}>emit-created</button>
    </div>
  ) : null,
}))
vi.mock("@/components/diagnostics/diagnostics-job-detail", () => ({
  DiagnosticsJobDetail: (props: { jobId: string | null; open: boolean }) => (
    props.open ? <div data-testid="job-detail">{props.jobId}</div> : null
  ),
}))
vi.mock("@/components/diagnostics/diagnostics-job-table", () => ({
  DiagnosticsRefreshButton: ({ onClick }: { onClick: () => void }) => <button onClick={onClick}>refresh-all</button>,
  DiagnosticsJobTable: (props: {
    jobs: DiagnosticJob[]
    loading: boolean
    interrupted?: boolean
    onSelect: (job: DiagnosticJob) => void
    onCancel: (job: DiagnosticJob) => void
    onRetry: (job: DiagnosticJob) => void
    onDelete: (job: DiagnosticJob) => void
  }) => (
    <div data-testid="job-table" data-loading={String(props.loading)} data-interrupted={String(Boolean(props.interrupted))}>
      <span data-testid="job-ids">{props.jobs.map((item) => item.id).join("|")}</span>
      <button onClick={() => props.onSelect(props.jobs[0])}>select</button>
      <button onClick={() => props.onCancel(props.jobs[0])}>cancel</button>
      <button onClick={() => props.onRetry(props.jobs[0])}>retry</button>
      <button onClick={() => props.onDelete(props.jobs[0])}>delete</button>
    </div>
  ),
}))

function policy(overrides: Partial<DiagnosticPolicy> = {}): DiagnosticPolicy {
  return {
    id: "policy-1", enabled: true, allowed_kinds: ["mtr", "iperf3"], allow_udp: false, allow_reverse: false,
    allow_private_targets: false, maximum_active_jobs: 4, maximum_active_jobs_per_node: 1,
    daily_byte_budget: 1_000_000, maximum_iperf_duration_seconds: 60, maximum_iperf_bitrate_mbps: 100,
    revision: 3, updated_by: "u1", updated_at: "2026-10-15T00:00:00Z", ...overrides,
  }
}

function target(overrides: Partial<DiagnosticTarget> = {}): DiagnosticTarget {
  return {
    id: "target-1", name: "Zurich edge", purpose: "latency", kinds: ["mtr"], address: "example.com",
    port: 80, ip_family: "ipv4", protocol: "tcp", direction: "forward", private_address: false,
    allowed_addresses: [], resolved_addresses: [], resolved_at: null, maximum_bitrate_mbps: 100,
    maximum_duration_seconds: 60, maximum_parallel_streams: 4, daily_byte_budget: 1_000_000,
    enabled: true, expires_at: null, revision: 1, created_by: "u1", updated_by: "u1",
    created_at: "2026-10-15T00:00:00Z", updated_at: "2026-10-15T00:00:00Z", ...overrides,
  }
}

function job(overrides: Partial<DiagnosticJob> = {}): DiagnosticJob {
  return {
    id: "job-1", source_node_id: "node-1", target_id: "target-1", kind: "mtr", state: "succeeded",
    actor_user_id: "u1", actor_username: "owner", confirmation: "", source_node_name: "Tokyo",
    target_name: "Zurich edge", target_revision: 1, policy_revision: 3, protocol: "tcp", direction: "forward",
    ip_family: "ipv4", probes: 5, hop_limit: 10, duration_seconds: 0, parallel_streams: 0, bitrate_mbps: 0,
    policy_snapshot: "{}", target_snapshot: "{}", capability_digest: "d", reserved_bytes: 0, actual_bytes: 0,
    attempt: 1, cursor_sequence: 3, cancel_requested: false, error_code: "", created_at: "2026-10-15T00:00:00Z",
    updated_at: "2026-10-15T00:01:00Z", leased_at: null, started_at: null, completed_at: null, ...overrides,
  }
}

function resource(data: unknown, overrides: Record<string, unknown> = {}) {
  return { data, loading: false, error: null, refresh: refreshMock(), ...overrides }
}
function refreshMock() {
  return vi.fn().mockResolvedValue(undefined)
}

function view(element: ReactNode) {
  return render(<LocaleProvider>{element}</LocaleProvider>)
}

const createJob = vi.mocked(diagnosticsApi.createDiagnosticJob)
const cancelJob = vi.mocked(diagnosticsApi.cancelDiagnosticJob)
const retryJob = vi.mocked(diagnosticsApi.retryDiagnosticJob)
const deleteJob = vi.mocked(diagnosticsApi.deleteDiagnosticJob)

describe("diagnostics panel", () => {
  beforeEach(() => {
    vi.resetAllMocks()
    vi.mocked(apiHooks.useNodes).mockReturnValue({
      data: [{ id: "node-1", name: "Tokyo" }, { id: "node-2", name: "Osaka" }],
      loading: false, error: null, refresh: refreshMock(),
    } as never)
    vi.mocked(diagnosticHooks.useDiagnosticPolicy).mockReturnValue(resource(policy()) as never)
    vi.mocked(diagnosticHooks.useDiagnosticTargets).mockReturnValue(resource([target()]) as never)
    vi.mocked(diagnosticHooks.useDiagnosticCapabilities).mockReturnValue(resource([]) as never)
    vi.mocked(diagnosticHooks.useDiagnosticJobs).mockReturnValue(resource([job()]) as never)
    cancelJob.mockResolvedValue(undefined)
    retryJob.mockResolvedValue(job())
    deleteJob.mockResolvedValue(undefined)
  })

  it("loads jobs, shows names in filters, and refreshes every source", async () => {
    view(<DiagnosticsPanel />)
    expect(screen.getByTestId("job-ids")).toHaveTextContent("job-1")
    const nodeFilter = screen.getByLabelText("Node")
    fireEvent.change(nodeFilter, { target: { value: "node-1" } })
    expect(screen.getByLabelText("Node")).toHaveAttribute("data-selected-label", "Tokyo")

    fireEvent.click(screen.getByRole("button", { name: "refresh-all" }))
    await waitFor(() => expect(vi.mocked(diagnosticHooks.useDiagnosticPolicy).mock.results[0].value.refresh).toHaveBeenCalled())
    expect(vi.mocked(diagnosticHooks.useDiagnosticJobs).mock.results[0].value.refresh).toHaveBeenCalled()
    expect(vi.mocked(diagnosticHooks.useDiagnosticTargets).mock.results[0].value.refresh).toHaveBeenCalled()
  })

  it("passes node names and capability digests into the create dialog and refreshes on creation", async () => {
    view(<DiagnosticsPanel />)
    fireEvent.click(screen.getByRole("button", { name: "New diagnostic" }))
    expect(screen.getByTestId("create-nodes")).toHaveTextContent("Tokyo|Osaka")
    expect(screen.getByTestId("create-targets")).toHaveTextContent("Zurich edge")
    fireEvent.click(screen.getByRole("button", { name: "emit-created" }))
    await waitFor(() => expect(vi.mocked(diagnosticHooks.useDiagnosticJobs).mock.results[0].value.refresh).toHaveBeenCalled())
    expect(createJob).not.toHaveBeenCalled()
  })

  it("filters by state, node and target", async () => {
    view(<DiagnosticsPanel />)
    fireEvent.change(screen.getByLabelText("State"), { target: { value: "failed" } })
    fireEvent.change(screen.getByLabelText("Node"), { target: { value: "node-2" } })
    fireEvent.change(screen.getByLabelText("Target"), { target: { value: "target-1" } })
    const last = vi.mocked(diagnosticHooks.useDiagnosticJobs).mock.calls.at(-1)?.[0]
    expect(last).toEqual({ state: "failed", nodeId: "node-2", targetId: "target-1" })
  })

  it("returns to all values when the filter is cleared", async () => {
    view(<DiagnosticsPanel />)
    fireEvent.change(screen.getByLabelText("State"), { target: { value: "failed" } })
    fireEvent.change(screen.getByLabelText("State"), { target: { value: "all" } })
    expect(vi.mocked(diagnosticHooks.useDiagnosticJobs).mock.calls.at(-1)?.[0]).toEqual({ state: "" })
  })

  it("opens job detail for the selected job", () => {
    view(<DiagnosticsPanel />)
    fireEvent.click(screen.getByRole("button", { name: "select" }))
    expect(screen.getByTestId("job-detail")).toHaveTextContent("job-1")
  })

  it("cancels, retries and deletes after confirmation", async () => {
    view(<DiagnosticsPanel />)
    fireEvent.click(screen.getByRole("button", { name: "cancel" }))
    fireEvent.click(screen.getByRole("button", { name: "Cancel job" }))
    await waitFor(() => expect(cancelJob).toHaveBeenCalledWith("job-1"))

    fireEvent.click(screen.getByRole("button", { name: "retry" }))
    fireEvent.click(screen.getByRole("button", { name: "Retry" }))
    await waitFor(() => expect(retryJob).toHaveBeenCalledWith("job-1"))

    fireEvent.click(screen.getByRole("button", { name: "delete" }))
    fireEvent.click(screen.getByRole("button", { name: "Delete" }))
    await waitFor(() => expect(deleteJob).toHaveBeenCalledWith("job-1"))
  })

  it("shows a disabled notice when the server policy is off", () => {
    vi.mocked(diagnosticHooks.useDiagnosticPolicy).mockReturnValue(resource(policy({ enabled: false })) as never)
    view(<DiagnosticsPanel />)
    expect(screen.getByText(/Diagnostics are disabled/)).toBeInTheDocument()
    expect(screen.queryByTestId("job-table")).not.toBeInTheDocument()
  })

  it("reports load errors and maps failure status codes", async () => {
    vi.mocked(diagnosticHooks.useDiagnosticJobs).mockReturnValue(resource([job()], { error: "jobs failed" }) as never)
    view(<DiagnosticsPanel />)
    expect(screen.getByText("jobs failed")).toBeInTheDocument()

    cancelJob.mockRejectedValueOnce({ response: { status: 409 } })
    fireEvent.click(screen.getByRole("button", { name: "cancel" }))
    fireEvent.click(screen.getByRole("button", { name: "Cancel job" }))
    expect(await screen.findByText(/changed state/)).toBeInTheDocument()

    cancelJob.mockRejectedValueOnce({ response: { status: 403 } })
    fireEvent.click(screen.getByRole("button", { name: "cancel" }))
    fireEvent.click(screen.getByRole("button", { name: "Cancel job" }))
    expect(await screen.findByText(/recent re-authentication/)).toBeInTheDocument()
  })

  it("keeps rendering with the previous data while a refresh is in flight", () => {
    vi.mocked(diagnosticHooks.useDiagnosticJobs).mockReturnValue(resource([job()], { loading: true, error: "stale" }) as never)
    view(<DiagnosticsPanel />)
    expect(screen.getByTestId("job-table")).toHaveAttribute("data-interrupted", "true")
    expect(screen.getByTestId("job-table")).toHaveAttribute("data-loading", "false")
  })
})
