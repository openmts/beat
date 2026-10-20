import { useMemo } from "react"
import { useSearchParams } from "react-router"
import { fleetControlsQuery, parseFleetControls, type FleetControls } from "@/lib/fleet-state"
import type { Group, Node } from "@/types"
export function useFleetControls(nodes: Node[], groups: Group[]) {
 const [query, setQuery] = useSearchParams(), raw = query.toString()
 const parsed = useMemo(() => {
  const value = parseFleetControls(raw)
  if (!new URLSearchParams(raw).has("layout") && !window.matchMedia("(max-width: 640px)").matches) {
   try { if (localStorage.getItem("beat.fleet.layout") === "table") value.layout = "table" } catch { /* presentation storage is optional */ }
  }
  return value
 }, [raw])
 const groupIDs = new Set(groups.map(group => group.id)), tags = new Set(nodes.flatMap(node => node.tags ?? []))
 const value: FleetControls = { ...parsed, groups: parsed.groups.filter(id => groupIDs.has(id)), tags: parsed.tags.filter(tag => tags.has(tag)) }
 const change = (patch: Partial<FleetControls>) => {
  const next = { ...value, ...patch }
  if (patch.layout) { try { localStorage.setItem("beat.fleet.layout", patch.layout) } catch { /* presentation storage is optional */ } }
  const updated = new URLSearchParams(fleetControlsQuery(next))
  if (patch.layout || query.has("layout")) updated.set("layout", next.layout)
  setQuery(updated, { replace: true })
 }
 return { value, change }
}
