import { describe, expect, it } from "vitest"
import { buildThemeSnapshot } from "@/lib/theme-snapshot"
import { boundedJSON, plainRecord, publicConfiguration, publicID, publicText } from "@/lib/theme-json"
import { defaultSiteSettings } from "@/context/site-settings"
import type { Node, NetworkTaskView } from "@/types"
import type { ThemeSnapshotInput } from "@/types/theme-runtime"

function source(): ThemeSnapshotInput {
  return { route: { kind: "node", node_id: "node" }, nodes: [{ id: "node", name: "Node", group_id: "group", is_public: true, status: "online", tags: [] } as unknown as Node],
    groups: [], site: defaultSiteSettings, locale: "en", theme: "light" }
}
function task(overrides = {}): NetworkTaskView {
  return { task: { id: "task", name: "Public check", type: "http", enabled: true, is_public: true, target: "DO_NOT_SEND", ...overrides },
    nodes: [{ node: { id: "node", name: "N", alias: "" }, latest: { timestamp: "2026-01-01", success: true, latency_ms: 0, status_code: 200, error_code: "DO_NOT_SEND" } },
      { node: { id: "hidden", name: "PRIVATE", alias: "" }, latest: null }] } as NetworkTaskView
}

describe("bounded public JSON", () => {
  it("accepts JSON primitives and rejects non-JSON or inherited objects", () => {
    expect(boundedJSON({ items: [true, false, null, 0, "ok"] }, { depth: 5, keys: 20, bytes: 100 })).toEqual({ items: [true, false, null, 0, "ok"] })
    for (const value of [undefined, () => 1, Infinity, NaN, Object.create({ inherited: true })])
      expect(() => boundedJSON(value, { depth: 5, keys: 20, bytes: 100 })).toThrow("invalid_data")
    expect(plainRecord(Object.create(null))).toBe(true)
  })
  it("bounds keys, key lengths and UTF-8 bytes", () => {
    expect(() => boundedJSON({ a: 1, b: 2 }, { depth: 5, keys: 1, bytes: 100 })).toThrow("snapshot_budget")
    expect(() => boundedJSON({ ["x".repeat(30)]: 1 }, { depth: 5, keys: 5, bytes: 20 })).toThrow("snapshot_budget")
    expect(() => boundedJSON("界界界界", { depth: 5, keys: 5, bytes: 10 })).toThrow("snapshot_budget")
    const cyclic: Record<string, unknown> = {}; cyclic.self = cyclic
    expect(() => boundedJSON(cyclic, { depth: 5, keys: 20, bytes: 100 })).toThrow("snapshot_budget")
  })
  it("rejects credential keys and non-object configuration", () => {
    for (const json of ["[]", "null", "true", '{"AGENT_TOKEN":"secret"}', '{"nested":{"ssh_private_key":"secret"}}'])
      expect(() => publicConfiguration(json)).toThrow("invalid_data")
    expect(() => publicConfiguration(JSON.stringify(Object.fromEntries(Array.from({ length: 257 }, (_, i) => [`key${i}`, true]))))).toThrow("snapshot_budget")
    expect(() => publicConfiguration(JSON.stringify({ label: "界".repeat(6000) }))).toThrow("snapshot_budget")
  })
  it("validates public text and identifiers without coercion", () => {
    expect(publicText(undefined)).toBe(""); expect(publicText(null)).toBe("")
    expect(() => publicText(1)).toThrow("invalid_data")
    expect(() => publicText("a".repeat(8193))).toThrow("snapshot_budget")
    expect(() => publicID("../admin")).toThrow("invalid_data")
  })
})

describe("public snapshot edge cases", () => {
  it("preserves finite traffic, system fields and zero values", () => {
    const input = source()
    input.nodes[0] = { ...input.nodes[0], cpu_model: "CPU", last_seen: "2026-01-01", metrics: { cpu: 0, memory: NaN },
      traffic: { sent: 0, received: 4, used: 4, limit: 0, remaining: null, percentage: null, limit_type: "sum", reset_day: 1, period_start: "start", next_reset: "next", tracked_since: null, status: "unlimited", private_data: "DO_NOT_SEND" } } as Node
    const snapshot = buildThemeSnapshot(input)
    expect(snapshot.nodes[0].metrics).toEqual({ cpu: 0 })
    expect(snapshot.nodes[0].system.cpu_model).toBe("CPU")
    expect(snapshot.nodes[0].traffic).toMatchObject({ sent: 0, remaining: null, tracked_since: null })
    expect(JSON.stringify(snapshot)).not.toContain("DO_NOT_SEND")
  })
  it("rejects duplicate nodes, too many tags/groups and aggregate byte overflow", () => {
    const duplicate = source(); duplicate.nodes.push({ ...duplicate.nodes[0] })
    expect(() => buildThemeSnapshot(duplicate)).toThrow("invalid_data")
    const tags = source(); tags.nodes[0].tags = Array(129).fill("tag")
    expect(() => buildThemeSnapshot(tags)).toThrow("snapshot_budget")
    const large = source(); large.route = { kind: "dashboard" }; large.nodes = Array.from({ length: 150 }, (_, i) => ({ ...large.nodes[0], id: `node-${i}`, public_remark: "x".repeat(8192) }))
    expect(() => buildThemeSnapshot(large)).toThrow("snapshot_budget")
    const groups = source(); groups.route = { kind: "dashboard" }
    groups.nodes = Array.from({ length: 201 }, (_, i) => ({ ...groups.nodes[0], id: `node-${i}`, group_id: `group-${i}` }))
    groups.groups = groups.nodes.map((node) => ({ id: node.group_id, name: "Group", sort_order: 0 }) as ThemeSnapshotInput["groups"][number])
    expect(() => buildThemeSnapshot(groups)).toThrow("snapshot_budget")
  })
  it("only exports current public detail history and named public series", () => {
    const input = source(); input.customMetricNames = ["custom_requests.total"]
    input.history = { from: 0, to: 2, series: { cpu: [{ timestamp: 1, value: 0 }, { timestamp: 2, value: NaN }], "custom_requests.total": [{ timestamp: 1, value: 9 }], private_metric: [{ timestamp: 1, value: 999 }] } }
    const snapshot = buildThemeSnapshot(input)
    expect(snapshot.history).toEqual({ from: 0, to: 2, series: { cpu: [{ timestamp: 1, value: 0 }], "custom_requests.total": [{ timestamp: 1, value: 9 }] } })
    const unloaded = buildThemeSnapshot({ ...input, route: { kind: "node", node_id: "other" } })
    expect(unloaded.nodes).toEqual([]); expect(unloaded.history).toBeUndefined()
  })
  it("preserves explicit history gaps and public aggregation metadata without admitting unknown series", () => {
    const input = source()
    input.historyMetricNames = ["traffic_delta"]
    input.history = { from: 0, to: 2, series: { cpu: [{ timestamp: 0, value: 0 }, { timestamp: 1, value: null }], traffic_delta: [{ timestamp: 1, value: 12 }], private_metric: [{ timestamp: 1, value: 999 }] },
      series_info: { cpu: { aggregation: "max", unit: "percent" }, traffic_delta: { aggregation: "rate", unit: "bytes/s" }, private_metric: { aggregation: "avg", unit: "secret" } } }
    expect(buildThemeSnapshot(input).history).toEqual({ from: 0, to: 2, series: { cpu: [{ timestamp: 0, value: 0 }, { timestamp: 1, value: null }], traffic_delta: [{ timestamp: 1, value: 12 }] },
      series_info: { cpu: { aggregation: "max", unit: "percent" }, traffic_delta: { aggregation: "rate", unit: "bytes/s" } } })
    input.history.series_info!.cpu = { aggregation: "script" as never, unit: "percent" }
    expect(() => buildThemeSnapshot(input)).toThrow("invalid_data")
    input.history.series_info!.cpu = { aggregation: "avg", unit: "" }
    expect(() => buildThemeSnapshot(input)).toThrow("invalid_data")
    input.historyMetricNames = Array(65).fill("custom_one")
    expect(() => buildThemeSnapshot(input)).toThrow("snapshot_budget")
  })
  it("rejects invalid or excessive history instead of truncating it", () => {
    for (const history of [{ from: NaN, to: 2, series: {} }, { from: 3, to: 2, series: {} }, { from: 0, to: 2, series: { cpu: Array(1025).fill({ timestamp: 1, value: 1 }) } }, { from: 0, to: 2, series: { cpu: "bad" } }])
      expect(() => buildThemeSnapshot({ ...source(), history: history as ThemeSnapshotInput["history"] })).toThrow()
    expect(() => buildThemeSnapshot({ ...source(), customMetricNames: Array(65).fill("custom_x"), history: { from: 0, to: 2, series: {} } })).toThrow("snapshot_budget")
    const names = Array.from({ length: 64 }, (_, i) => `custom_${i}`)
    const series = Object.fromEntries([...names, "cpu"].map((name) => [name, []]))
    expect(() => buildThemeSnapshot({ ...source(), customMetricNames: names, history: { from: 0, to: 2, series } })).toThrow("snapshot_budget")
  })
  it("exports only visible public network results with no targets or raw errors", () => {
    const input = source(); input.network = [task(), task({ id: "private-task", is_public: false }), task({ id: "disabled", enabled: false })]
    const snapshot = buildThemeSnapshot(input)
    expect(snapshot.network.tasks).toHaveLength(1)
    expect(snapshot.network.tasks[0]).toMatchObject({ id: "task", nodes: [{ node_id: "node", latest: { latency_ms: 0, status_code: 200 } }] })
    expect(JSON.stringify(snapshot.network)).not.toMatch(/DO_NOT_SEND|hidden|PRIVATE/)
    expect(buildThemeSnapshot({ ...input, site: { ...input.site, show_network_quality: false } }).network).toEqual({ visible: false, tasks: [] })
  })
  it("handles missing/invalid latest values and bounds network tasks", () => {
    const input = source(); const view = task(); view.nodes[0].latest = null; input.network = [view]
    expect(buildThemeSnapshot(input).network.tasks[0]).toMatchObject({ nodes: [{ node_id: "node", latest: null }] })
    view.nodes[0].latest = { timestamp: "now", success: false, latency_ms: Infinity, status_code: NaN, error_code: "private" }
    expect(buildThemeSnapshot(input).network.tasks[0]).toMatchObject({ nodes: [{ latest: { timestamp: "now", success: false } }] })
    input.network = Array.from({ length: 201 }, (_, i) => task({ id: `task-${i}` }))
    expect(() => buildThemeSnapshot(input)).toThrow("snapshot_budget")
  })
})
