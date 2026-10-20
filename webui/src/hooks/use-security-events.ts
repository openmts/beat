import { useCallback, useEffect, useState } from "react"
import { useApiList } from "@/hooks/use-api-list"
import { getSecurityEvent, getSecurityOptions, getSecurityPolicy, listSecurityEvents } from "@/lib/security-api"
import type { SecurityEventFilter, SecurityOptions, SecurityNotificationSettings } from "@/types/security"

const pollIntervalMs = 15_000

/** 页面可见时静默刷新，保持筛选、滚动位置与已打开的详情。 */
function useSecurityPolling(refresh: (options?: { silent?: boolean }) => Promise<number>) {
  useEffect(() => {
    let stopped = false
    let running = false
    let timer: ReturnType<typeof setTimeout>
    const tick = async () => {
      if (stopped || running || document.hidden) return
      running = true
      const retryDelay = await refresh({ silent: true })
      running = false
      const delay = Math.max(pollIntervalMs, retryDelay)
      if (!stopped && !document.hidden) timer = setTimeout(() => void tick(), delay)
    }
    const visible = () => { clearTimeout(timer); if (!document.hidden) void tick() }
    timer = setTimeout(() => void tick(), pollIntervalMs)
    document.addEventListener("visibilitychange", visible)
    return () => { stopped = true; clearTimeout(timer); document.removeEventListener("visibilitychange", visible) }
  }, [refresh])
}

export function useSecurityEvents(filter: SecurityEventFilter) {
  const { kind, severity, reason, source, routing, delivery, actor, offset, limit } = filter
  const fetch = useCallback(async (signal: AbortSignal) =>
    [await listSecurityEvents({ kind, severity, reason, source, routing, delivery, actor, offset, limit }, signal)],
    [kind, severity, reason, source, routing, delivery, actor, offset, limit])
  const result = useApiList(fetch)
  useSecurityPolling(result.refresh)
  return { ...result, data: result.data?.[0] ?? null }
}

export function useSecurityEvent(id: string | null) {
  const fetch = useCallback(async (signal: AbortSignal) =>
    id ? [await getSecurityEvent(id, signal)] : [], [id])
  const result = useApiList(fetch)
  return { ...result, data: result.data?.[0] ?? null }
}

export function useSecurityOptions() {
  const [state, setState] = useState<{ data: SecurityOptions | null; error: string }>({ data: null, error: "" })
  useEffect(() => {
    const controller = new AbortController()
    getSecurityOptions(controller.signal)
      .then(options => { if (!controller.signal.aborted) setState({ data: options, error: "" }) })
      .catch((cause: unknown) => {
        if (!controller.signal.aborted) setState({ data: null, error: cause instanceof Error ? cause.message : "Unknown error" })
      })
    return () => controller.abort()
  }, [])
  return state
}

/** owner 才读取策略；enabled=false 时不发请求，返回空数据。 */
export function useSecurityPolicy(enabled: boolean) {
  const fetch = useCallback(async (signal: AbortSignal) =>
    enabled ? [await getSecurityPolicy(signal)] : [], [enabled])
  const result = useApiList(fetch)
  return { ...result, data: (result.data?.[0] ?? null) as SecurityNotificationSettings | null }
}
