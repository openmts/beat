import { useCallback, useEffect } from "react"
import { useApiList } from "@/hooks/use-api-list"
import { getNotificationDelivery, listNotificationDeliveries } from "@/lib/notification-api"
import type { DeliveryFilter } from "@/types/notification"

export function useNotificationDeliveries(filter: DeliveryFilter) {
  const { state, channel_id, offset, limit } = filter
  const fetch = useCallback(async (signal: AbortSignal) => [await listNotificationDeliveries({ state, channel_id, offset, limit }, signal)], [state, channel_id, offset, limit])
  const result = useApiList(fetch)
  useNotificationPolling(result.refresh)
  return { ...result, data: result.data?.[0] ?? null }
}

export function useNotificationDelivery(id: string) {
  const fetch = useCallback(async (signal: AbortSignal) => [await getNotificationDelivery(id, signal)], [id])
  const result = useApiList(fetch)
  useNotificationPolling(result.refresh)
  return { ...result, data: result.data?.[0] ?? null }
}

/** useNotificationPolling 遵守服务端的 Retry-After，避免在限流期间继续按固定间隔重试。 */
function useNotificationPolling(refresh: (options?: { silent?: boolean }) => Promise<number>) {
  useEffect(() => {
    let stopped = false
    let running = false
    let timer: ReturnType<typeof setTimeout>
    const tick = async () => {
      if (stopped || running || document.hidden) return
      running = true
      const retryDelay = await refresh({ silent: true })
      running = false
      if (!stopped && !document.hidden) timer = setTimeout(() => void tick(), Math.max(5_000, retryDelay))
    }
    const visible = () => { clearTimeout(timer); if (!document.hidden) void tick() }
    timer = setTimeout(() => void tick(), 5_000)
    document.addEventListener("visibilitychange", visible)
    return () => { stopped = true; clearTimeout(timer); document.removeEventListener("visibilitychange", visible) }
  }, [refresh])
}
