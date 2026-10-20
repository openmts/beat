import { act, fireEvent, render, screen, waitFor } from "@testing-library/react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { LocaleProvider } from "@/context/locale"
import { SecurityTOTPPanel } from "@/pages/admin/security-totp-panel"
import * as security from "@/lib/security-api"

const auth = vi.hoisted(() => ({
  principal: { user: { totp_enabled_at: null as string | null } },
  refresh: vi.fn(),
}))

vi.mock("@/context/auth", () => ({ useAuth: () => auth }))
vi.mock("@/lib/security-api")

function view() {
  localStorage.setItem("locale", "en")
  return render(<LocaleProvider><SecurityTOTPPanel /></LocaleProvider>)
}

function confirmIdentity() {
  fireEvent.change(screen.getByLabelText("Current password"), { target: { value: "correct password" } })
  fireEvent.click(screen.getByRole("button", { name: "Confirm identity" }))
}

describe("authenticator confirmation", () => {
  beforeEach(() => {
    vi.resetAllMocks()
    auth.principal.user.totp_enabled_at = null
    vi.mocked(security.beginTOTP).mockResolvedValue({ secret: "PENDING_SECRET", uri: "otpauth://test" })
    vi.mocked(security.enableTOTP).mockResolvedValue()
    vi.mocked(security.disableTOTP).mockResolvedValue()
    auth.refresh.mockResolvedValue(undefined)
  })
  afterEach(() => vi.useRealTimers())

  it("confirms identity before beginning setup and requires proof before enabling", async () => {
    view()
    fireEvent.click(screen.getByRole("button", { name: "Set up authenticator" }))
    expect(security.beginTOTP).not.toHaveBeenCalled()
    confirmIdentity()
    expect(await screen.findByText("PENDING_SECRET")).toBeInTheDocument()
    expect(security.reauthenticateAdmin).toHaveBeenCalledWith("correct password", "")
    expect(security.enableTOTP).not.toHaveBeenCalled()
    expect(screen.getByRole("button", { name: "Enable authenticator" })).toBeDisabled()
    fireEvent.change(screen.getByLabelText("Authenticator code"), { target: { value: "123456" } })
    fireEvent.click(screen.getByRole("button", { name: "Enable authenticator" }))
    await waitFor(() => expect(security.enableTOTP).toHaveBeenCalledWith("123456"))
    await waitFor(() => expect(screen.queryByText("PENDING_SECRET")).not.toBeInTheDocument())
  })

  it("keeps the factor enabled until password and current code confirm disabling", async () => {
    auth.principal.user.totp_enabled_at = "2026-09-13T00:00:00Z"
    view()
    fireEvent.click(screen.getByRole("button", { name: "Disable authenticator" }))
    expect(security.disableTOTP).not.toHaveBeenCalled()
    fireEvent.change(screen.getByLabelText("Current authenticator code"), { target: { value: "654321" } })
    confirmIdentity()
    await waitFor(() => expect(security.disableTOTP).toHaveBeenCalledOnce())
    expect(security.reauthenticateAdmin).toHaveBeenCalledWith("correct password", "654321")
  })

  it("keeps identity confirmation retryable after an error", async () => {
    vi.mocked(security.reauthenticateAdmin).mockRejectedValueOnce(new Error("invalid credentials"))
    view()
    fireEvent.click(screen.getByRole("button", { name: "Set up authenticator" }))
    confirmIdentity()
    expect(await screen.findByText("invalid credentials")).toBeInTheDocument()
    expect(security.beginTOTP).not.toHaveBeenCalled()
    confirmIdentity()
    expect(await screen.findByText("PENDING_SECRET")).toBeInTheDocument()
  })

  it("removes the unconfirmed secret when canceled or expired", async () => {
    view()
    fireEvent.click(screen.getByRole("button", { name: "Set up authenticator" }))
    confirmIdentity()
    await screen.findByText("PENDING_SECRET")
    fireEvent.click(screen.getByRole("button", { name: "Cancel" }))
    expect(screen.queryByText("PENDING_SECRET")).not.toBeInTheDocument()
    expect(security.enableTOTP).not.toHaveBeenCalled()
    vi.useFakeTimers()
    fireEvent.click(screen.getByRole("button", { name: "Set up authenticator" }))
    await act(async () => confirmIdentity())
    expect(screen.getByText("PENDING_SECRET")).toBeInTheDocument()
    act(() => vi.advanceTimersByTime(5 * 60_000))
    expect(screen.queryByText("PENDING_SECRET")).not.toBeInTheDocument()
    expect(screen.getByText("Authenticator setup expired. Start setup again.")).toBeInTheDocument()
  })

  it("blocks duplicate submissions while identity confirmation is pending", async () => {
    let complete!: () => void
    vi.mocked(security.reauthenticateAdmin).mockImplementation(() => new Promise(resolve => {
      complete = () => resolve({} as Awaited<ReturnType<typeof security.reauthenticateAdmin>>)
    }))
    view()
    fireEvent.click(screen.getByRole("button", { name: "Set up authenticator" }))
    confirmIdentity()
    expect(screen.getByRole("button", { name: "Confirm identity" })).toBeDisabled()
    fireEvent.click(screen.getByRole("button", { name: "Confirm identity" }))
    expect(security.reauthenticateAdmin).toHaveBeenCalledOnce()
    await act(async () => complete())
    expect(await screen.findByText("PENDING_SECRET")).toBeInTheDocument()
  })
})
