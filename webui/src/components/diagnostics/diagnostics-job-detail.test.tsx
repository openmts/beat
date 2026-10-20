import type { ReactNode } from "react"
import { fireEvent, render, screen } from "@testing-library/react"
import { beforeEach, describe, expect, it, vi } from "vitest"

import { DiagnosticsJobDetail } from "@/components/diagnostics/diagnostics-job-detail"
import { LocaleProvider } from "@/context/locale"
import * as diagnosticHooks from "@/hooks/use-diagnostics"
import type { DiagnosticJob, DiagnosticSeries } from "@/lib/diagnostics-api"

vi.mock("@/hooks/use-diagnostics")
vi.mock("@/components/ui/sheet", async () => {
  const mocks = await vi.importActual<typeof import("@/test/admin-ui-mocks")>("@/test/admin-ui-mocks")
  return {
    Sheet: mocks.MockDialog,
    SheetContent: mocks.PassThrough,
    SheetHeader: mocks.PassThrough,
    SheetTitle: mocks.PassThrough,
    SheetDescription: mocks.PassThrough,
  }
})
vi.mock("recharts", () => ({
  ResponsiveContainer: ({ children }: { children: ReactNode }) => <div>{children}</div>,
  LineChart: ({ children }: { children: ReactNode }) => <div>{children}</div>,
  Line: ({ dataKey }: { dataKey: string }) => <span data-testid="chart-line">{dataKey}</span>,
  CartesianGrid: () => <span />,
  XAxis: () => <span />,
  YAxis: () => <span />,
  Tooltip: () => <span />,
}))

function job(overrides: Partial<DiagnosticJob> = {}): DiagnosticJob {
  return {
    id: "job-1", source_node_id: "node-1", target_id: "target-1", kind: "mtr", state: "succeeded", actor_user_id: "u1",
    actor_username: "owner", confirmation: "", source_node_name: "Tokyo", target_name: "Zurich edge", target_revision: 1,
    policy_revision: 1, protocol: "icmp", direction: "forward", ip_family: "ipv4", probes: 5, hop_limit: 10,
    duration_seconds: 0, parallel_streams: 0, bitrate_mbps: 0, policy_snapshot: "{}", target_snapshot: "{}",
    capability_digest: "d", reserved_bytes: 1024, actual_bytes: 4096, attempt: 2, cursor_sequence: 3,
    cancel_requested: false, error_code: "", created_at: "2026-10-15T00:00:00Z", updated_at: "2026-10-15T00:01:00Z",
    leased_at: "2026-10-15T00:00:05Z", started_at: "2026-10-15T00:00:06Z", completed_at: "2026-10-15T00:02:00Z", ...overrides,
  }
}

function series(metric: string, points: number[]): DiagnosticSeries {
  return {
    metric, unit: "", points: points.map((value, index) => ({ Timestamp: `2026-10-15T00:0${index}:00Z`, Value: value })),
  }
}

function view(element: ReactNode) {
  return render(<LocaleProvider>{element}</LocaleProvider>)
}

function detail(data: { job: DiagnosticJob; hops: unknown } | null, overrides: Record<string, unknown> = {}) {
  return { data, loading: false, error: null, refresh: vi.fn().mockResolvedValue(undefined), ...overrides }
}

const noSeries = { series: [], loading: false, error: null }

describe("diagnostics job detail", () => {
  beforeEach(() => {
    vi.resetAllMocks()
    vi.mocked(diagnosticHooks.useDiagnosticSerieses).mockReturnValue(noSeries as never)
  })

  it("masks MTR hop addresses until the operator reveals them", () => {
    vi.mocked(diagnosticHooks.useDiagnosticJobDetail).mockReturnValue(detail({
      job: job(),
      hops: [{ index: 1, address: "203.0.113.10", reached: true }, { index: 2, address: "2001:db8::1", reached: false }],
    }) as never)
    view(<DiagnosticsJobDetail jobId="job-1" open onOpenChange={() => undefined} />)
    expect(screen.getByText("203.0.113.***")).toBeInTheDocument()
    expect(screen.getByText("2001:db8:****")).toBeInTheDocument()
    expect(screen.queryByText("203.0.113.10")).toBeNull()
    expect(screen.getByText("No answer")).toBeInTheDocument()

    fireEvent.click(screen.getByRole("button", { name: "Show hop addresses" }))
    expect(screen.getByText("203.0.113.10")).toBeInTheDocument()
    expect(screen.getByText("2001:db8::1")).toBeInTheDocument()

    fireEvent.click(screen.getByRole("button", { name: "Mask hop addresses" }))
    expect(screen.queryByText("203.0.113.10")).toBeNull()
  })

  it("shows attempt, measured bytes and the derived timeline", () => {
    vi.mocked(diagnosticHooks.useDiagnosticJobDetail).mockReturnValue(detail({ job: job(), hops: [] }) as never)
    view(<DiagnosticsJobDetail jobId="job-1" open onOpenChange={() => undefined} />)
    expect(screen.getByText("Attempt 2")).toBeInTheDocument()
    expect(screen.getByText("4 KiB")).toBeInTheDocument()
    expect(screen.getByText("1 KiB")).toBeInTheDocument()
    expect(screen.getByText("Values persisted")).toBeInTheDocument()
    expect(screen.getByText("Completed")).toBeInTheDocument()
    expect(screen.getByText(/Requested by: owner/)).toBeInTheDocument()
  })

  it("renders the final iperf3 summary and hides the hop table", () => {
    vi.mocked(diagnosticHooks.useDiagnosticJobDetail).mockReturnValue(detail({ job: job({ kind: "iperf3", protocol: "tcp" }), hops: null }) as never)
    vi.mocked(diagnosticHooks.useDiagnosticSerieses).mockReturnValue({
      series: [series("diagnostic_iperf_bytes_per_second", [1024, 2048]), series("diagnostic_iperf_bytes_per_second", [512]), series("diagnostic_iperf_bytes", [1]), null],
      loading: false, error: null,
    } as never)
    view(<DiagnosticsJobDetail jobId="job-1" open onOpenChange={() => undefined} />)
    expect(screen.queryByText("MTR hops")).toBeNull()
    expect(screen.getByText("Final summary")).toBeInTheDocument()
    expect(screen.getByText("2 KiB/s")).toBeInTheDocument()
    expect(screen.getByText("512 B/s")).toBeInTheDocument()
    expect(screen.getByText("—")).toBeInTheDocument()
  })

  it("shows empty and error states", () => {
    vi.mocked(diagnosticHooks.useDiagnosticJobDetail).mockReturnValue(detail(null) as never)
    const rendered = view(<DiagnosticsJobDetail jobId="job-1" open onOpenChange={() => undefined} />)
    expect(screen.getByText(/No diagnostic job matches/)).toBeInTheDocument()

    vi.mocked(diagnosticHooks.useDiagnosticJobDetail).mockReturnValue(detail({ job: job(), hops: [] }, { error: "detail failed" }) as never)
    vi.mocked(diagnosticHooks.useDiagnosticSerieses).mockReturnValue({ series: [], loading: false, error: "series failed" } as never)
    rendered.rerender(<LocaleProvider><DiagnosticsJobDetail jobId="job-1" open onOpenChange={() => undefined} /></LocaleProvider>)
    expect(screen.getByText("detail failed")).toBeInTheDocument()
    expect(screen.getByText("series failed")).toBeInTheDocument()
    expect(screen.getAllByText(/No measured value was stored/).length).toBeGreaterThan(0)
  })

  it("stays closed and does not read a job when the sheet is closed", () => {
    vi.mocked(diagnosticHooks.useDiagnosticJobDetail).mockReturnValue(detail(null) as never)
    view(<DiagnosticsJobDetail jobId={null} open={false} onOpenChange={() => undefined} />)
    expect(diagnosticHooks.useDiagnosticJobDetail).toHaveBeenCalledWith(null, expect.any(Number))
    expect(screen.queryByRole("dialog")).toBeNull()
  })
})
