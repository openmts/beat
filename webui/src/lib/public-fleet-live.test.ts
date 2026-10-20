import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { workspaceNode } from "@/test/metric-workspace-fixtures"
import { PublicFleetLive, mergeLiveNode, type PublicFleetSnapshot } from "./public-fleet-live"
import { FleetReadError } from "./public-fleet-api"
import type { Node } from "@/types"
class Socket {
 onopen: (() => void) | null = null; onclose: (() => void) | null = null; onerror: (() => void) | null = null; onmessage: ((event: { data: unknown }) => void) | null = null
 close = vi.fn(() => this.onclose?.())
 message(nodes: Node[], timestamp = new Date(Date.now() + 1000).toISOString()) { this.onmessage?.({ data: JSON.stringify({ nodes, timestamp }) }) }
}
const tick = async () => { await Promise.resolve(); await Promise.resolve() }
let stop: (() => void) | undefined
beforeEach(() => { vi.useFakeTimers(); vi.setSystemTime(new Date("2026-10-01T00:00:00Z")); Object.defineProperty(document, "hidden", { configurable: true, value: false }); Object.defineProperty(navigator, "onLine", { configurable: true, value: true }) })
afterEach(() => { stop?.(); stop = undefined; vi.useRealTimers(); vi.restoreAllMocks() })
function fixture() {
 const initial = { ...workspaceNode("a"), metrics: { cpu: 1 }, telemetry_state: "fresh" as const }
 const read = vi.fn(async (_signal: AbortSignal) => ({ nodes: [initial], groups: [], serverTime: Date.now() }))
 const sockets: Socket[] = [], states: PublicFleetSnapshot[] = []
 const live = new PublicFleetLive({ read, socket: () => { const socket = new Socket(); sockets.push(socket); return socket as unknown as WebSocket }, publish: state => states.push(state) })
 stop = live.start()
 return { initial, read, sockets, states, live, current: () => states.at(-1)! }
}
describe("bounded public live transport", () => {
 it("reconciles once and only merges known public IDs, preserving REST metadata", async () => {
  const f = fixture(); await tick(); expect(f.sockets).toHaveLength(1); f.sockets[0].onopen?.()
  f.sockets[0].message([{ ...f.initial, name: "injected", is_public: true, metrics: { cpu: 50 } }, workspaceNode("unknown")])
  expect(f.current().nodes).toHaveLength(1); expect(f.current().nodes[0].name).toBe(f.initial.name); expect(f.current().nodes[0].metrics?.cpu).toBe(50)
  const previous = f.current().receivedAt
  f.sockets[0].message([{ ...f.initial, is_public: false, metrics: { cpu: 99 } }], "2100-01-01T00:00:00Z")
  f.sockets[0].message([workspaceNode("unknown")], "2101-01-01T00:00:00Z")
  expect(f.current().receivedAt).toBe(previous)
  f.sockets[0].message([{ ...f.initial, metrics: { cpu: 60 } }], new Date(Date.now() + 2000).toISOString())
  expect(f.current().nodes[0].metrics?.cpu).toBe(60)
  f.sockets[0].message([{ ...f.initial, metrics: { cpu: 70 } }], new Date(Date.now() + 2000).toISOString())
  expect(f.current().nodes[0].metrics?.cpu).toBe(60)
 })
 it("ignores malformed, duplicate, oversized and nontext frames", async () => {
  const f = fixture(); await tick(); const socket = f.sockets[0]
  for (const data of [null, "bad", "x".repeat(8 * 1024 * 1024 + 1), JSON.stringify({ timestamp: "invalid", nodes: [] }), JSON.stringify({ timestamp: "2100-01-01", nodes: [null] })]) socket.onmessage?.({ data })
  socket.message([f.initial, f.initial]); socket.message(Array.from({ length: 1001 }, (_, i) => workspaceNode(String(i))))
  expect(f.current().nodes[0].metrics?.cpu).toBe(1)
 })
 it("does not let a pending REST response overwrite newer live metrics", async () => {
  const f = fixture(); await tick()
  let resolve!: (value: Awaited<ReturnType<typeof f.read>>) => void
  f.read.mockImplementationOnce(() => new Promise(done => { resolve = done }))
  const request = f.live.refresh(); f.sockets[0].message([{ ...f.initial, metrics: { cpu: 77 } }])
  resolve({ nodes: [{ ...f.initial, name: "REST name", metrics: { cpu: 5 } }], groups: [], serverTime: Date.now() }); await request
  expect(f.current().nodes[0]).toMatchObject({ name: "REST name", metrics: { cpu: 77 } }); expect(f.sockets).toHaveLength(1)
 })
 it("clears memory offline, pauses hidden and resumes one stream", async () => {
  const f = fixture(); await tick()
  Object.defineProperty(navigator, "onLine", { configurable: true, value: false }); window.dispatchEvent(new Event("offline"))
  expect(f.current()).toMatchObject({ offline: true, nodes: [], groups: [] }); expect(f.sockets[0].close).toHaveBeenCalledOnce()
  await vi.advanceTimersByTimeAsync(60000); expect(f.read).toHaveBeenCalledTimes(1)
  Object.defineProperty(navigator, "onLine", { configurable: true, value: true }); window.dispatchEvent(new Event("online")); await tick(); expect(f.sockets).toHaveLength(2)
  Object.defineProperty(document, "hidden", { configurable: true, value: true }); document.dispatchEvent(new Event("visibilitychange")); await vi.advanceTimersByTimeAsync(60000); expect(f.read).toHaveBeenCalledTimes(2)
  Object.defineProperty(document, "hidden", { configurable: true, value: false }); document.dispatchEvent(new Event("visibilitychange")); await tick(); expect(f.sockets).toHaveLength(3)
 })
 it("reconnects with bounded retry and reconciles after the next opening", async () => {
  const f = fixture(); await tick(); f.sockets[0].onopen?.(); f.sockets[0].onerror?.()
  expect(f.current().connected).toBe(false); await vi.advanceTimersByTimeAsync(1250); expect(f.sockets).toHaveLength(2)
  f.sockets[1].onopen?.(); await tick(); expect(f.read).toHaveBeenCalledTimes(2)
  expect(f.current().connected).toBe(true)
 })
 it("shows failures/staleness and clears denied data without accepting stale requests", async () => {
  const f = fixture(); await tick(); f.read.mockRejectedValueOnce(new Error("down")); await f.live.refresh()
  expect(f.current()).toMatchObject({ error: "unavailable", stale: true }); expect(f.current().nodes).toHaveLength(1)
  f.read.mockRejectedValueOnce(new FleetReadError("denied")); await f.live.refresh(); expect(f.current().nodes).toEqual([])
  f.read.mockRejectedValue(new FleetReadError("capacity")); await vi.advanceTimersByTimeAsync(61000); expect(f.current().stale).toBe(true)
  stop?.(); stop = undefined; const count = f.states.length; await f.live.refresh(); expect(f.states).toHaveLength(count)
 })
 it("handles connection construction failures and unavailable optional live fields", async () => {
  const states: PublicFleetSnapshot[] = [], read = vi.fn(async () => ({ nodes: [workspaceNode()], groups: [], serverTime: null }))
  const socket = vi.fn(() => { throw new Error("socket") }), live = new PublicFleetLive({ publish: state => states.push(state), read, socket })
  stop = live.start(); await tick(); await vi.advanceTimersByTimeAsync(1250); expect(socket).toHaveBeenCalledTimes(2)
  expect(mergeLiveNode({ ...workspaceNode(), metrics: { cpu: 1 } }, { ...workspaceNode(), status: "invalid" } as unknown as Node)).toMatchObject({ status: "online", metrics: { cpu: 1 }, telemetry_state: "unavailable" })
 })
})
