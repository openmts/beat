import { useCallback, useEffect, useState } from "react"
import { retryAfterDelay } from "@/lib/retry-after"
export function useOperationResource<T>(load: (signal: AbortSignal) => Promise<T>, interval = 0) {
 const [generation, setGeneration] = useState(0)
 const [state, setState] = useState<{ data?: T; loading: boolean; error: string }>({ loading: true, error: "" })
 useEffect(() => {
  let stopped = false, timer: ReturnType<typeof setTimeout> | undefined, active: AbortController | undefined
  setState({ loading: true, error: "" })
  const poll = async () => {
   if (stopped || document.hidden) return
   active?.abort(); clearTimeout(timer)
   const controller = new AbortController(); active = controller
   let retryDelay = 0
   try {
    const data = await load(controller.signal)
    if (!controller.signal.aborted) setState({ data, loading: false, error: "" })
   } catch (error) {
    retryDelay = retryAfterDelay(error)
    if (!controller.signal.aborted) setState(previous => ({ ...previous, loading: false, error: error instanceof Error ? error.message : "Request failed" }))
   } finally { if (!stopped && !controller.signal.aborted && interval > 0) timer = setTimeout(() => { void poll() }, Math.max(interval, retryDelay)) }
  }
  const visibility = () => { if (document.hidden) { active?.abort(); clearTimeout(timer) } else void poll() }
  document.addEventListener("visibilitychange", visibility); void poll()
  return () => { stopped = true; active?.abort(); clearTimeout(timer); document.removeEventListener("visibilitychange", visibility) }
 }, [load, interval, generation])
 return { ...state, reload: useCallback(() => setGeneration(value => value + 1), []) }
}
