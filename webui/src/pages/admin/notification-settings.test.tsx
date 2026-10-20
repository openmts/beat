import { fireEvent, render, screen, waitFor } from "@testing-library/react"
import { beforeEach, describe, expect, it, vi } from "vitest"
import { LocaleProvider } from "@/context/locale"
import NotificationSettingsPanel from "@/pages/admin/notification-settings-panel"
import * as api from "@/lib/notification-api"
import { notificationSettings } from "@/test/notification-fixtures"

const auth = vi.hoisted(() => ({ principal: { user: { role: "owner" }, session: { reauthenticated_until: "2099-01-01T00:00:00Z" } } }))
vi.mock("@/context/auth", () => ({ useAuth: () => auth }))
vi.mock("@/lib/notification-api")
vi.mock("@/components/ui/select", async () => {
  const m = await vi.importActual<typeof import("@/test/admin-ui-mocks")>("@/test/admin-ui-mocks")
  return { Select: m.MockSelect, SelectContent: m.PassThrough, SelectItem: m.MockSelectItem, SelectTrigger: () => null, SelectValue: () => null }
})

function view() { return render(<LocaleProvider><NotificationSettingsPanel /></LocaleProvider>) }

describe("notification settings", () => {
  beforeEach(() => {
    vi.resetAllMocks(); localStorage.setItem("locale", "en"); auth.principal.user.role = "owner"
    vi.mocked(api.getNotificationSettings).mockResolvedValue(notificationSettings())
    vi.mocked(api.saveNotificationSettings).mockImplementation(async settings => ({ ...settings, revision: settings.revision + 1 }))
  })
  it("saves reviewed destinations and retention with the current revision", async () => {
    view(); await screen.findByLabelText("Concurrent deliveries")
    fireEvent.change(screen.getByLabelText("Successful history (days)"), { target: { value: "45" } })
    fireEvent.change(screen.getByLabelText("Failed history (days)"), { target: { value: "180" } })
    fireEvent.change(screen.getByLabelText("Concurrent deliveries"), { target: { value: "4" } })
    fireEvent.click(screen.getByRole("button", { name: "Add exception" }))
    fireEvent.change(screen.getByLabelText("Host or subnet"), { target: { value: "alerts.internal" } })
    fireEvent.change(screen.getByLabelText("Port"), { target: { value: "8443" } })
    fireEvent.change(screen.getByLabelText("Purpose"), { target: { value: "Internal alerts" } })
    fireEvent.click(screen.getByRole("button", { name: "Save" }))
    await waitFor(() => expect(api.saveNotificationSettings).toHaveBeenCalledWith(expect.objectContaining({ revision: 1, success_retention_days: 45, dead_retention_days: 180, maximum_workers: 4,
      egress_policy: expect.objectContaining({ exceptions: [expect.objectContaining({ host: "alerts.internal", port: 8443, purpose: "Internal alerts" })] }) })))
    expect(await screen.findByText("Changes saved.")).toBeInTheDocument()
    fireEvent.change(screen.getByTestId("select"), { target: { value: "cidr" } })
    fireEvent.change(screen.getByLabelText("Host or subnet"), { target: { value: "10.1.2.0/24" } })
    fireEvent.change(screen.getByLabelText("Expires (local time)"), { target: { value: new Date(Date.now() + 86_400_000).toISOString().slice(0, 16) } })
    fireEvent.click(screen.getByRole("button", { name: "Save" }))
    await waitFor(() => expect(api.saveNotificationSettings).toHaveBeenLastCalledWith(expect.objectContaining({ revision: 2 })))
    fireEvent.click(screen.getByRole("button", { name: "Remove exception" }))
    expect(screen.getByText("No internal destination exceptions.")).toBeInTheDocument()
  })
  it("recovers from a load error and retains edits when a save is rejected", async () => {
    vi.mocked(api.getNotificationSettings).mockRejectedValueOnce(new Error("temporarily unavailable"))
    view(); expect(await screen.findByText("temporarily unavailable")).toBeInTheDocument()
    fireEvent.click(screen.getByRole("button", { name: "Reload saved settings" }))
    await screen.findByLabelText("Concurrent deliveries")
    fireEvent.change(screen.getByLabelText("Concurrent deliveries"), { target: { value: "33" } })
    fireEvent.click(screen.getByRole("button", { name: "Save" }))
    expect(await screen.findByText(/Use 7–365 successful-history/)).toBeInTheDocument()
    expect(api.saveNotificationSettings).not.toHaveBeenCalled()
    fireEvent.change(screen.getByLabelText("Concurrent deliveries"), { target: { value: "4" } })
    vi.mocked(api.saveNotificationSettings).mockRejectedValueOnce(new Error("settings changed; reload"))
    fireEvent.click(screen.getByRole("button", { name: "Save" }))
    expect(await screen.findByText("settings changed; reload")).toBeInTheDocument()
    expect(screen.getByLabelText("Concurrent deliveries")).toHaveValue(4)
  })
  it("restricts destination policy inspection to owners", () => {
    auth.principal.user.role = "admin"
    view()
    expect(screen.queryByLabelText("Concurrent deliveries")).not.toBeInTheDocument()
    expect(api.getNotificationSettings).not.toHaveBeenCalled()
    expect(screen.queryByRole("button", { name: "Save" })).not.toBeInTheDocument()
    expect(screen.queryByRole("button", { name: "Add exception" })).not.toBeInTheDocument()
  })
})
