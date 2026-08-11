import { fireEvent, render, screen, waitFor } from "@testing-library/react"
import { beforeEach, describe, expect, it, vi } from "vitest"
import { LocaleProvider } from "@/context/locale"
import { SecurityAccountPanel } from "@/pages/admin/security-account-panel"
import { SecurityAdminsPanel } from "@/pages/admin/security-admins-panel"
import { SecurityAuditPanel } from "@/pages/admin/security-audit-panel"
import { SecuritySessionsPanel } from "@/pages/admin/security-sessions-panel"
import { SecurityTOTPPanel } from "@/pages/admin/security-totp-panel"
import * as security from "@/lib/security-api"
import type { AdminPrincipal } from "@/types/security"

const auth = vi.hoisted(() => ({
  principal: {
    user: { id: "owner", username: "owner", display_name: "Owner", role: "owner" as const,
      enabled: true, password_changed_at: "", last_login_at: null, totp_enabled_at: null,
      created_at: "", updated_at: "" },
    session: { id: "session", user_id: "owner", token_prefix: "prefix", created_at: "",
      last_activity_at: "2026-07-30T00:00:00Z", idle_expires_at: "", absolute_expires_at: "2026-08-01T00:00:00Z",
      reauthenticated_until: null, ip_address: "::1", user_agent: "Browser", revoked_at: null },
  } as AdminPrincipal,
  refresh: vi.fn(),
}))

vi.mock("@/context/auth", () => ({ useAuth: () => auth }))
vi.mock("@/lib/security-api")

function renderPanel(panel: React.ReactNode) {
  return render(<LocaleProvider>{panel}</LocaleProvider>)
}

describe("security panels", () => {
  beforeEach(() => {
    vi.resetAllMocks()
    auth.principal.user.totp_enabled_at = null
    vi.mocked(security.reauthenticateAdmin).mockResolvedValue(auth.principal.session)
    vi.mocked(security.changeAdminPassword).mockResolvedValue()
    vi.mocked(security.listAdminUsers).mockResolvedValue([auth.principal.user])
    vi.mocked(security.createAdminUser).mockResolvedValue(auth.principal.user)
    vi.mocked(security.deleteAdminUser).mockResolvedValue()
    vi.mocked(security.listAdminSessions).mockResolvedValue([{ ...auth.principal.session, current: true }])
    vi.mocked(security.revokeAdminSession).mockResolvedValue()
    vi.mocked(security.revokeOtherAdminSessions).mockResolvedValue(1)
    vi.mocked(security.listAuditEvents).mockResolvedValue({ events: [{ id: "event", request_id: "request",
      actor_id: "owner", actor_username: "owner", action: "backup.create", resource_type: "backup",
      resource_id: "backup", outcome: "success", detail_json: "{}", ip_address: "::1",
      user_agent: "Browser", session_prefix: "prefix", created_at: "2026-07-30T00:00:00Z" }],
      total: 1, limit: 50, offset: 0 })
    vi.mocked(security.beginTOTP).mockResolvedValue({ secret: "SECRET", uri: "otpauth://test" })
    vi.mocked(security.enableTOTP).mockResolvedValue()
    vi.mocked(security.disableTOTP).mockResolvedValue()
  })

  it("reauthenticates and changes the current password", async () => {
    renderPanel(<SecurityAccountPanel />)
    const passwords = screen.getAllByLabelText("Current password")
    fireEvent.change(passwords[0], { target: { value: "current-password" } })
    fireEvent.click(screen.getByRole("button", { name: "Confirm identity" }))
    await waitFor(() => expect(security.reauthenticateAdmin).toHaveBeenCalledWith("current-password", ""))

    fireEvent.change(passwords[1], { target: { value: "current-password" } })
    fireEvent.change(screen.getByLabelText("New password"), { target: { value: "replacement-password" } })
    fireEvent.click(screen.getByRole("button", { name: "Change password" }))
    await waitFor(() => expect(security.changeAdminPassword).toHaveBeenCalledWith(expect.objectContaining({
      new_password: "replacement-password",
    })))
  })

  it("creates and removes administrators", async () => {
    renderPanel(<SecurityAdminsPanel />)
    await screen.findByText("@owner · Owner")
    fireEvent.change(screen.getByLabelText("Username"), { target: { value: "new-admin" } })
    fireEvent.change(screen.getByLabelText("Display name"), { target: { value: "New Admin" } })
    fireEvent.change(screen.getByLabelText("Password"), { target: { value: "new-admin-password" } })
    fireEvent.click(screen.getByRole("button", { name: "Create administrator" }))
    await waitFor(() => expect(security.createAdminUser).toHaveBeenCalled())
    fireEvent.click(screen.getByRole("button", { name: "Delete" }))
    await waitFor(() => expect(security.deleteAdminUser).toHaveBeenCalledWith("owner"))
  })

  it("lists and revokes sessions", async () => {
    renderPanel(<SecuritySessionsPanel />)
    await screen.findByText("Browser")
    fireEvent.click(screen.getByRole("button", { name: "Revoke other sessions" }))
    await waitFor(() => expect(security.revokeOtherAdminSessions).toHaveBeenCalled())
    fireEvent.click(screen.getByRole("button", { name: "Revoke" }))
    await waitFor(() => expect(security.revokeAdminSession).toHaveBeenCalledWith("session"))
  })

  it("renders audit events", async () => {
    renderPanel(<SecurityAuditPanel />)
    expect(await screen.findByText("backup.create")).toBeInTheDocument()
    expect(screen.getByText("success")).toBeInTheDocument()
  })

  it("sets up, enables, and disables TOTP", async () => {
    const rendered = renderPanel(<SecurityTOTPPanel />)
    fireEvent.click(screen.getByRole("button", { name: "Set up authenticator" }))
    await screen.findByText(/SECRET/)
    fireEvent.change(screen.getByLabelText("Authenticator code"), { target: { value: "123456" } })
    fireEvent.click(screen.getByRole("button", { name: "Enable authenticator" }))
    await waitFor(() => expect(security.enableTOTP).toHaveBeenCalledWith("123456"))
    auth.principal.user.totp_enabled_at = "2026-07-30T00:00:00Z"
    rendered.rerender(<LocaleProvider><SecurityTOTPPanel /></LocaleProvider>)
    fireEvent.click(screen.getByRole("button", { name: "Disable authenticator" }))
    await waitFor(() => expect(security.disableTOTP).toHaveBeenCalled())
  })
})
