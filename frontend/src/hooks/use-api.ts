import { useCallback, useEffect, useState } from "react"
import type {
	  AlertChannel, AlertEvent, AlertRule, Group, MetricDataPoint,
	  MetricsSnapshot, Node, SSHKey,
	  ManagedNode, TrafficReportSchedule,
} from "@/types"
import * as api from "@/lib/api"

interface ApiState<T> {
  data: T | null
  loading: boolean
  error: string | null
}

interface RefreshOptions {
  silent?: boolean
}

type Refresh = (options?: RefreshOptions) => Promise<void>

function useApiList<T>(fetcher: () => Promise<T[]>): ApiState<T[]> & { refresh: Refresh } {
  const [state, setState] = useState<ApiState<T[]>>({
    data: null,
    loading: true,
    error: null,
  })

  const fetch = useCallback(async (options?: RefreshOptions) => {
    const silent = options?.silent === true
    setState((prev) => ({
      ...prev,
      loading: silent && prev.data !== null ? false : true,
      error: null,
    }))
    try {
      const data = await fetcher()
      setState({ data, loading: false, error: null })
    } catch (err) {
      const message = err instanceof Error ? err.message : "Unknown error"
      setState((prev) => ({
        ...prev,
        loading: false,
        error: silent && prev.data !== null ? null : message,
      }))
    }
  }, [fetcher])

  useEffect(() => {
    fetch()
  }, [fetch])

  return { ...state, refresh: fetch }
}

export function useNodes(groupId?: string) {
  const fetcher = useCallback(() => api.listNodes(groupId), [groupId])
  return useApiList<Node>(fetcher)
}

export function useManagedNodes() {
  const fetcher = useCallback(() => api.listManagedNodes(), [])
  return useApiList<ManagedNode>(fetcher)
}

export function useLiveNodes(groupId?: string) {
	  const base = useNodes(groupId)
	  const [liveNodes, setLiveNodes] = useState<Node[] | null>(null)

	  useEffect(() => {
	    let stopped = false
	    let socket: WebSocket | null = null
	    let retry: number | undefined
	    const connect = () => {
	      const protocol = window.location.protocol === "https:" ? "wss:" : "ws:"
	      socket = new WebSocket(`${protocol}//${window.location.host}/api/v1/ws/metrics`)
	      socket.onmessage = (event) => {
	        try {
	          const snapshot = JSON.parse(String(event.data)) as MetricsSnapshot
	          if (Array.isArray(snapshot.nodes)) setLiveNodes(snapshot.nodes)
	        } catch {
	          return
	        }
	      }
	      socket.onerror = () => socket?.close()
	      socket.onclose = () => {
	        if (!stopped) retry = window.setTimeout(connect, 2000)
	      }
	    }
	    connect()
	    return () => {
	      stopped = true
	      if (retry !== undefined) window.clearTimeout(retry)
	      socket?.close()
	    }
	  }, [])

	  const data = liveNodes === null
	    ? base.data
	    : liveNodes.filter((node) => !groupId || node.group_id === groupId)
	  return { ...base, data }
}

export function useGroups() {
  const fetcher = useCallback(() => api.listGroups(), [])
  return useApiList<Group>(fetcher)
}

export function useNode(id: string | undefined) {
  const [state, setState] = useState<ApiState<Node>>({
    data: null,
    loading: Boolean(id),
    error: null,
  })

  const fetch = useCallback(async () => {
    if (!id) {
      setState({ data: null, loading: false, error: "Node ID is required" })
      return
    }
    setState((prev) => ({ ...prev, loading: true, error: null }))
    try {
      const data = await api.getNode(id)
      setState({ data, loading: false, error: null })
    } catch (err) {
      const message = err instanceof Error ? err.message : "Unknown error"
      setState({ data: null, loading: false, error: message })
    }
  }, [id])

  useEffect(() => {
    void fetch()
  }, [fetch, id])

  return { ...state, refresh: fetch }
}

export function useSSHKeys() {
  const fetcher = useCallback(() => api.listSSHKeys(), [])
  return useApiList<SSHKey>(fetcher)
}

export function useAlertRules() {
  const fetcher = useCallback(() => api.listAlertRules(), [])
  return useApiList<AlertRule>(fetcher)
}

export function useAlertChannels() {
  const fetcher = useCallback(() => api.listAlertChannels(), [])
  return useApiList<AlertChannel>(fetcher)
}

export function useAlertEvents() {
  const fetcher = useCallback(() => api.listAlertEvents(), [])
  return useApiList<AlertEvent>(fetcher)
}

export function useTrafficReportSchedules() {
  const fetcher = useCallback(() => api.listTrafficReportSchedules(), [])
  return useApiList<TrafficReportSchedule>(fetcher)
}

export function useNodeMetrics(
  id: string | undefined,
  metrics?: string[],
  rangeHours = 1,
) {
  const metricsKey = metrics?.join(",") ?? ""
  const initialTo = Math.floor(Date.now() / 1000)
  const [state, setState] = useState<ApiState<Record<string, MetricDataPoint[]>>>({
    data: null,
    loading: Boolean(id),
    error: null,
  })
  const [domain, setDomain] = useState({
    from: initialTo - rangeHours * 60 * 60,
    to: initialTo,
  })

  const fetch = useCallback(async (options?: RefreshOptions) => {
    if (!id) return
    const silent = options?.silent === true
    const to = Math.floor(Date.now() / 1000)
    const from = to - rangeHours * 60 * 60
    setDomain({ from, to })
    setState((prev) => ({
      ...prev,
      loading: silent && prev.data !== null ? false : true,
      error: null,
    }))
    try {
      const requestedMetrics = metricsKey ? metricsKey.split(",") : undefined
      const data = await api.getNodeMetrics(
        id,
        requestedMetrics,
        new Date(from * 1000).toISOString(),
        new Date(to * 1000).toISOString(),
      )
      setState({ data, loading: false, error: null })
    } catch (err) {
      const message = err instanceof Error ? err.message : "Unknown error"
      setState((prev) => ({
        ...prev,
        loading: false,
        error: silent && prev.data !== null ? null : message,
      }))
    }
  }, [id, metricsKey, rangeHours])

  useEffect(() => {
    void fetch()
    if (!id) return
    const interval = window.setInterval(() => {
      void fetch({ silent: true })
    }, 10_000)
    return () => window.clearInterval(interval)
  }, [fetch, id])

  return { ...state, ...domain, refresh: fetch }
}
