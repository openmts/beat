import { render, screen } from "@testing-library/react"
import { beforeEach, describe, expect, it, vi } from "vitest"
import { LocaleProvider } from "@/context/locale"
import Security from "@/pages/admin/security"

const auth = vi.hoisted(() => ({ role: "owner" as "owner" | "admin" }))
vi.mock("@/context/auth", () => ({
  useAuth: () => ({ principal: { user: { role: auth.role } } }),
}))
vi.mock("@/pages/admin/security-account-panel", () => ({ SecurityAccountPanel: () => <div>account-panel</div> }))
vi.mock("@/pages/admin/security-admins-panel", () => ({ SecurityAdminsPanel: () => <div>admins-panel</div> }))
vi.mock("@/pages/admin/security-audit-panel", () => ({ SecurityAuditPanel: () => <div>audit-panel</div> }))
vi.mock("@/pages/admin/security-backup-panel", () => ({ SecurityBackupPanel: () => <div>backup-panel</div> }))
vi.mock("@/pages/admin/security-sessions-panel", () => ({ SecuritySessionsPanel: () => <div>sessions-panel</div> }))
vi.mock("@/pages/admin/security-totp-panel", () => ({ SecurityTOTPPanel: () => <div>totp-panel</div> }))

describe("security page roles", () => {
  beforeEach(() => { auth.role = "owner" })

  it("shows owner-only administrator and backup tabs", () => {
    render(<LocaleProvider><Security /></LocaleProvider>)
    expect(screen.getByRole("tab", { name: "Administrators" })).toBeInTheDocument()
    expect(screen.getByRole("tab", { name: "Backup and restore" })).toBeInTheDocument()
  })

  it("hides owner-only tabs from administrators", () => {
    auth.role = "admin"
    render(<LocaleProvider><Security /></LocaleProvider>)
    expect(screen.queryByRole("tab", { name: "Administrators" })).not.toBeInTheDocument()
    expect(screen.queryByRole("tab", { name: "Backup and restore" })).not.toBeInTheDocument()
  })
})
