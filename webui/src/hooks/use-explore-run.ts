import { useEffect, useRef, useState } from "react"
import { retryAfterDelay } from "@/lib/retry-after"
import { queryWorkspaceLatest, queryWorkspaceMetrics } from "@/lib/metric-workspace-api"
import { exploreExpanded, exploreInstantSupported, exploreLatestRequest, exploreRequest, type ExploreState } from "@/lib/metric-explore"
import type { Node } from "@/types"
import type { ExpandedSeries, MetricCatalog, MetricLatestQuery, MetricLatestResult, MetricQuery, MetricQueryResult } from "@/types/metric-workspace"

export interface ExploreRun {
  loading: boolean
  error?: string
  result?: MetricQueryResult
  query?: MetricQuery
  /** instant 查询的结果与载荷；与 result/query 互斥，页面按查询类型只渲染其中一套。 */
  latest?: MetricLatestResult
  latestQuery?: MetricLatestQuery
  expanded?: ExpandedSeries[]
  updatedAt?: number
}

export interface ExploreRunOptions {
  state: ExploreState
  nodes: Node[]
  catalog: MetricCatalog
  ready: boolean
  refreshSeconds: number
}

/** 探索执行的唯一入口：显式运行、可见性恢复与自动刷新都走同一个计数触发器。 */
export function useExploreRun(options: ExploreRunOptions): { run: ExploreRun; refresh: () => void } {
  const [snapshot, setSnapshot] = useState<ExploreRun>({ loading: false })
  const [tick, setTick] = useState(0)
  const [retryDelay, setRetryDelay] = useState(0)
  const latest = useRef(options)
  latest.current = options
  const ready = options.ready

  useEffect(() => {
    const current = latest.current
    if (!ready) return
    const instant = current.state.query_type === "instant"
    if (instant && !exploreInstantSupported(current.state)) {
      setSnapshot({ loading: false, error: "explore.instant_device_unsupported" })
      return
    }
    let rangeQuery: MetricQuery | undefined
    let latestQuery: MetricLatestQuery | undefined
    try {
      if (instant) latestQuery = exploreLatestRequest(current.state)
      else rangeQuery = exploreRequest(current.state)
    } catch {
      setSnapshot({ loading: false, error: "explore.no_queries" })
      return
    }
    if (!rangeQuery && !latestQuery) {
      setSnapshot({ loading: false, error: "explore.no_queries" })
      return
    }
    const controller = new AbortController()
    const expanded = exploreExpanded(current.state, current.nodes, current.catalog)
    setSnapshot((value) => ({ ...value, loading: true, error: undefined, query: rangeQuery, latestQuery, result: undefined, latest: undefined }))
    // 成功与失败都保留载荷、展开信息与更新时间，页面据此渲染「最近一次成功结果」。
    const settle = (patch: Partial<ExploreRun>) => {
      if (controller.signal.aborted) return
      setSnapshot({ loading: false, query: rangeQuery, latestQuery, expanded, updatedAt: Date.now(), ...patch })
    }
    const failed = (error: unknown) => {
      if (controller.signal.aborted) return
      setRetryDelay(retryAfterDelay(error))
      setSnapshot({ loading: false, query: rangeQuery, latestQuery, error: error instanceof Error ? error.message : "workspace.query_failed" })
    }
    if (latestQuery) {
      queryWorkspaceLatest(latestQuery, { signal: controller.signal }).then((latest) => { setRetryDelay(0); settle({ latest }) }).catch(failed)
    } else if (rangeQuery) {
      queryWorkspaceMetrics(rangeQuery, { signal: controller.signal }).then((result) => { setRetryDelay(0); settle({ result }) }).catch(failed)
    }
    return () => controller.abort()
  }, [tick, ready])

  // 自动刷新只在相对范围下开启：绝对范围是固定窗口，重复查询不会产生新数据。
  useEffect(() => {
    const current = latest.current
    if (!ready || current.refreshSeconds <= 0 || current.state.range.mode !== "relative") return
    const timer = setTimeout(() => setTick((value) => value + 1), Math.max(current.refreshSeconds * 1000, retryDelay))
    return () => clearTimeout(timer)
  }, [tick, retryDelay, ready, options.refreshSeconds, options.state.range.mode])

  // 标签页回到前台时补一次查询，隐藏期间不发起网络请求。
  useEffect(() => {
    const visible = () => { if (!document.hidden) setTick((value) => value + 1) }
    document.addEventListener("visibilitychange", visible)
    return () => document.removeEventListener("visibilitychange", visible)
  }, [])

  return { run: snapshot, refresh: () => setTick((value) => value + 1) }
}
