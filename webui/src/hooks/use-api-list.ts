import { useCallback, useEffect, useRef, useState } from "react"
import { retryAfterDelay } from "@/lib/retry-after"

export function useApiList<T>(fetcher: (signal: AbortSignal) => Promise<T[]>) {
  const [state, setState] = useState<{ data: T[] | null; loading: boolean; error: string | null }>({ data: null, loading: true, error: null })
  const currentRequest = useRef<AbortController | null>(null)
  const refresh = useCallback(async (options?: { silent?: boolean }) => {
    currentRequest.current?.abort()
    const controller = new AbortController()
    currentRequest.current = controller
    setState((previous) => ({ ...previous, loading: !options?.silent || previous.data === null, error: null }))
    try {
      const data = await fetcher(controller.signal)
      if (!controller.signal.aborted) setState({ data, loading: false, error: null })
      return 0
    } catch (error) {
      if (!controller.signal.aborted) setState((previous) => ({ ...previous, loading: false, error: error instanceof Error ? error.message : "Unknown error" }))
      // 轮询方据此遵守服务端的 Retry-After，而不是继续按固定间隔重试。
      return retryAfterDelay(error)
    }
  }, [fetcher])
  useEffect(() => {
    setState({ data: null, loading: true, error: null })
    void refresh()
    return () => currentRequest.current?.abort()
  }, [refresh])
  return { ...state, refresh }
}
