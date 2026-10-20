import { describe, expect, it } from "vitest"
import { workspaceNode } from "@/test/metric-workspace-fixtures"
import { countryName, fleetMatches, fleetName, fleetTotals, publicFleetNode, selectFleet } from "./fleet-selectors"
import { defaultFleetControls } from "./fleet-state"
import type { FleetControls } from "./fleet-state"
import type { Group, Node } from "@/types"
const node = (id: string, input: Partial<Node> = {}): Node => ({ ...workspaceNode(id), name: id, is_public: true, group_id: "g", status: "online", sort_order: 0, telemetry_state: "fresh", ...input })
const groups = [{ id: "g", sort_order: 2 }, { id: "first", sort_order: 1 }] as Group[]
const selected = (nodes: Node[], input: Partial<FleetControls> = {}) => selectFleet(nodes, groups, { ...defaultFleetControls, ...input }, "en").map(value => value.id)
describe("public fleet selectors", () => {
 it("searches only permitted presentation fields", () => {
  const value = node("private-id", { name: "internal name", alias: "Visible", public_remark: "remark", tags: ["edge"], host: "secret-host", os: "linux", platform: "debian", arch: "arm64", location: { scope: "public", country_code: "JP" } })
  for (const search of ["Visible", "remark", "edge", "linux", "debian", "arm64", "Japan"]) expect(fleetMatches(value, { ...defaultFleetControls, search }, "en")).toBe(true)
  for (const search of ["private-id", "secret-host", "internal name"]) expect(fleetMatches(value, { ...defaultFleetControls, search }, "en")).toBe(false)
  expect(fleetName(node("Name", { alias: "" }))).toBe("Name"); expect(countryName("invalid", "en")).toBe(""); expect(countryName("JP", "invalid_locale")).toBe("JP")
 })
 it("enforces visibility and combined filters", () => {
  const value = node("one", { tags: ["edge"], location: { scope: "public", country_code: "JP" }, lifecycle: { status: "due_today", days_remaining: 0 } })
  expect(selected([value], { groups: ["g"], tags: ["edge"], country: "JP", status: "online", freshness: "fresh", expiry: "today" })).toEqual(["one"])
  for (const input of [{ groups: ["other"] }, { tags: ["missing"] }, { country: "US" }, { status: "offline" }, { freshness: "stale" }, { expiry: "expired" }, { expiry: "untracked" }]) expect(selected([value], input)).toEqual([])
  expect(selected([node("hidden", { is_public: false })])).toEqual([])
  expect(selected([value], { expiry: "7" })).toEqual(["one"])
  expect(selected([node("other")], { expiry: "30" })).toEqual([])
 })
 it("keeps group order primary, offline explicit, and stable ties", () => {
  const a = node("a", { sort_order: 2 }), b = node("b", { status: "offline", sort_order: 1 }), c = node("c", { group_id: "first", sort_order: 100 })
  expect(selected([a, b, c])).toEqual(["c", "a", "b"])
  expect(selected([a, b], { offline: "first" })).toEqual(["b", "a"])
  expect(selected([b, a], { sort: "name", direction: "desc", offline: "keep" })).toEqual(["b", "a"])
  expect(selected([b, a], { sort: "status", offline: "keep" })).toEqual(["a", "b"])
  expect(selected([node("z"), node("a")])).toEqual(["a", "z"])
 })
 it("sorts each numeric field with unknown values last in both directions", () => {
  for (const sort of ["cpu", "memory", "disk", "net_sent", "net_recv"] as const) {
   const values = [node("a", { metrics: { [sort]: 10 } }), node("b", { metrics: { [sort]: 20 } }), node("unknown", { telemetry_state: "stale", metrics: { [sort]: 100 } })]
   expect(selected(values, { sort })).toEqual(["a", "b", "unknown"])
   expect(selected(values, { sort, direction: "desc" })).toEqual(["b", "a", "unknown"])
  }
  expect(selected([node("z"), node("a")], { sort: "cpu" })).toEqual(["a", "z"])
  expect(selected([node("later", { lifecycle: { expires_on: "2030-01-01" } }), node("none"), node("early", { lifecycle: { expires_on: "2025-01-01" } })], { sort: "expiry" })).toEqual(["early", "later", "none"])
 })
 it("strips private metadata and reports measured aggregate coverage", () => {
  const value = node("a", { host: "private-host", port: 22, ssh_public_key: "secret-key", metrics: { cpu: 10, net_sent: 20, net_recv: Number.NaN }, lifecycle: { price_micros: "9223372036854775807" }, location: { scope: "public", country_code: "JP" } })
  const safe = publicFleetNode({ ...value, secret: "not public", location: { ...value.location, source_ip: "private" } } as Node, false)
  expect(safe.host).toBe(""); expect(safe.port).toBe(0); expect(safe.ssh_public_key).toBe(""); expect(safe).not.toHaveProperty("secret"); expect(safe.location).not.toHaveProperty("source_ip"); expect(safe.metrics).not.toHaveProperty("net_recv")
  expect(publicFleetNode(value, true).host).toBe("private-host")
  expect(fleetTotals([value, node("b", { status: "offline", telemetry_state: "stale", metrics: { net_sent: 999 } })])).toMatchObject({ total: 2, online: 1, countries: 1, upload: { value: 20, covered: 1 }, download: { value: null, covered: 0 } })
 })
})
