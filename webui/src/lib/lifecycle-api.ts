import { api } from "@/lib/api-client"
import type { GeoIPSettings, GeoIPSettingsWrite, LifecycleSettings, NodeLocation, LocationWrite, NodeLifecycle, LifecycleWrite, LifecycleEvent } from "@/types/node-lifecycle"
const nodePath = (id: string) => `/admin/nodes/${encodeURIComponent(id)}`
export async function getGeoIPSettings(signal?: AbortSignal) { return (await api.get<GeoIPSettings>("/admin/settings/geoip", { signal })).data }
export async function saveGeoIPSettings(input: GeoIPSettingsWrite) { return (await api.put<GeoIPSettings>("/admin/settings/geoip", input)).data }
export async function getLifecycleSettings(signal?: AbortSignal) { return (await api.get<LifecycleSettings>("/admin/settings/lifecycle", { signal })).data }
export async function saveLifecycleSettings(input: LifecycleSettings) { return (await api.put<LifecycleSettings>("/admin/settings/lifecycle", input)).data }
export async function getLocation(id: string, signal?: AbortSignal) { return (await api.get<NodeLocation>(`${nodePath(id)}/location`, { signal })).data }
export async function saveLocation(id: string, input: LocationWrite) { return (await api.put<NodeLocation>(`${nodePath(id)}/location`, input)).data }
export async function refreshLocation(id: string) { return (await api.post<NodeLocation>(`${nodePath(id)}/location/refresh`)).data }
export async function getLifecycle(id: string, signal?: AbortSignal) { return (await api.get<NodeLifecycle>(`${nodePath(id)}/lifecycle`, { signal })).data }
export async function saveLifecycle(id: string, input: LifecycleWrite) { return (await api.put<NodeLifecycle>(`${nodePath(id)}/lifecycle`, input)).data }
export async function getLifecycleEvents(id: string, signal?: AbortSignal) { return (await api.get<LifecycleEvent[]>(`${nodePath(id)}/lifecycle/events`, { signal })).data }
export async function uploadMMDB(file: File, revision: number) {
 if (!file.size || file.size > 128 * 1024 * 1024) throw new Error("MMDB must be between 1 byte and 128 MiB")
 return (await api.post<GeoIPSettings>("/admin/settings/geoip/mmdb", file, { headers: { "Content-Type": "application/octet-stream", "If-Match": String(revision) } })).data
}
export interface MMDBUpdate { account_id: string; license_key: string; edition: "GeoLite2-Country" | "GeoIP2-Country"; expected_revision: number }
export async function updateMMDB(input: MMDBUpdate) { return (await api.post<GeoIPSettings>("/admin/settings/geoip/mmdb/update", input)).data }
export async function rollbackMMDB(expected_revision: number) { return (await api.post<GeoIPSettings>("/admin/settings/geoip/mmdb/rollback", { expected_revision })).data }
