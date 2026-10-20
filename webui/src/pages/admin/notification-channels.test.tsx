import userEvent from "@testing-library/user-event"
import { act, fireEvent, render, screen, waitFor, within } from "@testing-library/react"
import { beforeEach, describe, expect, it, vi } from "vitest"
import { LocaleProvider } from "@/context/locale"
import AlertChannelsPanel from "@/pages/admin/alert-channels-panel"
import * as lists from "@/hooks/use-api"
import * as api from "@/lib/api"
import * as delivery from "@/lib/notification-api"
import * as security from "@/lib/security-api"
import { notificationChannel, notificationReceipt } from "@/test/notification-fixtures"

const auth = vi.hoisted(() => ({ principal: { user: { role: "owner", totp_enabled_at: null as string | null }, session: { reauthenticated_until: "2099-01-01T00:00:00Z" } } }))
vi.mock("@/context/auth", () => ({ useAuth: () => auth }))
vi.mock("@/hooks/use-api")
vi.mock("@/lib/api")
vi.mock("@/lib/notification-api")
vi.mock("@/lib/security-api")
vi.mock("@/components/ui/dialog", async () => {
  const m = await vi.importActual<typeof import("@/test/admin-ui-mocks")>("@/test/admin-ui-mocks")
  return { Dialog: m.MockDialog, DialogContent: m.PassThrough, DialogHeader: m.PassThrough, DialogTitle: m.PassThrough, DialogDescription: m.PassThrough, DialogFooter: m.PassThrough }
})
vi.mock("@/components/ui/select", async () => {
  const m = await vi.importActual<typeof import("@/test/admin-ui-mocks")>("@/test/admin-ui-mocks")
  return { Select: m.MockSelect, SelectContent: m.PassThrough, SelectItem: m.MockSelectItem, SelectTrigger: () => null, SelectValue: () => null }
})

const refresh = vi.fn().mockResolvedValue(undefined)
function view(history = vi.fn()) { return render(<LocaleProvider><AlertChannelsPanel onHistory={history} /></LocaleProvider>) }
function create() {
  fireEvent.click(screen.getByRole("button", { name: "Create channel" }))
  fireEvent.change(screen.getByLabelText("Name"), { target: { value: "New channel" } })
  fireEvent.change(screen.getByLabelText(/^Delivery address/), { target: { value: "https://notify.example.test" } })
}

describe("notification channel operations", () => {
  beforeEach(() => {
    vi.resetAllMocks(); localStorage.setItem("locale", "en")
    auth.principal.user.role = "owner"; auth.principal.user.totp_enabled_at = null; auth.principal.session.reauthenticated_until = "2099-01-01T00:00:00Z"
    vi.mocked(lists.useAlertChannels).mockReturnValue({ data: [notificationChannel()], loading: false, error: null, refresh })
    vi.mocked(api.testAlertChannel).mockResolvedValue(notificationReceipt())
    vi.mocked(security.reauthenticateAdmin).mockResolvedValue({ reauthenticated_until: "2099-01-01T00:00:00Z" } as never)
  })
  it("shows operational health, toggles without rewriting secrets and opens history", async () => {
    const history = vi.fn(); view(history)
    expect(screen.getByText("Ready")).toBeInTheDocument()
    fireEvent.click(screen.getByRole("switch", { name: "Enable Operations" }))
    await waitFor(() => expect(delivery.setNotificationChannelEnabled).toHaveBeenCalledWith("c1", false))
    expect(api.updateAlertChannel).not.toHaveBeenCalled()
    fireEvent.click(screen.getByRole("button", { name: "Delivery history for Operations" }))
    expect(history).toHaveBeenCalledWith("c1")
    fireEvent.click(screen.getByRole("button", { name: "Test" }))
    expect(await screen.findByText(/Test request accepted: Queued/)).toBeInTheDocument()
    expect(screen.queryByText("Delivered")).not.toBeInTheDocument()
  })
  it("uses the stored revision and omits blank endpoint values when editing", async () => {
    view(); fireEvent.click(screen.getByRole("button", { name: "Edit Operations" }))
    expect(screen.getByLabelText(/^Delivery address/)).toHaveValue("")
    fireEvent.change(screen.getByLabelText("Name"), { target: { value: "Renamed" } })
    fireEvent.click(screen.getByRole("button", { name: "Save" }))
    await waitFor(() => expect(api.updateAlertChannel).toHaveBeenCalledWith("c1", expect.objectContaining({ name: "Renamed", expected_revision: 3, config: {} })))
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument()
  })
  it("requires current identity proof before creating a channel", async () => {
    auth.principal.session.reauthenticated_until = ""
    auth.principal.user.totp_enabled_at = "enabled"
    view(); create(); fireEvent.click(screen.getByRole("button", { name: "Save" }))
    expect(api.createAlertChannel).not.toHaveBeenCalled()
    fireEvent.change(screen.getByLabelText("Current password"), { target: { value: "test password" } })
    expect(screen.getByRole("button", { name: "Confirm" })).toBeDisabled()
    fireEvent.change(screen.getByLabelText("Authenticator code"), { target: { value: "123456" } })
    fireEvent.click(screen.getByRole("button", { name: "Confirm" }))
    await waitFor(() => expect(api.createAlertChannel).toHaveBeenCalledWith(expect.objectContaining({ config: { endpoint: "https://notify.example.test" } })))
    expect(security.reauthenticateAdmin).toHaveBeenCalledWith("test password", "123456")
  })
  it("validates configuration and previews a masked message without sending", async () => {
    vi.mocked(delivery.previewNotificationChannel).mockResolvedValue({ method: "POST", url: "https://[configured-host]", headers: {}, body: "preview", content_type: "application/json", subject: "Preview", text: "Preview" })
    view(); create()
    fireEvent.change(screen.getByLabelText(/^Delivery address/), { target: { value: "bad" } })
    fireEvent.click(screen.getByRole("button", { name: "Save" }))
    expect(await screen.findByText(/Enter an HTTP or HTTPS address/)).toBeInTheDocument()
    expect(api.createAlertChannel).not.toHaveBeenCalled()
    fireEvent.change(screen.getByLabelText(/^Delivery address/), { target: { value: "https://notify.example.test" } })
    fireEvent.click(screen.getByRole("button", { name: "Preview message" }))
    expect(await screen.findByText("Message preview")).toBeInTheDocument()
    expect(api.testAlertChannel).not.toHaveBeenCalled()
  })
  it("offers lifecycle subscriptions without private template fields", async () => {
    view(); create()
    for (const name of ["Node expiring", "Node expired", "Node date advanced"]) {
      const option = screen.getByRole("checkbox", { name })
      expect(option).toHaveAttribute("aria-checked", "true")
      await userEvent.click(option)
      expect(option).toHaveAttribute("aria-checked", "false")
      await userEvent.click(option)
      expect(option).toHaveAttribute("aria-checked", "true")
    }
    expect(screen.queryByText("private_notes")).not.toBeInTheDocument()
  })
  it("confirms deletion and leaves failed mutations retryable", async () => {
    vi.mocked(api.deleteAlertChannel).mockRejectedValueOnce(new Error("revision changed"))
    view(); fireEvent.click(screen.getByRole("button", { name: "Delete Operations" }))
    fireEvent.click(within(screen.getByRole("dialog")).getByRole("button", { name: "Confirm" }))
    expect(await screen.findAllByText("revision changed")).not.toHaveLength(0)
    fireEvent.click(within(screen.getByRole("dialog")).getByRole("button", { name: "Confirm" }))
    await waitFor(() => expect(api.deleteAlertChannel).toHaveBeenCalledTimes(2))
    await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument())
  })
  it("keeps useful data during refresh errors and hides owner controls for admins", () => {
    auth.principal.user.role = "admin"
    vi.mocked(lists.useAlertChannels).mockReturnValue({ data: [notificationChannel()], loading: false, error: "refresh unavailable", refresh })
    view()
    expect(screen.getByText("Operations")).toBeInTheDocument()
    expect(screen.getByText("refresh unavailable")).toBeInTheDocument()
    expect(screen.queryByRole("button", { name: "Create channel" })).not.toBeInTheDocument()
    expect(screen.queryByRole("button", { name: "Edit Operations" })).not.toBeInTheDocument()
  })
  it("surfaces admin action failures and prevents duplicate submissions", async () => {
    let fail!: (error: Error) => void
    vi.mocked(api.testAlertChannel).mockImplementation(() => new Promise((_, reject) => { fail = reject }))
    view(); fireEvent.click(screen.getByRole("button", { name: "Test" })); fireEvent.click(screen.getByRole("button", { name: "Test" }))
    expect(api.testAlertChannel).toHaveBeenCalledOnce()
    await act(async () => fail(new Error("wait before testing again")))
    expect(screen.getByText("wait before testing again")).toBeInTheDocument()
  })
})
