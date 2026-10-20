import { useCallback, useEffect, useRef, useState } from "react"
import { retryAfterDelay } from "@/lib/retry-after"

interface Page<Item> {
  items: Item[]
  next_cursor?: string
}

interface PageState<Item> {
  data: Item[] | null
  loading: boolean
  loadingMore: boolean
  error: string | null
  nextCursor: string | null
}

const emptyPage = <Item,>(): PageState<Item> => ({
  data: null, loading: true, loadingMore: false, error: null, nextCursor: null,
})

/** useApiPage 只累加服务端返回的游标页，绝不在浏览器端过滤或补齐整表。 */
export function useApiPage<Item>(
  fetcher: (cursor: string | undefined, signal: AbortSignal) => Promise<Page<Item>>,
) {
  const [state, setState] = useState<PageState<Item>>(emptyPage)
  const currentRequest = useRef<AbortController | null>(null)

  const refresh = useCallback(async (options?: { silent?: boolean }) => {
    currentRequest.current?.abort()
    const controller = new AbortController()
    currentRequest.current = controller
    setState((previous) => ({
      ...previous,
      loading: !options?.silent || previous.data === null,
      loadingMore: false,
      error: null,
    }))
    try {
      const page = await fetcher(undefined, controller.signal)
      if (!controller.signal.aborted) {
        setState({ data: page.items, loading: false, loadingMore: false, error: null,
          nextCursor: page.next_cursor ?? null })
      }
      return 0
    } catch (error) {
      if (!controller.signal.aborted) {
        setState((previous) => ({ ...previous, loading: false, loadingMore: false,
          error: error instanceof Error ? error.message : "Unknown error" }))
      }
      return retryAfterDelay(error)
    }
  }, [fetcher])

  useEffect(() => {
    setState(emptyPage())
    void refresh()
    return () => currentRequest.current?.abort()
  }, [refresh])

  const loadMore = useCallback(async () => {
    const cursor = state.nextCursor
    if (!cursor || state.loadingMore) return
    const controller = new AbortController()
    currentRequest.current = controller
    setState((previous) => ({ ...previous, loadingMore: true, error: null }))
    try {
      const page = await fetcher(cursor, controller.signal)
      if (controller.signal.aborted) return
      setState((previous) => ({
        data: [...(previous.data ?? []), ...page.items],
        loading: false, loadingMore: false, error: null, nextCursor: page.next_cursor ?? null,
      }))
    } catch (error) {
      if (!controller.signal.aborted) {
        setState((previous) => ({ ...previous, loadingMore: false,
          error: error instanceof Error ? error.message : "Unknown error" }))
      }
    }
  }, [fetcher, state.loadingMore, state.nextCursor])

  return { ...state, refresh, loadMore }
}
