import { fireEvent, render, screen, waitFor, within } from "@testing-library/react"
import { beforeEach, describe, expect, it, vi } from "vitest"
import { LocaleProvider } from "@/context/locale"
import { SecurityEventsPanel } from "@/pages/admin/security-events-panel"
import type { SecurityEvent, SecurityEventPage, SecurityNotificationSettings, SecurityOptions } from "@/types/security"

const auth = vi.hoisted(() => ({
  principal: {
    user: { id: "u1", role: "owner" as "owner" | "admin", totp_enabled_at: null as string | null },
    session: { id: "session-1", reauthenticated_until: "2099-01-01T00:00:00Z" },
  },
}))
vi.mock("@/context/auth", () => ({ useAuth: () => auth }))

const api = vi.hoisted(() => ({
  list: vi.fn(), detail: vi.fn(), ack: vi.fn(), options: vi.fn(), policy: vi.fn(), savePolicy: vi.fn(), reauth: vi.fn(), source: vi.fn(),
}))
vi.mock("@/lib/security-api", () => ({
  listSecurityEvents: (filter: unknown, signal?: AbortSignal) => api.list(filter, signal),
  getSecurityEvent: (id: string, signal?: AbortSignal) => api.detail(id, signal),
  acknowledgeSecurityEvent: (id: string, note: string) => api.ack(id, note),
  getSecurityOptions: (signal?: AbortSignal) => api.options(signal),
  getSecurityPolicy: (signal?: AbortSignal) => api.policy(signal),
  updateSecurityPolicy: (payload: unknown) => api.savePolicy(payload),
  reauthenticateAdmin: (password: string, code?: string) => api.reauth(password, code),
  getSecurityEventSource: (id: string, signal?: AbortSignal) => api.source(id, signal),
}))
vi.mock("@/components/ui/dialog", async () => {
  const m = await vi.importActual<typeof import("@/test/admin-ui-mocks")>("@/test/admin-ui-mocks")
  return { Dialog: m.MockDialog, DialogContent: m.PassThrough, DialogHeader: m.PassThrough, DialogTitle: m.PassThrough, DialogDescription: m.PassThrough, DialogFooter: m.PassThrough }
})
vi.mock("@/components/ui/sheet", async () => {
  const m = await vi.importActual<typeof import("@/test/admin-ui-mocks")>("@/test/admin-ui-mocks")
  return { Sheet: m.MockDialog, SheetContent: m.PassThrough, SheetHeader: m.PassThrough, SheetTitle: m.PassThrough, SheetDescription: m.PassThrough }
})
vi.mock("@/components/ui/select", async () => {
  const m = await vi.importActual<typeof import("@/test/admin-ui-mocks")>("@/test/admin-ui-mocks")
  return { Select: m.MockSelect, SelectContent: m.PassThrough, SelectItem: m.MockSelectItem, SelectTrigger: () => null, SelectValue: () => null }
})

function securityEvent(overrides: Partial<SecurityEvent> = {}): SecurityEvent {
  return {
    id: "evt-1", kind: "security.login_suspicious", severity: "high",
    actor_user_id: "u-actor-9", actor_display_name: "Owner", auth_method: "password",
    source_class: "public", source_family: "ipv4", source_summary: "203.0.113.0/24", source_kind: "prefix",
    browser_family: "Chrome", os_family: "Linux", device_class: "desktop", device_summary: "Chrome / Linux",
    country_code: "CN", region: "Chongqing", reasons: ["new_source_prefix"], policy_revision: 3,
    routing_state: "routed", audit_event_id: "audit-77", message_id: "msg-88",
    occurred_at: "2026-10-14T08:00:00Z", retain_until: "2027-04-12T08:00:00Z",
    delivery_state: "failed", delivery_channels: 1, delivery_failures: 1, delivery_pending: 0,
    deliveries: [{ delivery_id: "d-1", channel_id: "c-1", channel_name: "Operations", state: "failed", attempt_count: 2, error_code: "provider_rejected", updated_at: "2026-10-14T08:00:05Z" }],
    ...overrides,
  }
}

const options: SecurityOptions = {
  kinds: [{ value: "security.login_succeeded", label: "Sign-in succeeded" }, { value: "security.login_suspicious", label: "Suspicious sign-in" }],
  severities: [{ value: "info", label: "Info" }, { value: "medium", label: "Medium" }, { value: "high", label: "High" }],
  reasons: [{ value: "new_source_prefix", label: "New source network" }],
  sources: [{ value: "public", label: "Public network" }],
  deliveries: [{ value: "failed", label: "Failed" }],
  routing_states: [{ value: "routed", label: "Routed" }],
  privacy_levels: [{ value: "coarse", label: "Coarse source (recommended)" }, { value: "full_source", label: "Full source address" }],
}

const policy: SecurityNotificationSettings = {
  enabled_event_kinds: ["security.login_succeeded"], privacy_level: "coarse", lookback_days: 30,
  threshold_failures: 5, threshold_window_seconds: 900, invalid_totp_threshold: 5, retention_days: 180,
  revision: 7, updated_at: "2026-10-14T00:00:00Z", updated_by: "u1",
}

function page(overrides: Partial<SecurityEventPage> = {}): SecurityEventPage {
  return { events: [securityEvent()], total: 30, limit: 25, offset: 0, ...overrides }
}

function view() { return render(<LocaleProvider><SecurityEventsPanel /></LocaleProvider>) }

describe("sign-in security events console", () => {
  beforeEach(() => {
    vi.resetAllMocks()
    localStorage.setItem("locale", "en")
    auth.principal.user.role = "owner"
    Object.defineProperty(document, "hidden", { value: false, configurable: true })
    api.list.mockResolvedValue(page())
    api.detail.mockResolvedValueOnce(securityEvent())
      .mockResolvedValue(securityEvent({ acknowledged_at: "2026-10-14T09:00:00Z", acknowledged_by: "u1", acknowledged_note: "investigated" }))
    api.ack.mockResolvedValue(securityEvent({ acknowledged_at: "2026-10-14T09:00:00Z", acknowledged_by: "u1" }))
    api.options.mockResolvedValue(options)
    api.policy.mockResolvedValue(policy)
    api.savePolicy.mockResolvedValue({ ...policy, revision: 8, retention_days: 90 })
    api.reauth.mockResolvedValue({ id: "session-1", reauthenticated_until: "2099-01-01T00:00:00Z" })
  })

  it("renders labels instead of machine values and keeps identifiers out of the DOM", async () => {
    view()
    await screen.findByText("Total:")
    const table = within(screen.getByRole("table"))
    expect(table.getByText("Suspicious sign-in")).toBeInTheDocument()
    expect(table.getByText("High")).toBeInTheDocument()
    expect(table.getByText("New source network")).toBeInTheDocument()
    expect(table.getByText("203.0.113.0/24")).toBeInTheDocument()
    expect(screen.getByText("Total:")).toBeInTheDocument()
    const text = document.body.textContent ?? ""
    for (const hidden of ["security.login_suspicious", "u-actor-9", "audit-77", "msg-88", "evt-1", "full_source"]) {
      expect(text).not.toContain(hidden)
    }
  })

  it("filters by machine value while showing the localized label", async () => {
    view()
    await screen.findByText("Suspicious sign-in")
    const kind = screen.getAllByTestId("select")[0]
    fireEvent.change(kind, { target: { value: "security.login_succeeded" } })
    await waitFor(() => expect(api.list).toHaveBeenLastCalledWith(expect.objectContaining({ kind: "security.login_succeeded", offset: 0 }), expect.any(AbortSignal)))
    expect(kind).toHaveAttribute("data-selected-label", "Sign-in succeeded")
    fireEvent.click(screen.getByRole("button", { name: "Next" }))
    await waitFor(() => expect(api.list).toHaveBeenLastCalledWith(expect.objectContaining({ offset: 25 }), expect.any(AbortSignal)))
    fireEvent.click(screen.getByRole("button", { name: "Previous" }))
    await waitFor(() => expect(api.list).toHaveBeenLastCalledWith(expect.objectContaining({ offset: 0 }), expect.any(AbortSignal)))
  })

  it("inspects delivery evidence and records an acknowledgement", async () => {
    view()
    fireEvent.click(await screen.findByRole("button", { name: "Details: Suspicious sign-in" }))
    const sheet = within(await screen.findByRole("dialog"))
    expect(await sheet.findByText("Operations")).toBeInTheDocument()
    expect(sheet.getByText("Notification delivery failed. Check the channel.")).toBeInTheDocument()
    expect(sheet.getByText("provider_rejected")).toBeInTheDocument()
    fireEvent.change(screen.getByLabelText("Note (optional)"), { target: { value: "investigated" } })
    fireEvent.click(screen.getByRole("button", { name: "Record acknowledgement" }))
    await waitFor(() => expect(api.ack).toHaveBeenCalledWith("evt-1", "investigated"))
    expect(await sheet.findByText("Acknowledged")).toBeInTheDocument()
    expect(sheet.getByText("investigated")).toBeInTheDocument()
    await waitFor(() => expect(api.list).toHaveBeenCalledTimes(2))
    await waitFor(() => expect(api.detail).toHaveBeenCalledTimes(2))
  })

  it("saves the owner policy with the loaded revision", async () => {
    view()
    expect(await screen.findByText("Sign-in security notification policy")).toBeInTheDocument()
    fireEvent.change(screen.getByLabelText("Event retention (days)"), { target: { value: "90" } })
    fireEvent.click(screen.getByRole("button", { name: "Save policy" }))
    fireEvent.click(await screen.findByRole("button", { name: "Confirm" }))
    await waitFor(() => expect(api.savePolicy).toHaveBeenCalledWith(expect.objectContaining({ expected_revision: 7, retention_days: 90 })))
    expect(await screen.findByText("Policy saved.")).toBeInTheDocument()
    expect(api.reauth).not.toHaveBeenCalled()
  })

  it("warns that a full source address leaves the server", async () => {
    api.policy.mockResolvedValue({ ...policy, privacy_level: "full_source" })
    view()
    expect(await screen.findByText(/The full address is sent through third-party channels/)).toBeInTheDocument()
  })

  it("hides the owner policy from administrators", async () => {
    auth.principal.user.role = "admin"
    view()
    await screen.findByText("Suspicious sign-in")
    expect(screen.queryByText("Sign-in security notification policy")).not.toBeInTheDocument()
    expect(api.policy).not.toHaveBeenCalled()
  })

  it("validates subscriptions before requesting confirmation", async () => {
    api.policy.mockResolvedValue({ ...policy, enabled_event_kinds: [] })
    view()
    fireEvent.click(await screen.findByRole("button", { name: "Save policy" }))
    expect(await screen.findByText("Subscribe to at least one event type.")).toBeInTheDocument()
    expect(api.savePolicy).not.toHaveBeenCalled()
  })
  it("reports a failing history request without hiding the filters", async () => {
    api.list.mockRejectedValue(new Error("security history is unavailable"))
    view()
    expect(await screen.findByText("security history is unavailable")).toBeInTheDocument()
    expect(screen.getByText("Event type")).toBeInTheDocument()
  })

  it("marks an event that no channel subscribed to", async () => {
    api.list.mockResolvedValue(page({ events: [securityEvent({ delivery_state: "none", delivery_failures: 0, delivery_channels: 0, deliveries: [] })] }))
    api.detail.mockReset()
    api.detail.mockResolvedValue(securityEvent({ delivery_state: "none", delivery_failures: 0, delivery_channels: 0, deliveries: [] }))
    view()
    const table = within(await screen.findByRole("table"))
    expect(table.getByText("Not routed")).toBeInTheDocument()
    expect(table.queryByText("Notification delivery failed. Check the channel.")).not.toBeInTheDocument()
    fireEvent.click(screen.getByRole("button", { name: "Details: Suspicious sign-in" }))
    expect(await within(screen.getByRole("dialog")).findByText("No channel matched this event.")).toBeInTheDocument()
  })

  it("localizes the console in Chinese", async () => {
    localStorage.setItem("locale", "zh-CN")
    view()
    expect(await screen.findByText(/按时间核对/)).toBeInTheDocument()
    const table = within(screen.getByRole("table"))
    expect(table.getByText("可疑登录")).toBeInTheDocument()
    expect(table.getByText("高")).toBeInTheDocument()
  })

  it("reveals the protected source address after confirmation under full-source privacy", async () => {
    api.policy.mockResolvedValue({ ...policy, privacy_level: "full_source" })
    api.source.mockResolvedValue({ event_id: "evt-1", address: "203.0.113.47", captured_at: "2026-10-14T08:00:00Z" })
    view()
    fireEvent.click(await screen.findByRole("button", { name: "Details: Suspicious sign-in" }))
    const sheet = within(await screen.findByRole("dialog"))
    expect(api.source).not.toHaveBeenCalled()
    fireEvent.click(await sheet.findByRole("button", { name: "Reveal source address" }))
    fireEvent.click(await screen.findByRole("button", { name: "Confirm" }))
    expect(await sheet.findByText("203.0.113.47")).toBeInTheDocument()
    expect(api.source).toHaveBeenCalledTimes(1)
    expect(api.source.mock.calls[0]?.[0]).toBe("evt-1")
    expect(api.reauth).not.toHaveBeenCalled()
  })

  it("keeps the protected source address out of the detail while privacy stays coarse", async () => {
    view()
    fireEvent.click(await screen.findByRole("button", { name: "Details: Suspicious sign-in" }))
    const sheet = within(await screen.findByRole("dialog"))
    expect(await sheet.findByText("Operations")).toBeInTheDocument()
    expect(sheet.queryByText("Reveal source address")).not.toBeInTheDocument()
    expect(api.source).not.toHaveBeenCalled()
  })
})
