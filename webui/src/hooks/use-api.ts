import { useCallback, useEffect, useState } from "react"
import type {
	  AlertChannel, AlertEvent, AlertRule, Group, MetricDataPoint,
	  MetricsSnapshot, Node, SSHKey,
	  ManagedNode, TrafficReportSchedule, CustomMetric,
} from "@/types"
import * as api from "@/lib/api"
import { retryAfterDelay } from "@/lib/retry-after"
import { useApiList } from "@/hooks/use-api-list"
import { useApiPage } from "@/hooks/use-api-page"

interface ApiState<T> {
  data: T | null
  loading: boolean
  error: string | null
}

interface RefreshOptions {
  silent?: boolean
}



export function useNodes(groupId?: string) {
  const fetcher = useCallback((signal: AbortSignal) => api.listNodes(groupId, signal), [groupId])
  return useApiList<Node>(fetcher)
}

export function useManagedNodes() {
  const fetcher = useCallback((signal: AbortSignal) => api.listManagedNodes(signal), [])
  return useApiList<ManagedNode>(fetcher)
}

export function useLiveNodes(groupId?: string) {
	  const base = useNodes(groupId)
	  const [liveNodes, setLiveNodes] = useState<Node[] | null>(null)

	  useEffect(() => {
	    let stopped = false
	    let socket: WebSocket | null = null
	    let retry: number | undefined
	    let failures = 0
	    // 只在最初几次与每十次失败时输出：onerror 不带状态码，close 的 code/reason
	    // 与失败次数才是区分被拦截、被拒绝和正常断开的证据。
	    const diagnose = (phase: string, current: WebSocket, event?: CloseEvent) => {
	      if (failures > 3 && failures % 10 !== 0) return
	      console.warn(`[metrics-ws] ${phase}`, { url: current.url, code: event?.code, reason: event?.reason, wasClean: event?.wasClean, failures, hidden: document.hidden, online: navigator.onLine })
	    }
	    const connect = () => {
	      const protocol = window.location.protocol === "https:" ? "wss:" : "ws:"
	      const current = new WebSocket(`${protocol}//${window.location.host}/api/v1/ws/metrics`)
	      socket = current
	      current.onopen = () => { failures = 0 }
	      current.onmessage = (event) => {
	        try {
	          const snapshot = JSON.parse(String(event.data)) as MetricsSnapshot
	          if (Array.isArray(snapshot.nodes)) setLiveNodes(snapshot.nodes)
	        } catch {
	          return
	        }
	      }
	      current.onerror = () => { failures += 1; diagnose("error", current); current.close() }
	      current.onclose = (event) => {
	        if (stopped) return
	        diagnose("close", current, event)
	        retry = window.setTimeout(connect, 2000)
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
  const fetcher = useCallback((signal: AbortSignal) => api.listGroups(signal), [])
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
  const fetcher = useCallback((signal: AbortSignal) => api.listSSHKeys(signal), [])
  return useApiList<SSHKey>(fetcher)
}

export function useAlertRules() {
  const fetcher = useCallback((signal: AbortSignal) => api.listAlertRules(signal), [])
  return useApiList<AlertRule>(fetcher)
}

export function useCustomMetrics() {
  const fetcher = useCallback((signal: AbortSignal) => api.listCustomMetrics(signal), [])
  return useApiList<CustomMetric>(fetcher)
}

export function useAlertChannels() {
  const fetcher = useCallback((signal: AbortSignal) => api.listAlertChannels(signal), [])
  return useApiList<AlertChannel>(fetcher)
}

export function useAlertEvents(status?: string) {
  const fetcher = useCallback(
    (cursor: string | undefined, signal: AbortSignal) =>
      api.listAlertEvents({ status: status === "all" ? undefined : status, cursor, signal }),
    [status],
  )
  return useApiPage<AlertEvent>(fetcher)
}

export function useTrafficReportSchedules() {
  const fetcher = useCallback((signal: AbortSignal) => api.listTrafficReportSchedules(signal), [])
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
    if (!id) return 0
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
      return 0
    } catch (err) {
      const message = err instanceof Error ? err.message : "Unknown error"
      setState((prev) => ({
        ...prev,
        loading: false,
        error: silent && prev.data !== null ? null : message,
      }))
      return retryAfterDelay(err)
    }
  }, [id, metricsKey, rangeHours])

  useEffect(() => {
    void fetch()
    if (!id) return
    // 节点指标轮询使用超时链，服务端限流时按 Retry-After 退避。
    let stopped = false
    let timer: ReturnType<typeof setTimeout>
    const tick = async () => {
      const retryDelay = await fetch({ silent: true })
      if (!stopped) timer = setTimeout(() => void tick(), Math.max(10_000, retryDelay))
    }
    timer = setTimeout(() => void tick(), 10_000)
    return () => { stopped = true; clearTimeout(timer) }
  }, [fetch, id])

  return { ...state, ...domain, refresh: fetch }
}

export function useNodeCustomMetrics(id: string | undefined) {
  const [state, setState] = useState<ApiState<api.NodeCustomMetric[]>>({
    data: null,
    loading: Boolean(id),
    error: null,
  })

  const fetch = useCallback(async () => {
    if (!id) {
      setState({ data: null, loading: false, error: null })
      return
    }
    setState((prev) => ({ ...prev, loading: true, error: null }))
    try {
      const data = await api.listNodeCustomMetrics(id)
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
