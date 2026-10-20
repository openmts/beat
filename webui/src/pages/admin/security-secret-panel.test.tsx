import { act, fireEvent, render, screen } from "@testing-library/react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { LocaleProvider } from "@/context/locale"
import { SecuritySecretPanel } from "@/pages/admin/security-secret-panel"
import * as secrets from "@/lib/application-secrets-api"
import * as security from "@/lib/security-api"

const auth = vi.hoisted(() => ({ principal: { user: { role: "owner", totp_enabled_at: null as string | null } } }))
vi.mock("@/context/auth", () => ({ useAuth: () => auth }))
vi.mock("@/lib/application-secrets-api")
vi.mock("@/lib/security-api")

const ready = { healthy: true, root_available: true, registry_valid: true, backfill_complete: true,
  rotation: "idle" as const, generation: 1, data_keys: 1 }

function view() { return render(<LocaleProvider><SecuritySecretPanel /></LocaleProvider>) }
function identify() {
  fireEvent.change(screen.getByLabelText("Current password"), { target: { value: "owner password" } })
  fireEvent.click(screen.getByRole("button", { name: "Confirm identity and rotate" }))
}

describe("application secret controls", () => {
  beforeEach(() => {
    vi.resetAllMocks()
    localStorage.setItem("locale", "en")
    auth.principal.user.role = "owner"
    auth.principal.user.totp_enabled_at = null
    vi.mocked(secrets.getApplicationSecretStatus).mockResolvedValue(ready)
    vi.mocked(secrets.rotateApplicationDataKey).mockResolvedValue({ ...ready, rotation: "running", generation: 2, data_keys: 2 })
  })
  afterEach(() => vi.useRealTimers())

  it("shows state without exposing controls or fetching for administrators", async () => {
    const rendered = view()
    expect(await screen.findByText("Ready")).toBeInTheDocument()
    expect(screen.getByText("Keys retained")).toBeInTheDocument()
    rendered.unmount()
    vi.mocked(secrets.getApplicationSecretStatus).mockClear()
    auth.principal.user.role = "admin"
    view()
    expect(screen.queryByText("Application keys")).not.toBeInTheDocument()
    expect(secrets.getApplicationSecretStatus).not.toHaveBeenCalled()
  })

  it("requires fresh proof and tracks background rotation to completion", async () => {
    view()
    await screen.findByText("Ready")
    fireEvent.click(screen.getByRole("button", { name: "Rotate data key" }))
    expect(secrets.rotateApplicationDataKey).not.toHaveBeenCalled()
    identify()
    expect(await screen.findByText("Rotating")).toBeInTheDocument()
    expect(security.reauthenticateAdmin).toHaveBeenCalledWith("owner password", "")
    expect(screen.queryByLabelText("Current password")).not.toBeInTheDocument()
    expect(screen.getByRole("button", { name: "Rotate data key" })).toBeDisabled()
    vi.mocked(secrets.getApplicationSecretStatus).mockResolvedValue({ ...ready, generation: 2, data_keys: 2 })
    fireEvent.click(screen.getByRole("button", { name: "Refresh status" }))
    expect(await screen.findByText("Ready")).toBeInTheDocument()
  })

  it("requires the current authenticator code and permits canceling", async () => {
    auth.principal.user.totp_enabled_at = "2026-09-13"
    view()
    await screen.findByText("Ready")
    fireEvent.click(screen.getByRole("button", { name: "Rotate data key" }))
    fireEvent.change(screen.getByLabelText("Current password"), { target: { value: "owner password" } })
    expect(screen.getByRole("button", { name: "Confirm identity and rotate" })).toBeDisabled()
    fireEvent.change(screen.getByLabelText("Authenticator code"), { target: { value: "123456" } })
    fireEvent.click(screen.getByRole("button", { name: "Cancel" }))
    expect(security.reauthenticateAdmin).not.toHaveBeenCalled()
    fireEvent.click(screen.getByRole("button", { name: "Rotate data key" }))
    expect(screen.getByLabelText("Current password")).toHaveValue("")
  })

  it("keeps rejected confirmation retryable and clears entered credentials", async () => {
    vi.mocked(security.reauthenticateAdmin).mockRejectedValueOnce(new Error("proof rejected"))
    view()
    await screen.findByText("Ready")
    fireEvent.click(screen.getByRole("button", { name: "Rotate data key" }))
    identify()
    expect(await screen.findByText(/proof rejected/)).toBeInTheDocument()
    expect(secrets.rotateApplicationDataKey).not.toHaveBeenCalled()
    expect(screen.getByLabelText("Current password")).toHaveValue("")
    identify()
    expect(await screen.findByText("Rotating")).toBeInTheDocument()
  })

  it("provides refresh and recovery guidance after load or rotation failures", async () => {
    vi.mocked(secrets.getApplicationSecretStatus).mockRejectedValueOnce(new Error("unavailable"))
    view()
    expect(await screen.findByText(/Could not read key status/)).toBeInTheDocument()
    fireEvent.click(screen.getByRole("button", { name: "Refresh status" }))
    await screen.findByText("Ready")
    vi.mocked(secrets.getApplicationSecretStatus).mockResolvedValue({ ...ready, healthy: false, rotation: "degraded" })
    vi.mocked(secrets.rotateApplicationDataKey).mockRejectedValueOnce(new Error("rotation unavailable"))
    fireEvent.click(screen.getByRole("button", { name: "Rotate data key" }))
    identify()
    expect(await screen.findByText("Needs recovery")).toBeInTheDocument()
    expect(screen.getByText(/Restart Beat to resume/)).toBeInTheDocument()
  })

  it("blocks duplicate requests and stops polling on unmount", async () => {
    let complete!: () => void
    vi.mocked(security.reauthenticateAdmin).mockImplementation(() => new Promise(resolve => {
      complete = () => resolve({} as Awaited<ReturnType<typeof security.reauthenticateAdmin>>)
    }))
    const rendered = view()
    await screen.findByText("Ready")
    fireEvent.click(screen.getByRole("button", { name: "Rotate data key" }))
    identify()
    expect(screen.getByRole("button", { name: "Confirm identity and rotate" })).toBeDisabled()
    identify()
    expect(security.reauthenticateAdmin).toHaveBeenCalledOnce()
    vi.useFakeTimers()
    await act(async () => complete())
    vi.mocked(secrets.getApplicationSecretStatus).mockResolvedValue({ ...ready, rotation: "running" })
    await act(async () => vi.advanceTimersByTime(3_000))
    expect(secrets.getApplicationSecretStatus).toHaveBeenCalledTimes(2)
    rendered.unmount()
    await act(async () => vi.advanceTimersByTime(6_000))
    expect(secrets.getApplicationSecretStatus).toHaveBeenCalledTimes(2)
  })
})
