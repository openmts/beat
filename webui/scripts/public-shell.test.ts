import { runInNewContext } from "node:vm"
import { describe, expect, it, vi } from "vitest"
import { publicWorker } from "./public-worker"
import { publicIcon } from "./public-icons"
type Listener = (event: Record<string, unknown>) => void
function environment() {
 const handlers = new Map<string, Listener>(), entries = new Map<string, Response>(), names = new Map<string, unknown>()
 const cache = { put: vi.fn(async (path: string, response: Response) => { entries.set(path, response) }), match: vi.fn(async (path: string) => entries.get(path)) }
 const caches = { open: vi.fn(async () => cache), keys: vi.fn(async () => [...names.keys()]), delete: vi.fn(async (name: string) => names.delete(name)) }
 const clients: { id: string; url: string }[] = [], skipWaiting = vi.fn(async () => undefined), fetcher = vi.fn(async () => new Response("asset"))
 const self = { location: { origin: "https://beat.example" }, clients: { matchAll: vi.fn(async () => clients) }, skipWaiting, addEventListener: (name: string, listener: Listener) => handlers.set(name, listener) }
 class RelativeRequest extends Request { constructor(path: string, options?: RequestInit) { super(new URL(path, self.location.origin), options) } }
 runInNewContext(publicWorker("v1", ["/assets/app-v1.js", "/assets/shell-v1.html"], "/assets/shell-v1.html"), { self, caches, fetch: fetcher, Request: RelativeRequest, Response, URL, Set, Promise, Error })
 const emit = async (name: string, event: Record<string, unknown> = {}) => { let result: Promise<unknown> | undefined; handlers.get(name)?.({ ...event, waitUntil: (value: Promise<unknown>) => { result = value }, respondWith: (value: Promise<unknown>) => { result = value } }); return result ? await result : undefined }
 return { emit, caches, cache, entries, names, clients, skipWaiting, fetcher }
}
describe("versioned public shell", () => {
 it("emits dimensionally valid versioned PNGs", () => {
  for (const size of [192, 512]) { const icon = publicIcon(size); expect(icon.fileName).toMatch(new RegExp(`assets/beat-${size}-[a-f0-9]{12}.png`)); expect(icon.source.readUInt32BE(16)).toBe(size); expect(icon.source.readUInt32BE(20)).toBe(size); expect(icon.source.subarray(0, 8)).toEqual(Buffer.from([137, 80, 78, 71, 13, 10, 26, 10])) }
 })
 it("installs exact assets anonymously and never forces activation", async () => {
  const env = environment(); await env.emit("install"); expect(env.cache.put).toHaveBeenCalledTimes(2); expect(env.skipWaiting).not.toHaveBeenCalled()
  expect(env.fetcher.mock.calls[0][0]).toMatchObject({ credentials: "omit", cache: "reload" })
  expect(await env.emit("fetch", { request: new Request("https://beat.example/assets/app-v1.js") })).toBeInstanceOf(Response)
  for (const path of ["/api/v1/nodes", "/admin", "/node/a", "/metrics", "/assets/app-v1.js?token=x", "/assets/unknown.js", "https://other.example/assets/app-v1.js"]) expect(await env.emit("fetch", { request: new Request(new URL(path, "https://beat.example")) })).toBeUndefined()
  expect(await env.emit("fetch", { request: new Request("https://beat.example/assets/app-v1.js", { method: "POST" }) })).toBeUndefined()
  expect(await env.emit("fetch", { request: new Request("https://beat.example/assets/app-v1.js", { headers: { Authorization: "Bearer token" } }) })).toBeUndefined()
 })
 it("does not cache unsafe responses and removes failed install caches", async () => {
  for (const headers of [{ "Cache-Control": "no-store" }, { "Set-Cookie": "secret" }]) {
   const env = environment(); env.fetcher.mockResolvedValue(new Response("secret", { headers })); await expect(env.emit("install")).rejects.toThrow("Unsafe public asset"); expect(env.caches.delete).toHaveBeenCalledWith("beat-public-v1"); expect(env.cache.put).not.toHaveBeenCalled()
  }
 })
 it("falls back only on offline public navigation and never caches navigation data", async () => {
  const env = environment(); await env.emit("install"); env.fetcher.mockRejectedValue(new Error("offline"))
  const response = await env.emit("fetch", { request: { method: "GET", url: "https://beat.example/?q=one", mode: "navigate", headers: new Headers() } }) as Response
  expect(await response.text()).toBe("asset"); expect(env.cache.put).toHaveBeenCalledTimes(2)
  env.entries.clear(); const missing = await env.emit("fetch", { request: { method: "GET", url: "https://beat.example/", mode: "navigate", headers: new Headers() } }) as Response
  expect(missing.status).toBe(503)
 })
 it("defers explicit activation when private windows are active and preserves their caches", async () => {
  const env = environment(), source = { id: "public", postMessage: vi.fn() }; env.clients.push({ id: "public", url: "https://beat.example/" }, { id: "private", url: "https://beat.example/admin/operations" })
  await env.emit("message", { data: { type: "BEAT_ACCEPT_UPDATE" }, source }); expect(source.postMessage).toHaveBeenCalledWith({ type: "BEAT_UPDATE_DEFERRED", version: "v1" }); expect(env.skipWaiting).not.toHaveBeenCalled()
  env.names.set("beat-public-old", {}); env.names.set("other-cache", {}); await env.emit("activate"); expect(env.caches.delete).not.toHaveBeenCalled()
  env.clients.pop(); await env.emit("message", { data: { type: "BEAT_ACCEPT_UPDATE" }, source }); expect(env.skipWaiting).toHaveBeenCalledOnce()
  env.clients.length = 0; await env.emit("activate"); expect(env.caches.delete).toHaveBeenCalledWith("beat-public-old"); expect(env.caches.delete).not.toHaveBeenCalledWith("other-cache")
 })
})
