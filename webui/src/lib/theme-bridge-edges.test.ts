import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { ThemeBridge } from "@/lib/theme-bridge"
import { ThemeDataError } from "@/lib/theme-json"
import { buildThemeSnapshot } from "@/lib/theme-snapshot"
import { defaultSiteSettings } from "@/context/site-settings"
import type { Node } from "@/types"

const bridges: ThemeBridge[] = []
function setup(snapshot = () => buildThemeSnapshot({ route: { kind: "dashboard" }, site: defaultSiteSettings,
  nodes: [{ id: "node", is_public: true, status: "online" } as Node], groups: [], locale: "en", theme: "light" })) {
  const element = document.createElement("iframe"); document.body.appendChild(element)
  const target = element.contentWindow!
  const post = vi.spyOn(target, "postMessage")
  const ready = vi.fn(), fail = vi.fn(), navigate = vi.fn()
  const bridge = new ThemeBridge({ window: () => target, snapshot, ready, fail, navigate })
  bridges.push(bridge); bridge.start(); bridge.loaded()
  const instance = post.mock.calls[0][0].beat.instance_id
  const send = (type: string, payload?: unknown) => window.dispatchEvent(new MessageEvent("message", {
    source: target, data: { beat: { version: 2, instance_id: instance, type, ...(payload === undefined ? {} : { payload }) } },
  }))
  return { bridge, target, post, ready, fail, navigate, send, instance }
}
beforeEach(() => { vi.useFakeTimers() })
afterEach(() => { bridges.splice(0).forEach((bridge) => bridge.stop()); document.body.replaceChildren(); vi.restoreAllMocks(); vi.useRealTimers() })

describe("theme bridge failure and teardown", () => {
  it("does not navigate before readiness, then permits a visible node", () => {
    const state = setup(); state.send("navigate", { path: "/" }); expect(state.navigate).not.toHaveBeenCalled()
    state.send("ready"); state.send("navigate", { path: "/node/node" }); expect(state.navigate).toHaveBeenCalledWith("/node/node")
    const count = state.post.mock.calls.length; state.send("ready"); expect(state.post.mock.calls).toHaveLength(count)
  })
  it("reports data budgets and invalid-data failures once", () => {
    const budget = setup(() => { throw new ThemeDataError("snapshot_budget") }); budget.send("ready")
    expect(budget.fail).toHaveBeenCalledExactlyOnceWith("snapshot_budget")
    budget.bridge.publish(); budget.bridge.loaded(); vi.advanceTimersByTime(6000); expect(budget.fail).toHaveBeenCalledTimes(1)
    const invalid = setup(() => { throw new Error("DO_NOT_EXPOSE") }); invalid.send("ready")
    expect(invalid.fail).toHaveBeenCalledExactlyOnceWith("invalid_data")
  })
  it("rejects malformed messages from the actual frame", () => {
    const state = setup()
    window.dispatchEvent(new MessageEvent("message", { source: state.target, data: "invalid" }))
    expect(state.fail).toHaveBeenCalledWith("protocol_error")
  })
  it("bounds reload loops and stops publishing after teardown", () => {
    const state = setup(); state.send("ready"); state.bridge.loaded(); state.bridge.loaded(); state.bridge.loaded()
    expect(state.fail).toHaveBeenCalledWith("runtime_error")
    const normal = setup(); normal.send("ready"); const count = normal.post.mock.calls.length
    normal.bridge.stop(); vi.advanceTimersByTime(15000); expect(normal.post.mock.calls).toHaveLength(count)
  })
  it("handles browser postMessage errors without leaking thrown data", () => {
    const state = setup(); state.post.mockImplementation(() => { throw new Error("PRIVATE_BROWSER_ERROR") })
    state.bridge.loaded(); expect(state.fail).toHaveBeenCalledWith("invalid_data")
  })
  it("handles a detached window and missing secure randomness", () => {
    const failure = vi.fn()
    const detached = new ThemeBridge({ window: () => null, snapshot: () => { throw new Error("unused") }, navigate: vi.fn(), ready: vi.fn(), fail: failure })
    bridges.push(detached); detached.start(); detached.loaded(); detached.publish(); vi.advanceTimersByTime(5001)
    expect(failure).toHaveBeenCalledWith("timeout")
    const state = setup(); vi.spyOn(window.crypto, "getRandomValues").mockImplementation(() => { throw new Error("randomness unavailable") })
    state.bridge.loaded(); expect(state.fail).toHaveBeenCalledWith("invalid_data")
  })
})
