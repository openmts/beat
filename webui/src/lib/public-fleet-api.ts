import type { Group, Node } from "@/types"
export const maximumFleetNodes = 1000
export const maximumFleetGroups = 200
const maximumFleetBytes = 8 * 1024 * 1024
export class FleetReadError extends Error { readonly kind: "unavailable" | "denied" | "capacity"; constructor(kind: "unavailable" | "denied" | "capacity") { super(kind); this.kind = kind } }
async function readPublic<T>(path: string, signal: AbortSignal): Promise<{ data: T; serverTime: number | null }> {
 const response = await fetch(`/api/v1/${path}`, { credentials: "omit", cache: "no-store", signal: AbortSignal.any([signal, AbortSignal.timeout(15000)]), headers: { Accept: "application/json" } })
 if (!response.ok) throw new FleetReadError([401, 403].includes(response.status) ? "denied" : "unavailable")
 const reader = response.body?.getReader()
 if (!reader) throw new FleetReadError("unavailable")
 const chunks: Uint8Array[] = []; let total = 0
 try { for (;;) { const next = await reader.read(); if (next.done) break; total += next.value.byteLength; if (total > maximumFleetBytes) throw new FleetReadError("capacity"); chunks.push(next.value) } }
 catch (error) { await reader.cancel().catch(() => undefined); throw error }
 finally { reader.releaseLock() }
 const bytes = new Uint8Array(total); let offset = 0
 for (const chunk of chunks) { bytes.set(chunk, offset); offset += chunk.byteLength }
 const body: unknown = JSON.parse(new TextDecoder().decode(bytes))
 const date = Date.parse(response.headers.get("date") ?? "")
 return { data: (body && typeof body === "object" && "data" in body ? body.data : body) as T, serverTime: Number.isFinite(date) ? date : null }
}
export function validateFleetCollection(nodes: Node[], groups: Group[]) {
 if (!Array.isArray(nodes) || !Array.isArray(groups) || nodes.length > maximumFleetNodes || groups.length > maximumFleetGroups) throw new FleetReadError("capacity")
 if (nodes.some(node => !node || typeof node !== "object") || groups.some(group => !group || typeof group.id !== "string" || typeof group.name !== "string")) throw new FleetReadError("unavailable")
 if (new Set(nodes.map(node => node.id)).size !== nodes.length || new Set(groups.map(group => group.id)).size !== groups.length) throw new FleetReadError("unavailable")
 if (nodes.some(node => node.accelerators !== undefined && (!Array.isArray(node.accelerators) || node.accelerators.length > 32))) throw new FleetReadError("capacity")
 if (nodes.some(node => node.accelerators && new Set(node.accelerators.map(device => device.device_id)).size !== node.accelerators.length)) throw new FleetReadError("unavailable")
 if (nodes.some(node => (node.tags?.length ?? 0) > 12 || Object.keys(node.metrics ?? {}).length > 128)) throw new FleetReadError("capacity")
 if (nodes.some(node => node.tags != null && (!Array.isArray(node.tags) || node.tags.some(tag => typeof tag !== "string")))) throw new FleetReadError("unavailable")
 if (nodes.some(node => typeof node.id !== "string" || typeof node.name !== "string" || !["online", "offline"].includes(node.status) || typeof node.group_id !== "string")) throw new FleetReadError("unavailable")
}
export async function readPublicFleet(signal: AbortSignal): Promise<{ nodes: Node[]; groups: Group[]; serverTime: number | null }> {
 const [nodeResponse, groupResponse] = await Promise.all([readPublic<Node[]>("nodes", signal), readPublic<Group[]>("groups", signal)])
 const nodes = nodeResponse.data, groups = groupResponse.data
 validateFleetCollection(nodes, groups)
 const visible = nodes.filter(node => node.is_public === true), represented = new Set(visible.map(node => node.group_id))
 return { nodes: visible, groups: groups.filter(group => represented.has(group.id)), serverTime: nodeResponse.serverTime }
}
