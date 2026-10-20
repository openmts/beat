import { afterEach, describe, expect, it, vi } from "vitest"
import { workspaceNode } from "@/test/metric-workspace-fixtures"
import { readPublicFleet, validateFleetCollection } from "./public-fleet-api"
import type { Group, Node } from "@/types"
afterEach(() => vi.unstubAllGlobals())
const groups = [{ id: "group-one", name: "Visible" }, { id: "hidden", name: "Hidden" }] as Group[]
const fetchBody = (body: unknown, status = 200) => new Response(JSON.stringify(body), { status, headers: { Date: "Thu, 01 Oct 2026 00:00:00 GMT" } })
describe("public fleet REST bounds", () => {
 it("uses anonymous no-store fetch and removes hidden nodes and empty groups", async () => {
  const fetcher = vi.fn().mockResolvedValueOnce(fetchBody({ data: [workspaceNode(), { ...workspaceNode("hidden"), is_public: false }] })).mockResolvedValueOnce(fetchBody(groups))
  vi.stubGlobal("fetch", fetcher)
  const result = await readPublicFleet(new AbortController().signal)
  expect(result.nodes).toHaveLength(1); expect(result.groups).toHaveLength(1); expect(result.serverTime).toBe(Date.parse("2026-10-01T00:00:00Z"))
  expect(fetcher).toHaveBeenCalledWith("/api/v1/nodes", expect.objectContaining({ credentials: "omit", cache: "no-store" }))
 })
 it("classifies denied, network failures and absent streams", async () => {
  for (const [response, kind] of [[fetchBody({}, 403), "denied"], [fetchBody({}, 500), "unavailable"], [new Response(null), "unavailable"]] as const) {
   vi.stubGlobal("fetch", vi.fn().mockResolvedValue(response)); await expect(readPublicFleet(new AbortController().signal)).rejects.toMatchObject({ kind })
  }
 })
 it("rejects capacity overflow without truncation, duplicate IDs, and invalid status", () => {
  for (const [nodes, list] of [[Array(1001).fill(workspaceNode()), groups], [[workspaceNode()], Array(201).fill(groups[0])], [[{ ...workspaceNode(), tags: Array(13).fill("tag") }], groups], [[{ ...workspaceNode(), metrics: Object.fromEntries(Array.from({ length: 129 }, (_, i) => [String(i), i])) }], groups]]) expect(() => validateFleetCollection(nodes as Node[], list as Group[])).toThrow("capacity")
  expect(() => validateFleetCollection([workspaceNode(), workspaceNode()], groups)).toThrow("unavailable")
  expect(() => validateFleetCollection([workspaceNode()], [groups[0], groups[0]])).toThrow("unavailable")
  expect(() => validateFleetCollection([{ ...workspaceNode(), status: "other" } as unknown as Node], groups)).toThrow("unavailable")
  expect(() => validateFleetCollection(null as unknown as Node[], groups)).toThrow("capacity")
 })
 it("cancels a response larger than the byte limit and tolerates cancellation failure", async () => {
  const reader = { read: vi.fn().mockResolvedValue({ done: false, value: new Uint8Array(8 * 1024 * 1024 + 1) }), cancel: vi.fn().mockRejectedValue(new Error("cancel")), releaseLock: vi.fn() }
  vi.stubGlobal("fetch", vi.fn().mockResolvedValue({ ok: true, body: { getReader: () => reader }, headers: new Headers() }))
  await expect(readPublicFleet(new AbortController().signal)).rejects.toMatchObject({ kind: "capacity" }); expect(reader.cancel).toHaveBeenCalled(); expect(reader.releaseLock).toHaveBeenCalled()
 })
 it("handles missing response dates", async () => {
  vi.stubGlobal("fetch", vi.fn().mockResolvedValueOnce(new Response(JSON.stringify([workspaceNode()]))).mockResolvedValueOnce(new Response(JSON.stringify(groups))))
  expect((await readPublicFleet(new AbortController().signal)).serverTime).toBeNull()
 })
})
