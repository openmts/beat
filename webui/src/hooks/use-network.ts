import { useCallback, useEffect, useMemo, useState } from "react"

import * as networkApi from "@/lib/network-api"
import { networkSummaryByNode } from "@/lib/network-quality"
import { retryAfterDelay } from "@/lib/retry-after"
import type { NetworkHistoryResponse, NetworkTaskView } from "@/types"

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
