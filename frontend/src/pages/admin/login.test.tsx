import { fireEvent, render, screen, waitFor } from "@testing-library/react"
import { beforeEach, describe, expect, it, vi } from "vitest"
import { LocaleProvider } from "@/context/locale"
import AdminLogin from "@/pages/admin/login"

const auth = vi.hoisted(() => ({
  login: vi.fn(), bootstrap: vi.fn(), logout: vi.fn(), refresh: vi.fn(),
  authenticated: false, loading: false, setupRequired: false, principal: null,
}))
vi.mock("@/context/auth", () => ({ useAuth: () => auth }))

describe("admin login", () => {
  beforeEach(() => {
    vi.resetAllMocks()
    auth.setupRequired = false
  })

  it("shows branding heading from fallback site settings", () => {
    render(<LocaleProvider><AdminLogin /></LocaleProvider>)
    expect(screen.getByRole("heading", { level: 1 })).toHaveTextContent("Beat Monitor")
    expect(screen.getByText("Admin access")).toBeInTheDocument()
  })

  it("submits account credentials and requests TOTP when required", async () => {
    auth.login.mockRejectedValueOnce(new Error("TOTP code is required")).mockResolvedValueOnce(undefined)
    render(<LocaleProvider><AdminLogin /></LocaleProvider>)
    fireEvent.change(screen.getByLabelText("Username"), { target: { value: " owner " } })
    fireEvent.change(screen.getByLabelText("Password"), { target: { value: "password" } })
    fireEvent.click(screen.getByRole("button", { name: "Open admin" }))
    expect(await screen.findByLabelText("Authenticator code")).toBeInTheDocument()
    fireEvent.change(screen.getByLabelText("Authenticator code"), { target: { value: "123456" } })
    fireEvent.click(screen.getByRole("button", { name: "Open admin" }))
    await waitFor(() => expect(auth.login).toHaveBeenLastCalledWith(
      expect.objectContaining({ username: " owner ", totpCode: "123456" }),
    ))
  })

  it("creates the first owner in setup mode", async () => {
    auth.setupRequired = true
    auth.bootstrap.mockResolvedValue(undefined)
    render(<LocaleProvider><AdminLogin /></LocaleProvider>)
    fireEvent.change(screen.getByLabelText("Migration token"), { target: { value: "token" } })
    fireEvent.change(screen.getByLabelText("Username"), { target: { value: "owner" } })
    fireEvent.change(screen.getByLabelText("Display name"), { target: { value: "Owner" } })
    fireEvent.change(screen.getByLabelText("Password"), { target: { value: "long password" } })
    fireEvent.click(screen.getByRole("button", { name: "Create owner" }))
    await waitFor(() => expect(auth.bootstrap).toHaveBeenCalled())
  })
})
