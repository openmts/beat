import { useCallback, useEffect, useMemo, useRef, useState } from "react"

import * as diagnosticsApi from "@/lib/diagnostics-api"
import { retryAfterDelay } from "@/lib/retry-after"
import type {
  DiagnosticCapability,
  DiagnosticJob,
  DiagnosticJobDetail,
  DiagnosticJobFilter,
  DiagnosticPolicy,
  DiagnosticSeries,
  DiagnosticTarget,
} from "@/lib/diagnostics-api"

interface ResourceState<T> {
  data: T | null
  loading: boolean
  error: string | null
}

interface Resource<T> extends ResourceState<T> {
  /** refresh 返回服务端要求的退避毫秒数；0 表示可以立即按自身间隔刷新。 */
  refresh: () => Promise<number>
}

function messageOf(error: unknown): string {
  return error instanceof Error ? error.message : "Unknown error"
}

// useResource 保留上一次成功的数据，静默刷新失败时不打断正在查看的内容。
function useResource<T>(key: string, fetcher: () => Promise<T>, refreshMilliseconds?: number): Resource<T> {
  const fetcherRef = useRef(fetcher)
  fetcherRef.current = fetcher
  const [state, setState] = useState<ResourceState<T>>({ data: null, loading: true, error: null })

  const load = useCallback(async () => {
    try {
      const data = await fetcherRef.current()
      setState({ data, loading: false, error: null })
      return 0
    } catch (error) {
      setState((previous) => ({ data: previous.data, loading: false, error: messageOf(error) }))
      return retryAfterDelay(error)
    }
  }, [])

  useEffect(() => {
    let active = true
    setState((previous) => ({ data: previous.data, loading: previous.data === null, error: null }))
    void (async () => {
      try {
        const data = await fetcherRef.current()
        if (active) setState({ data, loading: false, error: null })
      } catch (error) {
        if (active) setState((previous) => ({ data: previous.data, loading: false, error: messageOf(error) }))
      }
    })()
    return () => {
      active = false
    }
  }, [key])

  useEffect(() => {
    if (!refreshMilliseconds) return undefined
    // 超时链让服务端的 Retry-After 优先于本地的刷新间隔。
    let stopped = false
    let timer: ReturnType<typeof setTimeout>
    const tick = async () => {
      const retryDelay = await load()
      if (!stopped) timer = setTimeout(() => void tick(), Math.max(refreshMilliseconds, retryDelay))
    }
    timer = setTimeout(() => void tick(), refreshMilliseconds)
    return () => { stopped = true; clearTimeout(timer) }
  }, [load, refreshMilliseconds])

  return { ...state, refresh: load }
}

export function useDiagnosticPolicy() {
  return useResource<DiagnosticPolicy>("diagnostics-policy", diagnosticsApi.getDiagnosticPolicy)
}

export function useDiagnosticTargets() {
  return useResource<DiagnosticTarget[]>("diagnostics-targets", diagnosticsApi.listDiagnosticTargets)
}

export function useDiagnosticCapabilities(refreshMilliseconds?: number) {
  return useResource<DiagnosticCapability[]>("diagnostics-capabilities", diagnosticsApi.listDiagnosticCapabilities, refreshMilliseconds)
}

export function useDiagnosticJobs(filter: DiagnosticJobFilter, refreshMilliseconds?: number) {
  const key = useMemo(() => JSON.stringify(filter), [filter])
  return useResource<DiagnosticJob[]>(key, () => diagnosticsApi.listDiagnosticJobs(filter), refreshMilliseconds)
}

export function useDiagnosticJobDetail(jobId: string | null, refreshMilliseconds?: number) {
  return useResource<DiagnosticJobDetail>(jobId ?? "", () => {
    if (!jobId) return Promise.resolve({ job: null, hops: null } as unknown as DiagnosticJobDetail)
    return diagnosticsApi.getDiagnosticJob(jobId)
  }, jobId ? refreshMilliseconds : undefined)
}

interface SeriesResult {
  series: (DiagnosticSeries | null)[]
  loading: boolean
  error: string | null
}

export function useDiagnosticSerieses(jobId: string | null, specs: { metric: string; role?: string; hop?: number; stat?: string }[]): SeriesResult {
  const key = useMemo(() => `${jobId ?? ""}|${JSON.stringify(specs)}`, [jobId, specs])
  const [state, setState] = useState<SeriesResult>({ series: [], loading: jobId !== null, error: null })
  const specsRef = useRef(specs)
  specsRef.current = specs

  useEffect(() => {
    if (!jobId) {
      setState({ series: [], loading: false, error: null })
      return undefined
    }
    let active = true
    setState((previous) => ({ series: previous.series, loading: true, error: null }))
    void (async () => {
      try {
        const loaded = await Promise.all(specsRef.current.map((spec) => diagnosticsApi.getDiagnosticSeries(jobId, spec)))
        if (active) setState({ series: loaded, loading: false, error: null })
      } catch (error) {
        if (active) setState((previous) => ({ series: previous.series, loading: false, error: messageOf(error) }))
      }
    })()
    return () => {
      active = false
    }
  }, [jobId, key])

  return state
}
