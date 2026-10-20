import { publicAccelerators } from "@/lib/accelerator-public"
import type { Group, Node } from "@/types"
import type { FleetControls } from "@/lib/fleet-state"
export const fleetName = (node: Pick<Node, "alias" | "name">) => node.alias || node.name
export function countryName(code: string | undefined, locale: string): string { if (!code || !/^[A-Z]{2}$/.test(code)) return ""; try { return new Intl.DisplayNames([locale], { type: "region" }).of(code) ?? code } catch { return code } }
export function publicFleetNode(node: Node, showIP: boolean): Node {
 return { accelerators: publicAccelerators(node.accelerators), id: node.id, name: node.name, alias: node.alias || "", group_id: node.group_id, status: node.status, host: showIP ? node.host || "" : "", port: showIP ? node.port : 0, ssh_public_key: "", cpu_model: node.cpu_model || "", os: node.os || "", platform: node.platform || "", os_version: node.os_version || "", kernel: node.kernel || "", arch: node.arch || "", virtualization: node.virtualization || "", agent_version: "", sort_order: node.sort_order || 0, tags: Array.isArray(node.tags) ? node.tags.filter(tag => typeof tag === "string") : [], is_public: true, public_remark: node.public_remark || "", last_seen: node.last_seen, created_at: "", updated_at: "", telemetry_at: node.telemetry_at, telemetry_age_seconds: node.telemetry_age_seconds, telemetry_state: node.telemetry_state ?? "unavailable", metrics: Object.fromEntries(Object.entries(node.metrics ?? {}).filter(([, value]) => typeof value === "number" && Number.isFinite(value))), traffic: node.traffic,
  ...(node.location ? { location: { scope: node.location.scope, country_code: node.location.country_code, resolved_at: node.location.resolved_at } } : {}), ...(node.lifecycle ? { lifecycle: { expires_on: node.lifecycle.expires_on, status: node.lifecycle.status, days_remaining: node.lifecycle.days_remaining, price_micros: node.lifecycle.price_micros, currency: node.lifecycle.currency, interval_unit: node.lifecycle.interval_unit, interval_count: node.lifecycle.interval_count } } : {}) }
}
export function fleetMatches(node: Node, controls: FleetControls, locale: string): boolean {
 if (!node.is_public || controls.groups.length && !controls.groups.includes(node.group_id) || controls.status && node.status !== controls.status || controls.freshness && (node.telemetry_state ?? "unavailable") !== controls.freshness) return false
 if (controls.tags.some(tag => !node.tags?.includes(tag)) || controls.country && node.location?.country_code !== controls.country) return false
 const expiry = node.lifecycle, remaining = expiry?.days_remaining
 if (controls.expiry === "expired" && expiry?.status !== "expired" || controls.expiry === "today" && expiry?.status !== "due_today" || controls.expiry === "untracked" && expiry?.status !== "untracked") return false
 if (["7", "30"].includes(controls.expiry) && (remaining === undefined || remaining < 0 || remaining > Number(controls.expiry))) return false
 const text = [fleetName(node), node.public_remark, ...(node.tags ?? []), node.os, node.platform, node.arch, countryName(node.location?.country_code, locale)].join(" ").toLocaleLowerCase(locale)
 return !controls.search || text.includes(controls.search.trim().toLocaleLowerCase(locale))
}
const numberMetric = (node: Node, key: string): number | null => node.telemetry_state === "fresh" && Number.isFinite(node.metrics?.[key]) ? node.metrics![key] : null
function compareNodes(first: Node, second: Node, controls: FleetControls, locale: string): number {
 if (controls.offline !== "keep" && first.status !== second.status) return (first.status === "offline" ? -1 : 1) * (controls.offline === "first" ? 1 : -1)
 let result = 0
 if (controls.sort === "order") result = first.sort_order - second.sort_order
 else if (controls.sort === "name") result = fleetName(first).localeCompare(fleetName(second), locale)
 else if (controls.sort === "status") result = (first.status === "online" ? 0 : 1) - (second.status === "online" ? 0 : 1)
 else {
  const a = controls.sort === "expiry" ? (first.lifecycle?.expires_on ? Date.parse(first.lifecycle.expires_on) : null) : numberMetric(first, controls.sort)
  const b = controls.sort === "expiry" ? (second.lifecycle?.expires_on ? Date.parse(second.lifecycle.expires_on) : null) : numberMetric(second, controls.sort)
  if (a === null || !Number.isFinite(a)) return b === null || !Number.isFinite(b) ? first.sort_order - second.sort_order || first.id.localeCompare(second.id) : 1
  if (b === null || !Number.isFinite(b)) return -1
  result = a - b
 }
 return result * (controls.direction === "desc" ? -1 : 1) || first.sort_order - second.sort_order || first.id.localeCompare(second.id)
}
export function selectFleet(nodes: Node[], groups: Group[], controls: FleetControls, locale: string) {
 const visible = nodes.filter(node => fleetMatches(node, controls, locale)), order = new Map(groups.map(group => [group.id, group.sort_order]))
 return visible.slice().sort((a, b) => (order.get(a.group_id) ?? Number.MAX_SAFE_INTEGER) - (order.get(b.group_id) ?? Number.MAX_SAFE_INTEGER) || a.group_id.localeCompare(b.group_id) || compareNodes(a, b, controls, locale))
}
export function fleetTotals(nodes: Node[]) {
 const rate = (key: string) => { const values = nodes.map(node => numberMetric(node, key)).filter((value): value is number => value !== null && value >= 0); return { value: values.length ? values.reduce((a, b) => a + b, 0) : null, covered: values.length } }
 return { total: nodes.length, online: nodes.filter(node => node.status === "online").length, groups: new Set(nodes.map(node => node.group_id)).size, countries: new Set(nodes.map(node => node.location?.country_code).filter(Boolean)).size, upload: rate("net_sent"), download: rate("net_recv") }
}
