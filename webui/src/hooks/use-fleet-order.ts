import { useEffect, useMemo, useRef, useState } from "react"
import type { Group, Node } from "@/types"
import { metricFleetSort, type FleetControls } from "@/lib/fleet-state"
import { selectFleet } from "@/lib/fleet-selectors"
export function useFleetOrder(nodes: Node[], groups: Group[], controls: FleetControls, locale: string, paused: boolean) {
 const [order, setOrder] = useState<string[]>([]), latest = useRef(nodes)
 latest.current = nodes
 const key = JSON.stringify(controls), groupKey = JSON.stringify(groups.map(group => [group.id, group.sort_order]))
 useEffect(() => {
  const update = () => { if (!paused) setOrder(selectFleet(latest.current, groups, controls, locale).map(node => node.id)) }
  update()
  if (!metricFleetSort(controls.sort) || paused) return
  const timer = setInterval(update, 5000)
  return () => clearInterval(timer)
 // Group/filter inputs are represented by stable serializations; live values intentionally do not trigger reordering.
 // eslint-disable-next-line react-hooks/exhaustive-deps
 }, [key, groupKey, locale, paused])
 return useMemo(() => {
  const selected = selectFleet(nodes, groups, controls, locale)
  if (!metricFleetSort(controls.sort) || !order.length) return selected
  const ranks = new Map(order.map((id, index) => [id, index]))
  const groupRanks = new Map(groups.slice().sort((a, b) => a.sort_order - b.sort_order || a.id.localeCompare(b.id)).map((group, index) => [group.id, index]))
  return selected.slice().sort((a, b) => (groupRanks.get(a.group_id) ?? Number.MAX_SAFE_INTEGER) - (groupRanks.get(b.group_id) ?? Number.MAX_SAFE_INTEGER) || (ranks.get(a.id) ?? Number.MAX_SAFE_INTEGER) - (ranks.get(b.id) ?? Number.MAX_SAFE_INTEGER))
 }, [nodes, groups, controls, locale, order])
}
