import { useCallback, useEffect, useState } from "react"

import { useLocale } from "@/context/locale"
import { useReauthRetry } from "@/hooks/use-reauth-retry"
import { APIError } from "@/lib/api-error"
import { retryAfterDelay } from "@/lib/retry-after"
import {
  cancelMetricErasureJob,
  createMetricErasureJob,
  erasureActive,
  erasureTargetKind,
  listMetricErasureJobs,
  retryMetricErasureJob,
  type MetricErasureJob,
  type MetricErasureScope,
} from "@/lib/metric-erasure-api"

const refreshMilliseconds = 5000
const jobLimit = 20

export interface JobAction {
  job: MetricErasureJob
  kind: "cancel" | "retry"
}

export interface ErasureState {
  jobs: MetricErasureJob[]
  loading: boolean
  pending: boolean
  error: string | null
  message: string | null
  scope: MetricErasureScope
  targetId: string
  setScope: (scope: MetricErasureScope) => void
  setTargetId: (id: string) => void
  submit: (confirmation: string, requestId: string) => Promise<void>
  runAction: (action: JobAction | null, done: () => void) => Promise<void>
  /** 面板必须渲染的确认身份对话框状态；擦除接口全部要求近期重认证。 */
  reauth: ReturnType<typeof useReauthRetry>
}

/** 任务列表与表单共享同一份状态：轮询只在存在活动任务时开启。 */
export function useErasureState(): ErasureState {
  const { t } = useLocale()
  const [jobs, setJobs] = useState<MetricErasureJob[]>([])
  const [loading, setLoading] = useState(true)
  const [pending, setPending] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [message, setMessage] = useState<string | null>(null)
  const [scope, setScopeState] = useState<MetricErasureScope>("node_history")
  const [targetId, setTargetId] = useState("")

  const reauth = useReauthRetry()
  const { run: runWithReauth } = reauth

  const refresh = useCallback(async () => {
    try {
      // 任务列表同样要求近期重认证：428 时整段刷新挂起，确认身份后连同结果一起重放。
      const loaded = await runWithReauth(async () => {
        setJobs(await listMetricErasureJobs({ limit: jobLimit }))
        setError(null)
        return 0
      })
      return loaded ?? 0
    } catch (reason) {
      setError(messageOf(reason, t))
      return retryAfterDelay(reason)
    } finally {
      setLoading(false)
    }
  }, [runWithReauth, t])

  useEffect(() => { void refresh() }, [refresh])
  useEffect(() => {
    if (!jobs.some((job) => erasureActive(job.state))) return
    let stopped = false
    let timer: ReturnType<typeof setTimeout>
    const tick = async () => {
      const retryDelay = await refresh()
      if (!stopped) timer = setTimeout(() => void tick(), Math.max(refreshMilliseconds, retryDelay))
    }
    timer = setTimeout(() => void tick(), refreshMilliseconds)
    return () => { stopped = true; clearTimeout(timer) }
  }, [jobs, refresh])

  const submit = async (confirmation: string, requestId: string) => {
    try {
      // 提交要求近期重认证：428 时把「提交 + 刷新」整体挂起，确认身份后重放，不会重复建任务
      // 也不会丢掉界面状态。
      await runWithReauth(async () => {
        setPending(true)
        try {
          const accepted = await createMetricErasureJob({
            scope,
            target_id: erasureTargetKind(scope) === "none" ? undefined : targetId,
            request_id: requestId,
            confirmation,
          })
          setMessage(accepted.replayed ? t("erase.replayed_message") : t("erase.queued_message"))
          setError(null)
          await refresh()
        } finally {
          setPending(false)
        }
      })
    } catch (reason) {
      setError(messageOf(reason, t))
    }
  }

  const runAction = async (action: JobAction | null, done: () => void) => {
    if (!action) return
    setError(null)
    try {
      await runWithReauth(async () => {
        setPending(true)
        try {
          if (action.kind === "cancel") await cancelMetricErasureJob(action.job.id)
          else await retryMetricErasureJob(action.job.id)
          done()
          await refresh()
        } finally {
          setPending(false)
        }
      })
    } catch (reason) {
      setError(messageOf(reason, t))
    }
  }

  const setScope = (next: MetricErasureScope) => {
    setScopeState(next)
    setTargetId("")
  }

  return { jobs, loading, pending, error, message, scope, targetId, setScope, setTargetId, submit, runAction, reauth }
}


function messageOf(reason: unknown, t: (key: string) => string): string {
  if (reason instanceof APIError) {
    if (reason.status === 409) return t("erase.error_conflict")
    if (reason.status === 404) return t("erase.error_missing")
    if (reason.status === 401 || reason.status === 403 || reason.status === 412) return t("erase.error_forbidden")
    if (reason.status === 428) return t("erase.error_reauth")
  }
  return reason instanceof Error ? reason.message : t("erase.error")
}
