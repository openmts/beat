import { useCallback, useEffect, useState } from "react"

import * as networkApi from "@/lib/network-api"
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

export function usePublicNetworkQuality() {
  return useNetworkTaskViews(networkApi.listPublicNetworkQuality, 15_000)
}

export function useAdminNetworkTasks() {
  return useNetworkTaskViews(networkApi.listNetworkTasks)
}

function useNetworkTaskViews(
  fetcher: () => Promise<NetworkTaskView[]>,
  refreshMilliseconds?: number,
) {
  const [state, setState] = useState<NetworkState<NetworkTaskView[]>>({
    data: null,
    loading: true,
    error: null,
  })

  const refresh = useCallback(async (options?: RefreshOptions) => {
    const silent = options?.silent === true
    setState((previous) => ({
      ...previous,
      loading: silent && previous.data !== null ? false : true,
      error: null,
    }))
    try {
      const data = await fetcher()
      setState({ data, loading: false, error: null })
    } catch (error) {
      const message = error instanceof Error ? error.message : "Unknown error"
      setState((previous) => ({
        ...previous,
        loading: false,
        error: silent && previous.data !== null ? null : message,
      }))
    }
  }, [fetcher])

  useEffect(() => {
    void refresh()
    if (!refreshMilliseconds) return
    const interval = window.setInterval(() => {
      void refresh({ silent: true })
    }, refreshMilliseconds)
    return () => window.clearInterval(interval)
  }, [refresh, refreshMilliseconds])

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
    if (!options.enabled || !options.taskId || !options.nodeId) return
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
    } catch (error) {
      const message = error instanceof Error ? error.message : "Unknown error"
      setState((previous) => ({
        ...previous,
        loading: false,
        error: silent && previous.data !== null ? null : message,
      }))
    }
  }, [options.admin, options.enabled, options.nodeId, options.rangeHours, options.taskId])

  useEffect(() => {
    void refresh()
    if (!options.enabled) return
    const interval = window.setInterval(() => {
      void refresh({ silent: true })
    }, 15_000)
    return () => window.clearInterval(interval)
  }, [options.enabled, refresh])

  return { ...state, ...domain, refresh }
}
