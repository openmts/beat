import { fireEvent, render, screen, waitFor } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { beforeEach, describe, expect, it, vi } from "vitest"

import { LocaleProvider } from "@/context/locale"
import * as api from "@/lib/application-retention-api"
import ApplicationRetentionPanel from "@/pages/admin/application-retention-panel"
import type { ApplicationRetentionOverview } from "@/types/application-retention"

vi.mock("@/lib/application-retention-api", () => ({
  getApplicationRetention: vi.fn(),
  updateApplicationRetention: vi.fn(),
  runApplicationRetention: vi.fn(),
}))

const settings = {
  alert_event_days: 180,
  audit_event_days: 90,
  cleanup_hour_utc: 3,
  auto_cleanup_enabled: true,
  updated_at: "2026-10-15T03:00:00Z",
}

function overview(overrides: Partial<ApplicationRetentionOverview["status"]> = {}): ApplicationRetentionOverview {
  return {
    settings,
    status: {
      running: false,
      last_started_at: null,
      last_completed_at: null,
      last_status: "never",
      last_error_code: "",
      last_trigger: "",
      last_duration_ms: 0,
      deleted_alert_events: 0,
      deleted_audit_events: 0,
      deleted_sessions: 0,
      ...overrides,
    },
    session_retention_days: 30,
  }
}

const renderPanel = () => render(<LocaleProvider><ApplicationRetentionPanel /></LocaleProvider>)

describe("application history retention panel", () => {
  beforeEach(() => {
    vi.resetAllMocks()
    vi.mocked(api.getApplicationRetention).mockResolvedValue(overview())
    vi.mocked(api.updateApplicationRetention).mockImplementation(async (next) => next)
    vi.mocked(api.runApplicationRetention).mockResolvedValue(undefined)
  })

  it("renders editable policy, fixed session window, and saved state", async () => {
    renderPanel()
    expect(await screen.findByLabelText("Alert event retention")).toHaveValue(180)
    expect(screen.getByLabelText("Audit log retention")).toHaveValue(90)
    expect(screen.getByText("30 days (fixed)")).toBeInTheDocument()
    expect(screen.getByText("Never run")).toBeInTheDocument()

    await userEvent.clear(screen.getByLabelText("Alert event retention"))
    fireEvent.change(screen.getByLabelText("Alert event retention"), { target: { value: "30" } })
    fireEvent.change(screen.getByLabelText("Audit log retention"), { target: { value: "45" } })
    await userEvent.click(screen.getByRole("button", { name: "Save" }))

    await waitFor(() => expect(api.updateApplicationRetention).toHaveBeenCalledWith(
      expect.objectContaining({ alert_event_days: 30, audit_event_days: 45 }),
    ))
    expect(await screen.findByText("Application history retention saved")).toBeInTheDocument()
  })

  it("runs a manual cleanup only after confirmation and shows deleted counts", async () => {
    vi.mocked(api.getApplicationRetention)
      .mockResolvedValueOnce(overview())
      .mockResolvedValue(overview({
        last_status: "success", last_trigger: "manual", last_started_at: "2026-10-15T03:00:00Z",
        last_completed_at: "2026-10-15T03:00:05Z", deleted_alert_events: 12,
        deleted_audit_events: 3, deleted_sessions: 4,
      }))
    const confirm = vi.spyOn(window, "confirm").mockReturnValue(false)
    renderPanel()
    await screen.findByLabelText("Alert event retention")

    await userEvent.click(screen.getByRole("button", { name: "Run cleanup now" }))
    expect(api.runApplicationRetention).not.toHaveBeenCalled()

    confirm.mockReturnValue(true)
    await userEvent.click(screen.getByRole("button", { name: "Run cleanup now" }))
    await waitFor(() => expect(api.runApplicationRetention).toHaveBeenCalledOnce())
    expect(await screen.findByText("12 alerts · 3 audits · 4 sessions")).toBeInTheDocument()
    expect(screen.getByText(/Succeeded/)).toBeInTheDocument()
    confirm.mockRestore()
  })

  it("maps fixed failure codes without leaking storage details", async () => {
    vi.mocked(api.getApplicationRetention).mockResolvedValue(overview({
      last_status: "failed", last_trigger: "automatic", last_error_code: "timeout",
    }))
    renderPanel()
    expect(await screen.findByText("Cleanup timed out")).toBeInTheDocument()
  })

  it("surfaces load failures as an alert", async () => {
    vi.mocked(api.getApplicationRetention).mockRejectedValue(new Error("storage offline"))
    renderPanel()
    expect(await screen.findByRole("alert")).toHaveTextContent("storage offline")
  })
})
