import { useCallback, useEffect, useRef, useState } from "react"
import { useAuth } from "@/context/auth"
import { useLocale } from "@/context/locale"
import type { Translate } from "@/context/locales"
import { AgentRolloutKeyedError } from "@/lib/agent-rollout-api"
import { APIError } from "@/lib/api-error"
import { retryAfterDelay } from "@/lib/retry-after"
import type { AgentPage } from "@/types/agent-rollouts"
export function agentRolloutError(error: unknown, t: Translate) {
 if (error instanceof APIError && error.status === 404) return t("agentRollout.error_module")
 if (error instanceof AgentRolloutKeyedError) return t(error.key)
 return error instanceof Error ? error.message : t("agentRollout.error_request")
}
const refreshInterval = 15_000
export function useAgentRolloutData<T>(fetcher: (signal: AbortSignal) => Promise<T>) {
 const { t } = useLocale(), { principal } = useAuth(), identity = `${principal?.user.id}/${principal?.session.id}`
 const [state, setState] = useState<{ data: T | null; loading: boolean; error: string }>({ data: null, loading: true, error: "" })
 const controller = useRef<AbortController | null>(null)
 const refresh = useCallback(async () => {
  controller.current?.abort(); const request = new AbortController(); controller.current = request
  try {
   const data = await fetcher(request.signal)
   if (!request.signal.aborted) setState({ data, loading: false, error: "" })
   return 0
  } catch (error) {
   if (!request.signal.aborted) setState({ data: null, loading: false, error: agentRolloutError(error, t) })
   return retryAfterDelay(error)
  }
 }, [fetcher, t])
 useEffect(() => {
  setState({ data: null, loading: true, error: "" })
  let stopped = false
  let timer: ReturnType<typeof setTimeout>
  const tick = async () => {
   if (!document.hidden) {
    const retryDelay = await refresh()
    if (!stopped) timer = setTimeout(() => void tick(), Math.max(refreshInterval, retryDelay))
    return
   }
   if (!stopped) timer = setTimeout(() => void tick(), refreshInterval)
  }
  void tick()
  return () => { stopped = true; controller.current?.abort(); clearTimeout(timer) }
 }, [refresh, identity])
 return { ...state, refresh }
}
export function useAgentRolloutPage<T>(fetcher: (cursor: string, signal: AbortSignal) => Promise<AgentPage<T>>) {
 const [cursors, setCursors] = useState([""])
 const cursor = cursors[cursors.length - 1]
 const read = useCallback((signal: AbortSignal) => fetcher(cursor, signal), [fetcher, cursor])
 const source = useAgentRolloutData(read)
 return { ...source, previous: cursors.length > 1 ? () => setCursors(values => values.slice(0, -1)) : undefined,
  next: source.data?.next_cursor ? () => setCursors(values => [...values, source.data!.next_cursor]) : undefined }
}
