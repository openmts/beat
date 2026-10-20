import { fireEvent, render, screen, waitFor } from "@testing-library/react"
import { beforeEach, describe, expect, it, vi } from "vitest"

import { LocaleProvider } from "@/context/locale"
import * as hooks from "@/hooks/use-api"
import * as networkHooks from "@/hooks/use-network"
import * as api from "@/lib/api"
import * as applicationRetentionApi from "@/lib/application-retention-api"
import * as backupApi from "@/lib/backup-api"
import * as erasureApi from "@/lib/metric-erasure-api"
import { DataStorageSection } from "@/pages/admin/data-storage-section"

vi.mock("@/lib/api")
const auth = vi.hoisted(() => ({ principal: { user: { role: "owner", totp_enabled_at: null as string | null } } }))
vi.mock("@/context/auth", () => ({ useAuth: () => auth }))
vi.mock("@/lib/application-retention-api", () => ({
  getApplicationRetention: vi.fn(),
  updateApplicationRetention: vi.fn(),
  runApplicationRetention: vi.fn(),
}))
vi.mock("@/hooks/use-api")
vi.mock("@/hooks/use-network")
vi.mock("@/lib/backup-api", () => ({ listBackups: vi.fn(), listBackupQuarantine: vi.fn(), createBackup: vi.fn() }))
vi.mock("@/lib/metric-erasure-api", async () => {
  const actual = await vi.importActual<typeof import("@/lib/metric-erasure-api")>("@/lib/metric-erasure-api")
  return { ...actual, listMetricErasureJobs: vi.fn(), createMetricErasureJob: vi.fn(), retryMetricErasureJob: vi.fn(), cancelMetricErasureJob: vi.fn() }
})

const state = (data: unknown) => ({ data, loading: false, error: null, refresh: vi.fn() })

describe("data and storage section", () => {
  beforeEach(() => {
    vi.resetAllMocks()
    vi.mocked(api.getMaintenanceOverview).mockResolvedValue({
      settings: { retention_days: 30, auto_cleanup_enabled: true, cleanup_hour_utc: 3, updated_at: "" },
      status: {
        running: false, last_started_at: null, last_completed_at: null, last_status: "never",
        last_error: "", last_cutoff_at: null, last_duration_ms: 0, last_trigger: "", sqlite_integrity: "",
      },
      storage: { mts_bytes: 1024, sqlite_bytes: 512, total_bytes: 1536, mts_healthy: true, mts_health_reasons: [] },
    })
    vi.mocked(hooks.useNodes).mockReturnValue(state([]) as never)
    vi.mocked(networkHooks.useAdminNetworkTasks).mockReturnValue(state([]) as never)
    vi.mocked(backupApi.listBackups).mockResolvedValue({ items: [] })

    vi.mocked(applicationRetentionApi.getApplicationRetention).mockResolvedValue({
      settings: { alert_event_days: 180, audit_event_days: 180, cleanup_hour_utc: 3, auto_cleanup_enabled: true, updated_at: "" },
      status: {
        running: false, last_started_at: null, last_completed_at: null, last_status: "never",
        last_error_code: "", last_trigger: "", last_duration_ms: 0, deleted_alert_events: 0,
        deleted_audit_events: 0, deleted_sessions: 0,
      },
      session_retention_days: 30,
    })
    vi.mocked(erasureApi.listMetricErasureJobs).mockResolvedValue([])

  })

  it("keeps retention default and reveals erasure on demand", async () => {
    render(<LocaleProvider><DataStorageSection /></LocaleProvider>)
    expect(screen.getByText("Data and storage")).toBeInTheDocument()
    expect(await screen.findByText("Metric retention")).toBeInTheDocument()
    expect(screen.queryByText("Erase metric history")).not.toBeInTheDocument()

    fireEvent.click(screen.getByRole("tab", { name: "Erase history" }))
    await waitFor(() => expect(screen.getByText("Erase metric history")).toBeInTheDocument())
    expect(screen.queryByText("Metric retention")).not.toBeInTheDocument()
  })

  it("separates application history from MTS retention", async () => {
    render(<LocaleProvider><DataStorageSection /></LocaleProvider>)
    await screen.findByText("Metric retention")

    fireEvent.click(screen.getByRole("tab", { name: "Application history" }))
    expect(await screen.findByLabelText("Alert event retention")).toBeInTheDocument()
    expect(screen.getByText("30 days (fixed)")).toBeInTheDocument()
    expect(screen.queryByText("Metric retention")).not.toBeInTheDocument()
  })
})
