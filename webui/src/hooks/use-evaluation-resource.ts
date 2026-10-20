import { useCallback, useEffect, useRef, useState } from "react"

export function useEvaluationResource<T>(fetcher: (signal: AbortSignal) => Promise<T>) {
  const [state, setState] = useState<{ data: T | null; loading: boolean; error: string | null }>({ data: null, loading: true, error: null })
  const request = useRef<AbortController | null>(null)
  const refresh = useCallback(async () => {
    request.current?.abort()
    const controller = new AbortController(); request.current = controller
    setState(previous => ({ ...previous, loading: true, error: null }))
    try {
      const data = await fetcher(controller.signal)
      if (!controller.signal.aborted) setState({ data, loading: false, error: null })
    } catch (error) {
      if (!controller.signal.aborted) setState(previous => ({ ...previous, loading: false, error: error instanceof Error ? error.message : "Unknown error" }))
    }
  }, [fetcher])
  useEffect(() => { setState({ data: null, loading: true, error: null }); void refresh(); return () => request.current?.abort() }, [refresh])
  return { ...state, refresh }
}
