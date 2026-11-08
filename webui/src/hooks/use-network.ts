import { useCallback, useEffect, useMemo, useRef, useState } from "react"

import * as networkApi from "@/lib/network-api"
import { networkSummaryByNode } from "@/lib/network-quality"
import { retryAfterDelay } from "@/lib/retry-after"
import { MAX_THEME_NETWORK_TASKS } from "@/lib/theme-network-snapshot"
import { workspaceDomain } from "@/lib/workspace-query"
import type { NetworkHistoryPoint, NetworkHistoryResponse, NetworkTaskView } from "@/types"
import type { ThemeNetworkHistoryInput } from "@/types/theme-runtime"
import type { WorkspaceRange } from "@/types/metric-workspace"

interface NetworkState<T> {
  data: T | null
  loading: boolean
  error: string | null
}

interface HistoryOptions {
  taskId?: string
  nodeId?: string
  rangeHours: number
  admin?: boolean
  enabled?: boolean
}

interface RefreshOptions {
  silent?: boolean
}

interface NodeNetworkHistoryState {
  key: string
  data: ThemeNetworkHistoryInput | null
  loading: boolean
  error: string | null
}

export function usePublicNetworkQuality(enabled = true) {
  return useNetworkTaskViews(networkApi.listPublicNetworkQuality, 15_000, enabled)
}

export function useAdminNetworkTasks() {
  return useNetworkTaskViews(networkApi.listNetworkTasks)
}

export function useNodeNetworkSummaries(enabled = true) {
  const quality = usePublicNetworkQuality(enabled)
  const summaries = useMemo(() => networkSummaryByNode(quality.data), [quality.data])
  return { ...quality, summaries }
}

/** 节点详情只请求当前节点、当前范围所需的公开网络历史。 */
export function usePublicNodeNetwork(nodeId: string | undefined, range: WorkspaceRange | undefined, enabled = true) {
  const quality = usePublicNetworkQuality(Boolean(nodeId) && enabled)
  const rangeKey = range ? `${range.mode}:${range.mode === "relative" ? range.seconds : `${range.from}:${range.to}`}` : "none"
  const rangeRef = useRef(range)
  rangeRef.current = range
  const requestID = useRef(0)
  const [history, setHistory] = useState<NodeNetworkHistoryState>({ key: rangeKey, data: null, loading: Boolean(nodeId && enabled), error: null })

  useEffect(() => {
    const currentRequest = ++requestID.current
    const currentRange = rangeRef.current
    if (!enabled || !nodeId || !currentRange) {
      setHistory({ key: rangeKey, data: null, loading: false, error: null })
      return
    }
    const tasks = publicNodeNetworkTasks(quality.data, nodeId)
    if (!tasks.length) {
      setHistory({ key: rangeKey, data: null, loading: false, error: null })
      return
    }
    let domain: { from: string; to: string }
    try {
      domain = workspaceDomain(currentRange)
    } catch {
      setHistory({ key: rangeKey, data: null, loading: false, error: "network.invalid_range" })
      return
    }
    const controller = new AbortController()
    setHistory((previous) => ({
      key: rangeKey,
      data: previous.key === rangeKey ? previous.data : null,
      loading: previous.key !== rangeKey || previous.data === null,
      error: null,
    }))
    void loadNodeNetworkHistory(tasks, nodeId, domain, controller.signal).then((data) => {
      if (controller.signal.aborted || currentRequest !== requestID.current) return
      setHistory({ key: rangeKey, data, loading: false, error: null })
    }).catch((error: unknown) => {
      if (controller.signal.aborted || currentRequest !== requestID.current) return
      setHistory((previous) => ({ ...previous, loading: false, error: error instanceof Error ? error.message : "Unknown error" }))
    })
    return () => controller.abort()
  }, [enabled, nodeId, quality.data, rangeKey])

  return {
    quality,
    history: { data: history.data, loading: history.loading, error: history.error },
  }
}

export function publicNodeNetworkTasks(views: NetworkTaskView[] | null, nodeId?: string): NetworkTaskView[] {
  if (!views || !nodeId) return []
  return views.filter((view) => view.task.enabled === true && view.task.is_public === true &&
    view.nodes.some((state) => state.node.id === nodeId)).slice(0, MAX_THEME_NETWORK_TASKS)
}

async function loadNodeNetworkHistory(tasks: NetworkTaskView[], nodeId: string, domain: { from: string; to: string }, signal: AbortSignal): Promise<ThemeNetworkHistoryInput> {
  const settled = await Promise.allSettled(tasks.map(async (view) => {
    const response = await networkApi.getPublicNetworkHistory(view.task.id, { nodeId, from: domain.from, to: domain.to, signal })
    return { task_id: view.task.id, points: publicNetworkHistoryPoints(response.points) }
  }))
  const historyTasks = settled.flatMap((result) => result.status === "fulfilled" ? [result.value] : [])
  if (!historyTasks.length && settled.length) {
    const failure = settled.find((result) => result.status === "rejected")
    throw failure?.status === "rejected" ? failure.reason : new Error("network history unavailable")
  }
  return {
    from: Math.floor(Date.parse(domain.from) / 1000),
    to: Math.floor(Date.parse(domain.to) / 1000),
    tasks: historyTasks,
  }
}

function publicNetworkHistoryPoints(points: NetworkHistoryPoint[]): ThemeNetworkHistoryInput["tasks"][number]["points"] {
  return points.flatMap((point) => {
    const timestamp = Math.floor(Date.parse(point.timestamp) / 1000)
    if (!Number.isFinite(timestamp) || timestamp < 0) return []
    const reachable = Number.isFinite(point.success_percent) && point.success_percent > 0
    const latency = reachable && Number.isFinite(point.average_latency_ms) && point.average_latency_ms >= 0
      ? point.average_latency_ms
      : null
    return [{ timestamp, latency_ms: latency }]
  })
}

function useNetworkTaskViews(
  fetcher: () => Promise<NetworkTaskView[]>,
  refreshMilliseconds?: number,
  enabled = true,
) {
  const [state, setState] = useState<NetworkState<NetworkTaskView[]>>({
    data: null,
    loading: enabled,
    error: null,
  })

  const refresh = useCallback(async (options?: RefreshOptions) => {
    if (!enabled) return 0
    const silent = options?.silent === true
    setState((previous) => ({
      ...previous,
      loading: silent && previous.data !== null ? false : true,
      error: null,
    }))
    try {
      const data = await fetcher()
      setState({ data, loading: false, error: null })
      return 0
    } catch (error) {
      const message = error instanceof Error ? error.message : "Unknown error"
      setState((previous) => ({
        ...previous,
        loading: false,
        error: silent && previous.data !== null ? null : message,
      }))
      return retryAfterDelay(error)
    }
  }, [enabled, fetcher])

  useEffect(() => {
    if (!enabled) {
      setState((previous) => previous.loading ? { ...previous, loading: false } : previous)
      return
    }
    void refresh()
    if (!refreshMilliseconds) return
    // 轮询用超时链而不是固定间隔，这样服务端的 Retry-After 能得到遵守。
    let stopped = false
    let timer: ReturnType<typeof setTimeout>
    const tick = async () => {
      const retryDelay = await refresh({ silent: true })
      if (!stopped) timer = setTimeout(() => void tick(), Math.max(refreshMilliseconds, retryDelay))
    }
    timer = setTimeout(() => void tick(), refreshMilliseconds)
    return () => { stopped = true; clearTimeout(timer) }
  }, [enabled, refresh, refreshMilliseconds])

  return { ...state, refresh }
}

export function useNetworkHistory(options: HistoryOptions) {
  const initialTo = Math.floor(Date.now() / 1000)
  const [state, setState] = useState<NetworkState<NetworkHistoryResponse>>({
    data: null,
    loading: Boolean(options.enabled),
    error: null,
  })
  const [domain, setDomain] = useState({
    from: initialTo - options.rangeHours * 3600,
    to: initialTo,
  })

  const refresh = useCallback(async (refreshOptions?: RefreshOptions) => {
    if (!options.enabled || !options.taskId || !options.nodeId) return 0
    const silent = refreshOptions?.silent === true
    const to = Math.floor(Date.now() / 1000)
    const from = to - options.rangeHours * 3600
    setDomain({ from, to })
    setState((previous) => ({
      ...previous,
      loading: silent && previous.data !== null ? false : true,
      error: null,
    }))
    const query = {
      nodeId: options.nodeId,
      from: new Date(from * 1000).toISOString(),
      to: new Date(to * 1000).toISOString(),
    }
    try {
      const request = options.admin
        ? networkApi.getAdminNetworkHistory
        : networkApi.getPublicNetworkHistory
      const data = await request(options.taskId, query)
      setState({ data, loading: false, error: null })
      return 0
    } catch (error) {
      const message = error instanceof Error ? error.message : "Unknown error"
      setState((previous) => ({
        ...previous,
        loading: false,
        error: silent && previous.data !== null ? null : message,
      }))
      return retryAfterDelay(error)
    }
  }, [options.admin, options.enabled, options.nodeId, options.rangeHours, options.taskId])

  useEffect(() => {
    void refresh()
    if (!options.enabled) return
    let stopped = false
    let timer: ReturnType<typeof setTimeout>
    const tick = async () => {
      const retryDelay = await refresh({ silent: true })
      if (!stopped) timer = setTimeout(() => void tick(), Math.max(15_000, retryDelay))
    }
    timer = setTimeout(() => void tick(), 15_000)
    return () => { stopped = true; clearTimeout(timer) }
  }, [options.enabled, refresh])

  return { ...state, ...domain, refresh }
}
