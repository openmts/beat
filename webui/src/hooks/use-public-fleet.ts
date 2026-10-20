import { useEffect, useRef, useState } from "react"
import { PublicFleetLive, type PublicFleetSnapshot } from "@/lib/public-fleet-live"
export function usePublicFleet() {
 const [state, setState] = useState<PublicFleetSnapshot>({ nodes: [], groups: [], loading: true, error: "", connected: false, offline: false, receivedAt: null, serverTime: null, stale: false })
 const current = useRef<PublicFleetLive | null>(null)
 useEffect(() => { const live = new PublicFleetLive({ publish: setState }); current.current = live; const stop = live.start(); return () => { current.current = null; stop() } }, [])
 return { ...state, refresh: () => { void current.current?.refresh() } }
}
