import type { ReactNode } from "react"
import { fireEvent, render, screen } from "@testing-library/react"
import { describe, expect, it, vi } from "vitest"

import { DiagnosticsActionDialog, type DiagnosticAction } from "@/components/diagnostics/diagnostics-action-dialog"
import { LocaleProvider } from "@/context/locale"
import type { DiagnosticJob } from "@/lib/diagnostics-api"

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

function job(overrides: Partial<DiagnosticJob> = {}): DiagnosticJob {
  return {
    id: "job-1", source_node_id: "node-1", target_id: "target-1", kind: "mtr", state: "running", actor_user_id: "u1",
    actor_username: "owner", confirmation: "", source_node_name: "Tokyo", target_name: "Zurich edge", target_revision: 1,
    policy_revision: 1, protocol: "icmp", direction: "forward", ip_family: "ipv4", probes: 5, hop_limit: 10,
    duration_seconds: 0, parallel_streams: 0, bitrate_mbps: 0, policy_snapshot: "{}", target_snapshot: "{}",
    capability_digest: "d", reserved_bytes: 0, actual_bytes: 0, attempt: 1, cursor_sequence: 0, cancel_requested: false,
    error_code: "", created_at: "2026-10-15T00:00:00Z", updated_at: "2026-10-15T00:00:00Z",
    leased_at: null, started_at: null, completed_at: null, ...overrides,
  }
}

function view(element: ReactNode) {
  return render(<LocaleProvider>{element}</LocaleProvider>)
}

const titles: Record<DiagnosticAction, string> = {
  cancel: "Cancel this diagnostic?",
  retry: "Retry this diagnostic?",
  delete: "Delete this diagnostic?",
}

describe("diagnostic action dialog", () => {
  it.each(["cancel", "retry", "delete"] as DiagnosticAction[])("confirms the %s action with its own copy", (action) => {
    const onConfirm = vi.fn()
    view(<DiagnosticsActionDialog job={job()} action={action} loading={false} onOpenChange={() => undefined} onConfirm={onConfirm} />)
    expect(screen.getByRole("dialog")).toHaveTextContent(titles[action])
    expect(screen.getByText(/Tokyo → Zurich edge/)).toBeInTheDocument()
    fireEvent.click(screen.getByRole("button", { name: action === "cancel" ? "Cancel job" : action === "retry" ? "Retry" : "Delete" }))
    expect(onConfirm).toHaveBeenCalled()
  })

  it("stays hidden without a job and blocks the confirm button while working", () => {
    const rendered = view(<DiagnosticsActionDialog job={null} action="cancel" loading={false} onOpenChange={() => undefined} onConfirm={() => undefined} />)
    expect(screen.queryByRole("dialog")).toBeNull()
    rendered.rerender(
      <LocaleProvider>
        <DiagnosticsActionDialog job={job()} action="delete" loading onOpenChange={() => undefined} onConfirm={() => undefined} />
      </LocaleProvider>,
    )
    expect(screen.getByRole("button", { name: "Delete" })).toBeDisabled()
    fireEvent.click(screen.getByRole("button", { name: "Cancel" }))
  })

  it("closes when dismissed", () => {
    const onOpenChange = vi.fn()
    view(<DiagnosticsActionDialog job={job()} action="retry" loading={false} onOpenChange={onOpenChange} onConfirm={() => undefined} />)
    fireEvent.click(screen.getByRole("button", { name: "Cancel" }))
    expect(onOpenChange).toHaveBeenCalledWith(false)
  })
})
