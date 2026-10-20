export type FleetLayout = "cards" | "table"
export type FleetSort = "order" | "name" | "status" | "cpu" | "memory" | "disk" | "net_sent" | "net_recv" | "expiry"
export interface FleetControls { search: string; groups: string[]; tags: string[]; country: string; status: string; freshness: string; expiry: string; sort: FleetSort; direction: "asc" | "desc"; offline: "first" | "keep" | "last"; layout: FleetLayout }
export const defaultFleetControls: FleetControls = { search: "", groups: [], tags: [], country: "", status: "", freshness: "", expiry: "", sort: "order", direction: "asc", offline: "last", layout: "cards" }
const choice = <T extends string>(value: string | null, allowed: readonly T[], fallback: T) => allowed.includes(value as T) ? value as T : fallback
const values = (query: URLSearchParams, key: string, maximum: number) => [...new Set(query.getAll(key).filter(value => value.length > 0 && value.length <= 128))].slice(0, maximum)
export function parseFleetControls(query: string): FleetControls {
 const params = new URLSearchParams(query.length <= 4096 ? query : "")
 return { search: (params.get("q") ?? "").slice(0, 128), groups: values(params, "group", 16), tags: values(params, "tag", 16), country: /^[A-Z]{2}$/.test(params.get("country") ?? "") ? params.get("country")! : "",
  status: choice(params.get("status"), ["", "online", "offline"], ""), freshness: choice(params.get("freshness"), ["", "fresh", "delayed", "stale", "unavailable"], ""), expiry: choice(params.get("expiry"), ["", "expired", "today", "7", "30", "untracked"], ""), sort: choice(params.get("sort"), ["order", "name", "status", "cpu", "memory", "disk", "net_sent", "net_recv", "expiry"], "order"), direction: choice(params.get("direction"), ["asc", "desc"], "asc"), offline: choice(params.get("offline"), ["first", "keep", "last"], "last"), layout: choice(params.get("layout"), ["cards", "table"], "cards") }
}
export function fleetControlsQuery(value: FleetControls): string {
 const query = new URLSearchParams()
 if (value.search) query.set("q", value.search.slice(0, 128))
 value.groups.slice(0, 16).forEach(group => query.append("group", group)); value.tags.slice(0, 16).forEach(tag => query.append("tag", tag))
 for (const key of ["country", "status", "freshness", "expiry", "sort", "direction", "offline", "layout"] as const) if (value[key] !== defaultFleetControls[key]) query.set(key, value[key])
 return query.toString()
}
export const metricFleetSort = (sort: FleetSort) => ["cpu", "memory", "disk", "net_sent", "net_recv"].includes(sort)
