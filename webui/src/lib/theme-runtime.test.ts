import { describe, expect, it } from "vitest"
import { buildThemeSnapshot } from "@/lib/theme-snapshot"
import { frameMessage, themeAssetURL } from "@/lib/theme-protocol"
import { defaultSiteSettings } from "@/context/site-settings"
import type { Node } from "@/types"
import type { ThemeSnapshotInput } from "@/types/theme-runtime"

const node = { id: "visible-node", name: "Public", alias: "Alias", group_id: "group", host: "192.0.2.9", status: "online",
  is_public: true, public_remark: "Public note", tags: ["production"], metrics: { cpu: 7, private_metric: 99, memory: Infinity },
  ssh_public_key: "DO_NOT_SEND", private_remark: "DO_NOT_SEND", agent_token: "DO_NOT_SEND", kernel: "6.8", port: 22 } as unknown as Node
function input(): ThemeSnapshotInput {
  return { route: { kind: "dashboard" }, site: { ...defaultSiteSettings, public_address_mode: "hidden" },
    nodes: [node, { ...node, id: "hidden", is_public: false }], groups: [{ id: "group", name: "Public group", sort_order: 0 } as ThemeSnapshotInput["groups"][number]],
    locale: "zh-CN", theme: "dark", configuration: '{"title":"Public theme"}', state: { nodes: "ready" } }
}

describe("theme public snapshot", () => {
  it("exposes only public whitelisted fields and finite metrics", () => {
    const snapshot = buildThemeSnapshot(input())
    expect(snapshot.nodes).toHaveLength(1)
    expect(snapshot.nodes[0].metrics).toEqual({ cpu: 7 })
    expect(snapshot.nodes[0]).not.toHaveProperty("host")
    expect(JSON.stringify(snapshot)).not.toContain("DO_NOT_SEND")
    expect(snapshot.configuration).toEqual({ title: "Public theme" })
    expect(snapshot.locale).toBe("zh-CN")
    expect(snapshot.theme).toBe("dark")
  })
  it("exposes host only when public IP display is enabled", () => {
    const source = input(); source.site.public_address_mode = "full"
    expect(buildThemeSnapshot(source).nodes[0].host).toBe("192.0.2.9")
  })
  it("exports only valid public country codes to themes", () => {
    const source = input()
    const withCountry = (country_code?: string, scope = "public") => ({ ...node, location: { scope, ...(country_code === undefined ? {} : { country_code }) } })
    source.nodes = [withCountry("JP"), { ...withCountry("US"), id: "hidden-country", is_public: false }]
    expect(buildThemeSnapshot(source).nodes[0].country_code).toBe("JP")
    for (const location of [withCountry("jp"), withCountry("JPN"), withCountry("JP", "private"), withCountry(undefined)]) {
      source.nodes = [location]
      expect(buildThemeSnapshot(source).nodes[0]).not.toHaveProperty("country_code")
    }
  })
  it("rejects invalid, deep, oversized or prototype-bearing configuration", () => {
    for (const configuration of ["not JSON", '{"__proto__":{}}', '{"safe":{"constructor":1}}', '{"a":{"b":{"c":{"d":{"e":{"f":1}}}}}}', JSON.stringify({ title: "x".repeat(20000) })])
      expect(() => buildThemeSnapshot({ ...input(), configuration })).toThrow()
  })
  it("never silently truncates a large visible fleet", () => {
    const source = input(); source.nodes = Array.from({ length: 1001 }, (_, index) => ({ ...node, id: `node-${index}` }))
    expect(() => buildThemeSnapshot(source)).toThrow("snapshot_budget")
  })
  it("does not expose a hidden detail ID or its history while retaining the public list", () => {
    const source = input(); source.route = { kind: "node", node_id: "hidden" }; source.history = { from: 1, to: 2, series: { cpu: [{ timestamp: 1, value: 90 }] } }
    const snapshot = buildThemeSnapshot(source)
    expect(snapshot.route).not.toHaveProperty("node_id")
    expect(snapshot).not.toHaveProperty("history")
    expect(snapshot.nodes.map((entry) => entry.id)).toEqual(["visible-node"])
  })
  it("publishes the active range and relative options for visible details", () => {
    const source = input(); source.route = { kind: "node", node_id: "visible-node" }
    source.range = { mode: "relative", seconds: 21600 }
    expect(buildThemeSnapshot(source)).toMatchObject({ range: { mode: "relative", seconds: 21600 },
      range_options: [300, 900, 3600, 21600, 86400, 604800, 2592000] })
    source.range = { mode: "absolute", from: "2026-11-07T10:00:00Z", to: "2026-11-07T16:00:00Z" }
    expect(buildThemeSnapshot(source).range).toEqual({ mode: "absolute", from: "2026-11-07T10:00:00.000Z", to: "2026-11-07T16:00:00.000Z" })
    source.range = { mode: "relative", seconds: 0 }
    expect(() => buildThemeSnapshot(source)).toThrow("invalid_data")
  })
})

describe("theme protocol 3", () => {
  const source = {}
  const message = (type: string, payload?: unknown) => ({ beat: { version: 3, instance_id: "instance", type, ...(payload === undefined ? {} : { payload }) } })
  it("binds ready to the frame and current instance", () => {
    expect(frameMessage({ source, data: message("ready") }, { window: source, instance: "instance" }, new Set())).toEqual({ type: "ready" })
    expect(frameMessage({ source: {}, data: message("ready") }, { window: source, instance: "instance" }, new Set())).toBeNull()
    expect(frameMessage({ source, data: message("ready") }, { window: source, instance: "old" }, new Set())).toBeNull()
  })
  it("allows only the homepage or an actually visible node", () => {
    const frame = { window: source, instance: "instance" }
    expect(frameMessage({ source, data: message("navigate", { path: "/node/visible-node" }) }, frame, new Set(["visible-node"]))?.type).toBe("navigate")
    for (const path of ["/admin", "/node/hidden", "//outside.example", "/node/visible-node?admin=1", "#/node/visible-node"])
      expect(frameMessage({ source, data: message("navigate", { path }) }, frame, new Set(["visible-node"]))).toBeNull()
  })
  it("rejects extra fields, wrong versions and excessive payloads", () => {
    for (const data of [null, [], { beat: { version: 2, instance_id: "instance", type: "ready" } }, { ...message("ready"), secret: 1 }, message("ready", {}), message("error", { code: "unknown" }), message("error", { code: "x".repeat(20000) })])
      expect(frameMessage({ source, data }, { window: source, instance: "instance" }, new Set())).toBeNull()
  })
  it("accepts only allowlisted relative and bounded absolute ranges", () => {
    const frame = { window: source, instance: "instance" }
    const range = (payload: unknown) => frameMessage({ source, data: message("setRange", payload) }, frame, new Set())
    expect(range({ mode: "relative", seconds: 21600 })).toEqual({ type: "setRange", payload: { mode: "relative", seconds: 21600 } })
    for (const seconds of [299, 123, 0, -1, 3600.5, 2592001, "3600"]) expect(range({ mode: "relative", seconds })).toBeNull()
    expect(range({ mode: "absolute", from: "2026-11-07T10:00:00Z", to: "2026-11-07T16:00:00Z" }))
      .toEqual({ type: "setRange", payload: { mode: "absolute", from: "2026-11-07T10:00:00.000Z", to: "2026-11-07T16:00:00.000Z" } })
    for (const payload of [
      { mode: "absolute", from: "invalid", to: "2026-11-07T16:00:00Z" },
      { mode: "absolute", from: "2026-11-07T16:00:00Z", to: "2026-11-07T10:00:00Z" },
      { mode: "absolute", from: "2026-01-01T00:00:00Z", to: "2026-02-01T00:00:01Z" },
      { mode: "absolute", from: "2026-11-07T10:00:00Z", to: "2026-11-07T16:00:00Z", extra: true },
      { mode: "unknown", seconds: 3600 },
      { mode: "relative", seconds: 3600, extra: true },
    ]) expect(range(payload)).toBeNull()
  })
  it("preserves nested entry directories", () => {
    expect(themeAssetURL("a".repeat(64), "dist/theme entry.html")).toBe(`/themes/assets/${"a".repeat(64)}/dist/theme%20entry.html`)
    expect(() => themeAssetURL("bad", "../index.html")).toThrow()
    for (const path of ["../index.html", "bad\\index.html", "bad:index.html", "bad\nindex.html", ""])
      expect(() => themeAssetURL("a".repeat(64), path)).toThrow()
  })
})
