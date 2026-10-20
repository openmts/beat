import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { PublicNodePoller } from "@/lib/public-node-poller"
import { getPublicNodeSummary } from "@/lib/public-workspace-api"
import { workspaceNode } from "@/test/metric-workspace-fixtures"
import { APIError } from "@/lib/api-error"
import type { Node } from "@/types"

vi.mock("@/lib/public-workspace-api")
describe("bounded public node summary polling", () => {
  beforeEach(() => { vi.useFakeTimers(); vi.resetAllMocks(); Object.defineProperty(document, "hidden", { configurable: true, value: false }) })
  afterEach(() => { vi.useRealTimers() })
  it("does not overlap slow requests and aborts hidden/unmounted work", async () => {
    const replies: ((node: Node) => void)[] = []
    vi.mocked(getPublicNodeSummary).mockImplementation(() => new Promise((resolve) => replies.push(resolve)))
    const update = vi.fn()
    const stop = new PublicNodePoller({ nodeId: "node-one", refreshSeconds: 1, update }).start()
    await vi.advanceTimersByTimeAsync(60_000)
    expect(getPublicNodeSummary).toHaveBeenCalledOnce()
    const first = vi.mocked(getPublicNodeSummary).mock.calls[0][0].signal
    Object.defineProperty(document, "hidden", { configurable: true, value: true })
    document.dispatchEvent(new Event("visibilitychange"))
    expect(first.aborted).toBe(true)
    Object.defineProperty(document, "hidden", { configurable: true, value: false })
    document.dispatchEvent(new Event("visibilitychange"))
    expect(getPublicNodeSummary).toHaveBeenCalledTimes(2)
    replies[0](workspaceNode("stale"))
    replies[1](workspaceNode())
    await vi.advanceTimersByTimeAsync(0)
    expect(update.mock.calls.filter(([event]) => event.type === "ready").map(([event]) => event.node.id)).toEqual(["node-one"])
    await vi.advanceTimersByTimeAsync(4999)
    expect(getPublicNodeSummary).toHaveBeenCalledTimes(2)
    await vi.advanceTimersByTimeAsync(1)
    expect(getPublicNodeSummary).toHaveBeenCalledTimes(3)
    stop()
    expect(vi.mocked(getPublicNodeSummary).mock.calls[2][0].signal.aborted).toBe(true)
    replies[2](workspaceNode("late"))
    await vi.advanceTimersByTimeAsync(0)
    expect(update.mock.calls.at(-1)?.[0].type).not.toBe("ready")
  })
  it("loads once when a paused page becomes visible and distinguishes unavailable nodes", async () => {
    Object.defineProperty(document, "hidden", { configurable: true, value: true })
    const update = vi.fn()
    vi.mocked(getPublicNodeSummary).mockRejectedValue(new APIError("missing", 404))
    const stop = new PublicNodePoller({ nodeId: "node-one", refreshSeconds: 0, update }).start()
    expect(getPublicNodeSummary).not.toHaveBeenCalled()
    Object.defineProperty(document, "hidden", { configurable: true, value: false })
    document.dispatchEvent(new Event("visibilitychange"))
    await vi.advanceTimersByTimeAsync(0)
    expect(update).toHaveBeenLastCalledWith({ type: "error", unavailable: true })
    await vi.advanceTimersByTimeAsync(30_000)
    expect(getPublicNodeSummary).toHaveBeenCalledOnce()
    stop()
  })
})
