import { Suspense, useState, type ComponentProps } from "react"
import { ChunkBoundary, RouteLoading } from "@/components/route-chunk"
import { resetChunk, stableChunk } from "@/components/route-chunk-cache"
import type { NetworkHistoryDialog } from "@/components/network-history-dialog"
const load = () => import("@/components/network-history-dialog").then(module => ({ default: module.NetworkHistoryDialog }))
export function PublicNetworkHistory(props: ComponentProps<typeof NetworkHistoryDialog>) {
 const [attempts, setAttempts] = useState(0)
 const retry = () => {
  resetChunk(load)
  setAttempts(value => value + 1)
 }
 return <ChunkBoundary key={attempts} attempts={attempts} retry={retry}><HistoryAttempt {...props} /></ChunkBoundary>
}

function HistoryAttempt(props: ComponentProps<typeof NetworkHistoryDialog>) {
 const Dialog = stableChunk(load).Page
 return <Suspense fallback={<RouteLoading />}><Dialog {...props} /></Suspense>
}
