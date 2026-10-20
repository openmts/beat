import { publicIdle } from "@/lib/public-pwa-state"
import { act, fireEvent, render, screen, waitFor } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { MemoryRouter } from "react-router"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { PublicPWA } from "./public-pwa"
vi.mock("@/context/locale", async () => {
  const { testLocaleValue } = await import("@/test/catalogs")
  return { useLocale: () => testLocaleValue("en"), useOptionalLocale: () => testLocaleValue("en") }
})
class Worker extends EventTarget { state = "installed"; postMessage = vi.fn() }
let worker: Worker, service: EventTarget & { register: ReturnType<typeof vi.fn>; controller: object | null }, registration: { waiting: Worker | null; installing: Worker | null; onupdatefound: (() => void) | null }
beforeEach(() => {
 vi.stubEnv("PROD", true); Object.defineProperty(window, "isSecureContext", { configurable: true, value: true }); worker = new Worker(); registration = { waiting: worker, installing: null, onupdatefound: null }
 service = Object.assign(new EventTarget(), { register: vi.fn(async () => registration), controller: {} }); Object.defineProperty(navigator, "serviceWorker", { configurable: true, value: service })
})
afterEach(() => { vi.unstubAllEnvs(); vi.restoreAllMocks() })
const view = (path = "/") => render(<MemoryRouter initialEntries={[path]}><PublicPWA /></MemoryRouter>)
describe("public PWA lifecycle UI", () => {
 it("registers only secure public production routes and offers explicit acceptance", async () => {
  view(); await waitFor(() => expect(service.register).toHaveBeenCalledWith("/sw.js", { scope: "/", updateViaCache: "none" }))
  await userEvent.click(await screen.findByRole("button", { name: "Update app" })); expect(worker.postMessage).toHaveBeenCalledWith({ type: "BEAT_ACCEPT_UPDATE" })
  act(() => service.dispatchEvent(new MessageEvent("message", { data: { type: "BEAT_UPDATE_DEFERRED" } }))); expect(screen.getByText(/Close admin/)).toBeInTheDocument()
  await userEvent.click(screen.getByRole("button", { name: "Dismiss" })); expect(screen.queryByText(/Close admin/)).not.toBeInTheDocument()
  await userEvent.click(screen.getByRole("button", { name: "Later" })); expect(screen.queryByRole("button", { name: "Update app" })).not.toBeInTheDocument()
 })
 it("silently degrades on HTTP LAN and avoids admin registration", () => {
  Object.defineProperty(window, "isSecureContext", { configurable: true, value: false }); const first = view(); expect(service.register).not.toHaveBeenCalled(); expect(first.container).not.toHaveTextContent(/unavailable|Install|Update/); first.unmount()
  Object.defineProperty(window, "isSecureContext", { configurable: true, value: true }); view("/admin"); expect(service.register).not.toHaveBeenCalled()
 })
 it("shows install only after browser promotion and respects dismissal", async () => {
  registration.waiting = null; view(); const prompt = vi.fn(async () => undefined)
  const event = Object.assign(new Event("beforeinstallprompt", { cancelable: true }), { prompt, userChoice: Promise.resolve({ outcome: "dismissed" }) })
  act(() => window.dispatchEvent(event)); expect(event.defaultPrevented).toBe(true); fireEvent.click(screen.getByText("App options")); await userEvent.click(screen.getByRole("button", { name: "Install app" })); expect(prompt).toHaveBeenCalledOnce()
  await waitFor(() => expect(screen.queryByText("App options")).not.toBeInTheDocument()); act(() => window.dispatchEvent(event)); expect(screen.queryByText("App options")).not.toBeInTheDocument()
 })
 it("surfaces registration failure and checks focus/selection before offering updates", async () => {
  service.register.mockRejectedValueOnce(new Error("failed")); view(); expect(await screen.findByText("Offline support is temporarily unavailable.")).toBeInTheDocument()
  const field = document.createElement("input"); document.body.append(field); field.focus(); expect(publicIdle()).toBe(false); field.remove(); expect(publicIdle()).toBe(true)
 })
 it("observes an installing worker without activating it automatically", async () => {
  registration.waiting = null; view(); await waitFor(() => expect(service.register).toHaveBeenCalled()); registration.installing = worker
  act(() => { registration.onupdatefound?.(); worker.dispatchEvent(new Event("statechange")) }); expect(await screen.findByText("A new version is ready.")).toBeInTheDocument(); expect(worker.postMessage).not.toHaveBeenCalledWith({ type: "BEAT_ACCEPT_UPDATE" })
 })
})
