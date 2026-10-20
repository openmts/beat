import { act, fireEvent, render, screen } from "@testing-library/react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { MemoryRouter, useLocation } from "react-router"
import { LocaleProvider } from "@/context/locale"
import { SandboxedThemeFrame } from "@/components/sandboxed-theme-frame"
import { buildThemeSnapshot } from "@/lib/theme-snapshot"
import { defaultSiteSettings } from "@/context/site-settings"
import type { ActiveThemeDescriptor } from "@/types/theme"

const descriptor: ActiveThemeDescriptor = { package_id: "package", theme_id: "theme", name: "Sandbox", version: "1.0.0",
  runtime: "sandboxed", built_in: false, protocol: 2, sandbox_entry: "dist/index.html", archive_digest: "a".repeat(64) }
const snapshot = () => buildThemeSnapshot({ route: { kind: "dashboard" }, site: defaultSiteSettings, nodes: [], groups: [], locale: "en", theme: "light" })
const fallback = <div>built-in-dashboard</div>
function Location() { return <output data-testid="location">{useLocation().pathname}</output> }
function tree(props = {}) {
  return <LocaleProvider><MemoryRouter><Location /><SandboxedThemeFrame descriptor={descriptor} snapshot={snapshot} fallback={fallback} {...props} /></MemoryRouter></LocaleProvider>
}
function boot() {
  const frame = screen.getByTitle("Sandbox") as HTMLIFrameElement
  const send = vi.spyOn(frame.contentWindow!, "postMessage")
  fireEvent.load(frame)
  const instance = (send.mock.calls[0][0] as { beat: { instance_id: string } }).beat.instance_id
  const receive = (type: string, payload?: unknown, instanceID = instance) => act(() => window.dispatchEvent(new MessageEvent("message", {
    source: frame.contentWindow, data: { beat: { version: 2, instance_id: instanceID, type, ...(payload === undefined ? {} : { payload }) } },
  })))
  return { frame, send, instance, receive }
}

describe("SandboxedThemeFrame protocol 2 lifecycle", () => {
  beforeEach(() => { localStorage.clear(); vi.useFakeTimers() })
  afterEach(() => { vi.useRealTimers() })
  it("handshakes per load and sends snapshots to the opaque origin", () => {
    render(tree())
    const { frame, send, receive } = boot()
    expect(frame.src).toContain(`/themes/assets/${descriptor.archive_digest}/dist/index.html`)
    expect(frame.getAttribute("sandbox")).toBe("allow-scripts")
    expect(send.mock.calls[0][1]).toBe("*")
    expect(send.mock.calls[0][0].beat.type).toBe("init")
    receive("ready")
    expect(send.mock.calls[1][0].beat.type).toBe("snapshot")
    expect(send.mock.calls[1][1]).toBe("*")
    expect(screen.queryByText("built-in-dashboard")).not.toBeInTheDocument()
    act(() => vi.advanceTimersByTime(5000))
    expect(send.mock.calls[2][0].beat.type).toBe("snapshot")
  })
  it("keeps heartbeats when parent callbacks change and rejects stale instances", () => {
    const view = render(tree())
    const { frame, send, receive, instance } = boot()
    receive("ready", undefined, "wrong-instance")
    expect(screen.getByText("built-in-dashboard")).toBeInTheDocument()
    receive("ready")
    act(() => vi.advanceTimersByTime(3000))
    view.rerender(tree({ snapshot: () => snapshot() }))
    const count = send.mock.calls.length
    act(() => vi.advanceTimersByTime(2000))
    expect(send.mock.calls.length).toBe(count + 1)
    fireEvent.load(frame)
    expect(send.mock.lastCall?.[0].beat.type).toBe("init")
    expect(send.mock.lastCall?.[0].beat.instance_id).not.toBe(instance)
    receive("ready")
    expect(screen.getByText("built-in-dashboard")).toBeInTheDocument()
  })
  it("uses router navigation only for public targets", () => {
    render(tree())
    const { receive } = boot(); receive("ready"); receive("navigate", { path: "/" })
    expect(screen.getByTestId("location")).toHaveTextContent("/")
    receive("navigate", { path: "/admin" })
    expect(screen.getByTestId("location")).toHaveTextContent("/")
    expect(screen.getByText("built-in-dashboard")).toBeInTheDocument()
  })
  it("falls back after runtime error, timeout or invalid snapshot", () => {
    const onFallback = vi.fn()
    const view = render(tree({ onFallback }))
    const { receive } = boot(); receive("ready"); receive("error", { code: "render_failed" })
    expect(screen.getByText("built-in-dashboard")).toBeInTheDocument()
    expect(onFallback).toHaveBeenCalledWith("runtime_error")
    view.unmount()
    render(tree({ onFallback }))
    act(() => vi.advanceTimersByTime(5001))
    expect(onFallback).toHaveBeenCalledWith("timeout")
  })
  it("does not accept another window's messages", () => {
    render(tree()); const { instance } = boot()
    act(() => window.dispatchEvent(new MessageEvent("message", { source: window, data: { beat: { version: 2, instance_id: instance, type: "ready" } } })))
    expect(screen.getByText("built-in-dashboard")).toBeInTheDocument()
  })
  it("falls back for unsupported descriptors and preserves declarative built-in rendering", () => {
    const view = render(tree({ descriptor: { ...descriptor, protocol: 1 } }))
    expect(screen.queryByTitle("Sandbox")).not.toBeInTheDocument()
    expect(screen.getByText("built-in-dashboard")).toBeInTheDocument()
    view.rerender(tree({ descriptor: { ...descriptor, sandbox_entry: "../outside.html" } }))
    expect(screen.queryByTitle("Sandbox")).not.toBeInTheDocument()
    view.rerender(tree({ descriptor: { ...descriptor, runtime: "declarative" } }))
    expect(screen.getByText("built-in-dashboard")).toBeInTheDocument()
  })

})
