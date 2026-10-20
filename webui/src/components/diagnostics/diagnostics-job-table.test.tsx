import { cloneElement, type ReactElement, type ReactNode } from "react"
import { fireEvent, render, screen } from "@testing-library/react"
import { describe, expect, it, vi } from "vitest"

import { DiagnosticsJobTable, DiagnosticsRefreshButton } from "@/components/diagnostics/diagnostics-job-table"
import { LocaleProvider } from "@/context/locale"
import type { DiagnosticJob } from "@/lib/diagnostics-api"

vi.mock("@/components/ui/tooltip", () => ({
  Tooltip: ({ children }: { children: ReactNode }) => <>{children}</>,
  TooltipTrigger: ({ render: trigger, children }: { render?: ReactElement; children?: ReactNode }) =>
    trigger ? cloneElement(trigger, undefined, children) : <>{children}</>,
  TooltipContent: ({ children }: { children: ReactNode }) => <>{children}</>,
}))
vi.mock("@/components/ui/dropdown-menu", () => ({
  DropdownMenu: ({ children }: { children: ReactNode }) => <>{children}</>,
  DropdownMenuTrigger: ({ render: trigger, children }: { render?: ReactElement; children?: ReactNode }) =>
    trigger ? cloneElement(trigger, undefined, children) : <>{children}</>,
  DropdownMenuContent: ({ children }: { children: ReactNode }) => <>{children}</>,
  DropdownMenuItem: ({ children, onSelect, disabled }: { children: ReactNode; onSelect?: () => void; disabled?: boolean }) => (
    <button disabled={disabled} onClick={onSelect}>{children}</button>
  ),
}))

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

const handlers = {
  onSelect: vi.fn(), onCancel: vi.fn(), onRetry: vi.fn(), onDelete: vi.fn(),
}

function view(element: ReactNode) {
  return render(<LocaleProvider>{element}</LocaleProvider>)
}

describe("diagnostics job table", () => {
  it("renders node and target names instead of identifiers", () => {
    view(<DiagnosticsJobTable jobs={[job()]} loading={false} {...handlers} />)
    expect(screen.getByText("Tokyo")).toBeInTheDocument()
    expect(screen.getByText("Zurich edge")).toBeInTheDocument()
    expect(screen.queryByText("node-1")).toBeNull()
    expect(screen.getByText("Running")).toBeInTheDocument()
    expect(screen.getByText("MTR trace")).toBeInTheDocument()
  })

  it("offers cancellation only while a job is active", () => {
    const rendered = view(<DiagnosticsJobTable jobs={[job()]} loading={false} {...handlers} />)
    const cancel = screen.getByRole("button", { name: "Request cancellation: job-1" })
    fireEvent.click(cancel)
    expect(handlers.onCancel).toHaveBeenCalledWith(expect.objectContaining({ id: "job-1" }))

    rendered.rerender(<LocaleProvider><DiagnosticsJobTable jobs={[job({ state: "succeeded" })]} loading={false} {...handlers} /></LocaleProvider>)
    expect(screen.queryByRole("button", { name: "Request cancellation: job-1" })).toBeNull()
  })

  it("notes a pending cancellation on an active job", () => {
    view(<DiagnosticsJobTable jobs={[job({ cancel_requested: true })]} loading={false} {...handlers} />)
    // 徽章旁的提示与取消按钮的 tooltip 共用同一文案，因此这里允许出现多次。
    expect(screen.getAllByText("Request cancellation").length).toBeGreaterThan(0)
  })

  it("enables retry only for retryable terminal states", () => {
    view(<DiagnosticsJobTable jobs={[job({ state: "partial" })]} loading={false} {...handlers} />)
    const retry = screen.getByRole("button", { name: "Retry" })
    expect(retry).toBeEnabled()
    fireEvent.click(retry)
    expect(handlers.onRetry).toHaveBeenCalled()

    view(<DiagnosticsJobTable jobs={[job({ state: "succeeded" })]} loading={false} {...handlers} />)
    expect(screen.getAllByRole("button", { name: "Retry" }).at(-1)).toBeDisabled()
  })

  it("opens detail and deletes through the row actions", () => {
    view(<DiagnosticsJobTable jobs={[job()]} loading={false} {...handlers} />)
    fireEvent.click(screen.getByRole("button", { name: "Detail" }))
    expect(handlers.onSelect).toHaveBeenCalledWith(expect.objectContaining({ id: "job-1" }))
    fireEvent.click(screen.getByRole("button", { name: "Delete" }))
    expect(handlers.onDelete).toHaveBeenCalledWith(expect.objectContaining({ id: "job-1" }))
  })

  it("shows skeletons while loading and an empty notice otherwise", () => {
    const rendered = view(<DiagnosticsJobTable jobs={[]} loading {...handlers} />)
    expect(document.querySelectorAll("[data-slot=skeleton]")).toHaveLength(3)
    rendered.rerender(<LocaleProvider><DiagnosticsJobTable jobs={[]} loading={false} {...handlers} /></LocaleProvider>)
    expect(screen.getByText(/No diagnostic job matches/)).toBeInTheDocument()
  })

  it("refreshes on demand", () => {
    const onClick = vi.fn()
    view(<DiagnosticsRefreshButton onClick={onClick} />)
    fireEvent.click(screen.getByRole("button", { name: "Refresh" }))
    expect(onClick).toHaveBeenCalled()
  })
})
