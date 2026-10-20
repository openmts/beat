import { act, fireEvent, render, screen, waitFor, within } from "@testing-library/react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { LocaleProvider } from "@/context/locale"
import NotificationDeliveriesPanel from "@/pages/admin/notification-deliveries-panel"
import * as lists from "@/hooks/use-api"
import * as api from "@/lib/notification-api"
import { notificationChannel, notificationDelivery } from "@/test/notification-fixtures"
import type { DeliveryPage } from "@/types/notification"

const auth = vi.hoisted(() => ({ principal: { user: { role: "owner" }, session: { reauthenticated_until: "2099-01-01T00:00:00Z" } } }))
vi.mock("@/context/auth", () => ({ useAuth: () => auth }))
vi.mock("@/hooks/use-api")
vi.mock("@/lib/notification-api")
vi.mock("@/components/ui/dialog", async () => {
  const m = await vi.importActual<typeof import("@/test/admin-ui-mocks")>("@/test/admin-ui-mocks")
  return { Dialog: m.MockDialog, DialogContent: m.PassThrough, DialogHeader: m.PassThrough, DialogTitle: m.PassThrough, DialogDescription: m.PassThrough, DialogFooter: m.PassThrough }
})
vi.mock("@/components/ui/select", async () => {
  const m = await vi.importActual<typeof import("@/test/admin-ui-mocks")>("@/test/admin-ui-mocks")
  return { Select: m.MockSelect, SelectContent: m.PassThrough, SelectItem: m.MockSelectItem, SelectTrigger: () => null, SelectValue: () => null }
})

function view() { return render(<LocaleProvider><NotificationDeliveriesPanel /></LocaleProvider>) }
const page = (overrides: Partial<DeliveryPage> = {}): DeliveryPage => ({ items: [notificationDelivery()], total: 26, offset: 0, limit: 25, ...overrides })

describe("durable notification history", () => {
  beforeEach(() => {
    vi.resetAllMocks(); localStorage.setItem("locale", "en"); auth.principal.user.role = "owner"
    vi.mocked(lists.useAlertChannels).mockReturnValue({ data: [notificationChannel()], loading: false, error: null, refresh: vi.fn() })
    vi.mocked(api.listNotificationDeliveries).mockResolvedValue(page())
    vi.mocked(api.getNotificationDelivery).mockResolvedValue(notificationDelivery())
    Object.defineProperty(document, "hidden", { value: false, configurable: true })
  })
  afterEach(() => vi.useRealTimers())

  it("filters by readable channel and state names and paginates", async () => {
    view(); await screen.findByRole("button", { name: "Details" })
    const selectors = screen.getAllByTestId("select")
    fireEvent.change(selectors[0], { target: { value: "c1" } })
    expect(selectors[0]).toHaveAttribute("data-selected-label", "Operations")
    await waitFor(() => expect(api.listNotificationDeliveries).toHaveBeenLastCalledWith(expect.objectContaining({ channel_id: "c1", offset: 0 }), expect.any(AbortSignal)))
    fireEvent.change(selectors[1], { target: { value: "dead" } })
    await waitFor(() => expect(api.listNotificationDeliveries).toHaveBeenLastCalledWith(expect.objectContaining({ state: "dead" }), expect.any(AbortSignal)))
    fireEvent.click(screen.getByRole("button", { name: "Next" }))
    await waitFor(() => expect(api.listNotificationDeliveries).toHaveBeenLastCalledWith(expect.objectContaining({ offset: 25 }), expect.any(AbortSignal)))
    fireEvent.click(screen.getByRole("button", { name: "Previous" }))
    await waitFor(() => expect(api.listNotificationDeliveries).toHaveBeenLastCalledWith(expect.objectContaining({ offset: 0 }), expect.any(AbortSignal)))
  })
  it("shows attempt evidence and requires an explicit duplicate acknowledgement to retry", async () => {
    vi.mocked(api.getNotificationDelivery).mockImplementation(async id => notificationDelivery({ id, state: id === "retry" ? "pending" : "dead", attempt_count: 1,
      attempts: [{ number: 1, started_at: "2026-09-13T00:00:00Z", duration_ms: 50, outcome: "permanent", error_code: "provider_rejected", error_stage: "response", status_class: "4xx" }], retry_of: id === "retry" ? "d1" : undefined }))
    vi.mocked(api.retryNotificationDelivery).mockResolvedValue({ id: "retry", channel_id: "c1", channel_revision: 4, state: "pending", attempt_count: 0 })
    view(); fireEvent.click(await screen.findByRole("button", { name: "Details" }))
    await screen.findByText("Attempt 1")
    expect(screen.getByText(/provider_rejected/)).toBeInTheDocument()
    fireEvent.click(screen.getByRole("button", { name: "Retry delivery" }))
    expect(screen.getByRole("button", { name: "Confirm" })).toBeDisabled()
    fireEvent.click(screen.getByRole("checkbox", { name: /I understand this retry/ }))
    fireEvent.click(screen.getByRole("button", { name: "Confirm" }))
    await waitFor(() => expect(api.retryNotificationDelivery).toHaveBeenCalledWith("d1"))
    expect(await screen.findByText("Retry record: Queued.")).toBeInTheDocument()
    await waitFor(() => expect(api.getNotificationDelivery).toHaveBeenLastCalledWith("retry", expect.any(AbortSignal)))
  })
  it("cancels unfinished delivery and preserves a retryable error", async () => {
    vi.mocked(api.cancelNotificationDelivery).mockRejectedValueOnce(new Error("state changed"))
    view(); fireEvent.click(await screen.findByRole("button", { name: "Details" }))
    fireEvent.click(await screen.findByRole("button", { name: "Cancel delivery" }))
    fireEvent.click(screen.getByRole("button", { name: "Confirm" }))
    await screen.findAllByText("state changed")
    fireEvent.click(screen.getByRole("button", { name: "Confirm" }))
    expect(await screen.findByText("Delivery cancelled. Earlier attempts are retained.")).toBeInTheDocument()
    expect(api.cancelNotificationDelivery).toHaveBeenCalledTimes(2)
  })
  it("aborts old filters and ignores responses that arrive after a newer selection", async () => {
    let resolveOld!: (page: DeliveryPage) => void
    let oldSignal!: AbortSignal
    vi.mocked(api.listNotificationDeliveries).mockImplementationOnce((_, signal) => { oldSignal = signal!; return new Promise(resolve => { resolveOld = resolve }) })
    view()
    fireEvent.change(screen.getAllByTestId("select")[1], { target: { value: "dead" } })
    await screen.findByRole("button", { name: "Details" })
    expect(oldSignal.aborted).toBe(true)
    await act(async () => resolveOld(page({ items: [notificationDelivery({ channel_name: "Obsolete" })] })))
    expect(screen.queryByText("Obsolete")).not.toBeInTheDocument()
  })
  it("polls only while visible and stops outstanding work on unmount", async () => {
    const rendered = view(); await screen.findByRole("button", { name: "Details" })
    vi.useFakeTimers()
    Object.defineProperty(document, "hidden", { value: true, configurable: true })
    act(() => document.dispatchEvent(new Event("visibilitychange")))
    await act(async () => vi.advanceTimersByTimeAsync(10_000))
    expect(api.listNotificationDeliveries).toHaveBeenCalledOnce()
    Object.defineProperty(document, "hidden", { value: false, configurable: true })
    act(() => document.dispatchEvent(new Event("visibilitychange")))
    await act(async () => vi.advanceTimersByTimeAsync(5_000))
    expect(vi.mocked(api.listNotificationDeliveries).mock.calls.length).toBeGreaterThan(1)
    const count = vi.mocked(api.listNotificationDeliveries).mock.calls.length
    rendered.unmount()
    await act(async () => vi.advanceTimersByTimeAsync(20_000))
    expect(api.listNotificationDeliveries).toHaveBeenCalledTimes(count)
  })
  it("does not show retry or cancel controls for administrators", async () => {
    auth.principal.user.role = "admin"
    vi.mocked(api.getNotificationDelivery).mockResolvedValue(notificationDelivery({ state: "dead" }))
    view(); fireEvent.click(await screen.findByRole("button", { name: "Details" }))
    const detail = await screen.findByRole("dialog")
    await within(detail).findByText("Retries exhausted")
    expect(within(detail).queryByRole("button", { name: "Retry delivery" })).not.toBeInTheDocument()
  })
})
