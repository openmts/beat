import { Component, Suspense, useEffect, useState, type ReactNode } from "react"
import { Button } from "@/components/ui/button"
import { Skeleton } from "@/components/ui/skeleton"
import { useLocale } from "@/context/locale"
import { resetChunk, stableChunk, type RouteLoader } from "@/components/route-chunk-cache"
import type { StalledChunkDeps } from "@/lib/chunk-stall"
export type { RouteChunk, RouteLoader } from "@/components/route-chunk-cache"
export class ChunkBoundary extends Component<{ children: ReactNode; retry: () => void; attempts: number }, { failed: boolean }> {
 state = { failed: false }
 static getDerivedStateFromError() { return { failed: true } }
 render() { return this.state.failed ? <ChunkFailure retry={this.props.retry} attempts={this.props.attempts} /> : this.props.children }
}
function ChunkFailure({ retry, attempts }: { retry: () => void; attempts: number }) {
 const { t } = useLocale()
 return <div role="alert" className="container-page space-y-3 py-8"><p>{t("shell.chunk_failed")}</p><Button onClick={attempts < 2 ? retry : () => location.reload()}>{attempts < 2 ? t("shell.chunk_retry") : t("shell.chunk_reload")}</Button></div>
}
export function RouteLoading({ recovery }: { recovery?: StalledChunkDeps } = {}) {
 const { t } = useLocale()
 // 分片挂起时加载态不会自行结束；看门狗按需加载，避免占用公共外壳预算。
 useEffect(() => {
  let stop: (() => void) | undefined
  let cancelled = false
  void import("@/lib/chunk-stall").then(module => { if (!cancelled) stop = module.watchStalledChunk(recovery) }).catch(() => undefined)
  return () => { cancelled = true; stop?.() }
 }, [recovery])
 return <div role="status" aria-label={t("app.loading_page")} className="container-page space-y-4 py-6"><Skeleton className="h-8 w-48" /><Skeleton className="h-40 w-full" /></div>
}
export function LazyRoute({ load }: { load: RouteLoader }) {
 const [retryState, setRetryState] = useState({ load, attempts: 0 })
 const attempts = retryState.load === load ? retryState.attempts : 0
 const { Page, key } = stableChunk(load)
 const retry = () => {
  resetChunk(load)
  setRetryState({ load, attempts: attempts + 1 })
 }
 return <ChunkBoundary key={key} attempts={attempts} retry={retry}><Suspense fallback={<RouteLoading />}><Page /></Suspense></ChunkBoundary>
}
