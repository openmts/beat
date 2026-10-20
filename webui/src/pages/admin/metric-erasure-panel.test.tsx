import { fireEvent, render, screen, waitFor, within } from "@testing-library/react"
import { beforeEach, describe, expect, it, vi } from "vitest"

import { LocaleProvider } from "@/context/locale"
import * as hooks from "@/hooks/use-api"
import * as networkHooks from "@/hooks/use-network"
import * as backupApi from "@/lib/backup-api"
import { APIError } from "@/lib/api-error"
import * as erasureApi from "@/lib/metric-erasure-api"
import * as securityApi from "@/lib/security-api"
import { MetricErasurePanel } from "@/pages/admin/metric-erasure-panel"
import type { BackupRecord } from "@/types/backup"

vi.mock("@/hooks/use-api")
vi.mock("@/hooks/use-network")
vi.mock("@/lib/backup-api", () => ({ listBackups: vi.fn(), listBackupQuarantine: vi.fn(), createBackup: vi.fn() }))
const auth = vi.hoisted(() => ({ principal: { user: { role: "owner", totp_enabled_at: null as string | null } } }))
vi.mock("@/context/auth", () => ({ useAuth: () => auth }))
vi.mock("@/lib/security-api")
vi.mock("@/lib/metric-erasure-api", async () => {
  const actual = await vi.importActual<typeof import("@/lib/metric-erasure-api")>("@/lib/metric-erasure-api")
  return {
    ...actual,
    listMetricErasureJobs: vi.fn(),
    createMetricErasureJob: vi.fn(),
    retryMetricErasureJob: vi.fn(),
    cancelMetricErasureJob: vi.fn(),
  }
})
vi.mock("@/components/ui/dialog", async () => {
  const mocks = await vi.importActual<typeof import("@/test/admin-ui-mocks")>("@/test/admin-ui-mocks")
  return {
    Dialog: mocks.MockDialog, DialogContent: mocks.PassThrough, DialogHeader: mocks.PassThrough,
    DialogTitle: mocks.PassThrough, DialogDescription: mocks.PassThrough, DialogFooter: mocks.PassThrough,
  }
})
vi.mock("@/components/ui/select", async () => {
  const mocks = await vi.importActual<typeof import("@/test/admin-ui-mocks")>("@/test/admin-ui-mocks")
  return {
    Select: mocks.MockSelect, SelectContent: mocks.PassThrough, SelectGroup: mocks.PassThrough,
    SelectItem: mocks.MockSelectItem, SelectTrigger: () => null, SelectValue: () => null,
  }
})
vi.mock("@/components/ui/alert-dialog", async () => {
  const mocks = await vi.importActual<typeof import("@/test/admin-ui-mocks")>("@/test/admin-ui-mocks")
  return {
    AlertDialog: mocks.MockDialog, AlertDialogContent: mocks.PassThrough,
    AlertDialogHeader: mocks.PassThrough, AlertDialogTitle: mocks.PassThrough,
    AlertDialogDescription: mocks.PassThrough, AlertDialogFooter: mocks.PassThrough,
    AlertDialogCancel: ({ children, ...rest }: { children: React.ReactNode }) => <button {...rest}>{children}</button>,
    AlertDialogAction: ({ children, ...rest }: { children: React.ReactNode }) => <button {...rest}>{children}</button>,
  }
})

const state = (data: unknown) => ({ data, loading: false, error: null, refresh: vi.fn() })
const node = { id: "n1", name: "server-one" }
const task = { task: { id: "t1", name: "Probe" }, nodes: [] }

function readyBackup(id: string, createdAt: string): BackupRecord {
  return {
    id, filename: `${id}.zip`, source: "generated", state: "ready", created_at: createdAt,
    completed_at: createdAt, snapshot_cutoff: createdAt, size_bytes: 1, sqlite_bytes: 1,
    metrics_bytes: 1, metric_rows: 1, error_message: "",
    updated_at: createdAt, reconciled_at: null,
  }
}

function view() {
  return render(<LocaleProvider><MetricErasurePanel /></LocaleProvider>)
}

function queuedJob(scope: erasureApi.MetricErasureScope) {
  return {
    id: "job-1", scope, state: "queued" as const, target_type: "node" as const,
    target_id: "n1", target_name: "server-one", attempts: 1,
    requested_at: "2026-10-15T08:00:00Z",
  }
}

describe("metric erasure panel", () => {
  beforeEach(() => {
    vi.resetAllMocks()
    vi.mocked(hooks.useNodes).mockReturnValue(state([node]) as never)
    vi.mocked(networkHooks.useAdminNetworkTasks).mockReturnValue(state([task]) as never)
    vi.mocked(erasureApi.listMetricErasureJobs).mockResolvedValue([])

    vi.mocked(backupApi.listBackups).mockResolvedValue({ items: [readyBackup("nightly", new Date().toISOString())] })

  })

  it("lists jobs and cancels a queued one", async () => {
    vi.mocked(erasureApi.listMetricErasureJobs).mockResolvedValue([queuedJob("node_history")])

    vi.mocked(erasureApi.cancelMetricErasureJob).mockResolvedValue(queuedJob("node_history"))
    view()
    expect(await screen.findByText("One node's history")).toBeInTheDocument()
    expect(screen.getByText("Queued")).toBeInTheDocument()
    fireEvent.click(screen.getByRole("button", { name: "Cancel" }))
    const dialog = await screen.findByRole("dialog")
    expect(dialog).toHaveTextContent("Cancel this erasure?")
    fireEvent.click(within(dialog).getAllByRole("button", { name: "Cancel" }).at(-1) as HTMLElement)
    await waitFor(() => expect(erasureApi.cancelMetricErasureJob).toHaveBeenCalledWith("job-1"))
  })

  it("requires the exact entity name before queuing a node erasure", async () => {
    vi.mocked(erasureApi.createMetricErasureJob).mockResolvedValue({ job: queuedJob("node_history"), replayed: false })
    view()
    const selects = screen.getAllByTestId("select")
    fireEvent.change(selects[0] as HTMLElement, { target: { value: "node_history" } })
    fireEvent.change(selects[1] as HTMLElement, { target: { value: "n1" } })
    fireEvent.click(screen.getByRole("button", { name: "Erase history" }))

    const dialog = await screen.findByRole("dialog")
    const confirm = within(dialog).getByRole("button", { name: "Start erasure" })
    expect(confirm).toBeDisabled()
    fireEvent.change(within(dialog).getByLabelText("Entity display name"), { target: { value: "server" } })
    expect(confirm).toBeDisabled()
    fireEvent.change(within(dialog).getByLabelText("Entity display name"), { target: { value: "server-one" } })
    await waitFor(() => expect(confirm).not.toBeDisabled())
    fireEvent.click(confirm)
    await waitFor(() => expect(erasureApi.createMetricErasureJob).toHaveBeenCalledWith({
      scope: "node_history", target_id: "n1", request_id: expect.any(String), confirmation: "server-one",
    }))
  })

  it("requires the fixed phrase and a backup acknowledgement for fleet erasure", async () => {
    vi.mocked(backupApi.listBackups).mockResolvedValue({ items: [] })

    vi.mocked(erasureApi.createMetricErasureJob).mockResolvedValue({ job: queuedJob("fleet_telemetry"), replayed: true })
    view()
    fireEvent.change(screen.getAllByTestId("select")[0] as HTMLElement, { target: { value: "fleet_telemetry" } })
    fireEvent.click(screen.getByRole("button", { name: "Erase history" }))

    const dialog = await screen.findByRole("dialog")
    expect(await within(dialog).findByText("No ready backup was created in the last 24 hours.")).toBeInTheDocument()
    const confirm = within(dialog).getByRole("button", { name: "Start erasure" })
    fireEvent.change(within(dialog).getByLabelText("Confirmation phrase"), { target: { value: "ERASE FLEET TELEMETRY" } })
    expect(confirm).toBeDisabled()
    fireEvent.click(within(dialog).getByRole("checkbox"))
    await waitFor(() => expect(confirm).not.toBeDisabled())
    fireEvent.click(confirm)
    await waitFor(() => expect(erasureApi.createMetricErasureJob).toHaveBeenCalledWith({
      scope: "fleet_telemetry", target_id: undefined, request_id: expect.any(String),
      confirmation: "ERASE FLEET TELEMETRY",
    }))
    expect(await screen.findByText("This request was already accepted; the existing job is shown.")).toBeInTheDocument()
  })

  it("offers a backup action and reports conflicts without exposing internals", async () => {
    vi.mocked(backupApi.listBackups).mockRejectedValue(new Error("unavailable"))
    vi.mocked(backupApi.createBackup).mockResolvedValue(readyBackup("manual", new Date().toISOString()))
    vi.mocked(erasureApi.createMetricErasureJob).mockRejectedValue(new APIError("conflict", 409, null))
    view()
    fireEvent.change(screen.getAllByTestId("select")[0] as HTMLElement, { target: { value: "all_metric_history" } })
    fireEvent.click(screen.getByRole("button", { name: "Erase history" }))

    const dialog = await screen.findByRole("dialog")
    expect(await within(dialog).findByText("The backup list could not be read, so a recent recovery point cannot be confirmed."))
      .toBeInTheDocument()
    fireEvent.click(within(dialog).getByRole("button", { name: "Create backup" }))
    expect(await within(dialog).findByText(/A ready backup from the last 24 hours is available/)).toBeInTheDocument()
    fireEvent.change(within(dialog).getByLabelText("Confirmation phrase"), { target: { value: "ERASE ALL METRIC HISTORY" } })
    fireEvent.click(within(dialog).getByRole("button", { name: "Start erasure" }))
    expect(await screen.findByText("Another erasure already covers this scope or entity.")).toBeInTheDocument()
  })

  it("retries a failed job and renders the sanitized error code", async () => {
    vi.mocked(erasureApi.listMetricErasureJobs).mockResolvedValue([
      { ...queuedJob("network_task_history"), state: "failed", error_code: "mts_delete_failed", progress_cursor: "network_probe" },
    ])
    vi.mocked(erasureApi.retryMetricErasureJob).mockResolvedValue(queuedJob("network_task_history"))
    view()
    expect(await screen.findByText("Error code: mts_delete_failed")).toBeInTheDocument()
    fireEvent.click(screen.getByRole("button", { name: "Retry" }))
    const dialog = await screen.findByRole("dialog")
    fireEvent.click(within(dialog).getByRole("button", { name: "Retry" }))
    await waitFor(() => expect(erasureApi.retryMetricErasureJob).toHaveBeenCalledWith("job-1"))
  })

  it("asks for a fresh identity proof when the erasure list is blocked by a 428", async () => {
    vi.mocked(erasureApi.listMetricErasureJobs)
      .mockRejectedValueOnce(new APIError("recent authentication is required", 428, null))
      .mockResolvedValue([queuedJob("node_history")])
    vi.mocked(securityApi.reauthenticateAdmin).mockResolvedValue({ reauthenticated_until: "2099-01-01T00:00:00Z" } as never)
    view()
    // 428 不再停在英文后端错误上，而是直接弹出确认身份对话框。
    const dialog = await screen.findByRole("dialog")
    expect(dialog).toHaveTextContent("Confirm your identity")
    expect(screen.queryByText("recent authentication is required")).not.toBeInTheDocument()
    fireEvent.change(within(dialog).getByLabelText("Password"), { target: { value: "test password" } })
    fireEvent.click(within(dialog).getByRole("button", { name: "Confirm and continue" }))
    await waitFor(() => expect(securityApi.reauthenticateAdmin).toHaveBeenCalledWith("test password", ""))
    // 确认身份后原样重放被拦下的列表读取。
    expect(await screen.findByText("Queued")).toBeInTheDocument()
    expect(erasureApi.listMetricErasureJobs).toHaveBeenCalledTimes(2)
  })
})
