import { useEffect, useState } from "react"
import { getWorkspaceSources, type WorkspaceSources } from "@/lib/metric-workspace-api"
import { useErrorRecovery } from "@/hooks/use-error-recovery"
import { workspaceRecoveryDelay, workspaceRetryDelay } from "@/lib/workspace-retry"

export function useWorkspaceSources() {
  const [sources, setSources] = useState<WorkspaceSources | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [revision, setRevision] = useState(0)
  useEffect(() => {
    const controller = new AbortController()
    let attempt = 0
    let recovery = 0
    let timer: number | undefined
    setLoading(true)
    setError(null)
    const load = () => {
      getWorkspaceSources(controller.signal, attempt).then((value) => {
        // 恢复重试成功时必须同时清掉错误态，否则页面会一直挂着上一次失败的提示。
        if (!controller.signal.aborted) { setSources(value); setLoading(false); setError(null) }
      }).catch((reason: unknown) => {
        if (controller.signal.aborted) return
        // 后台工作区同样会撞上重启、限流或瞬时抖动；先按退避重试，再落到错误态，随后按慢速
        // 节奏继续重试，避免页面停在必须手动点重试或刷新的错误态。
        const delay = workspaceRetryDelay(reason, attempt)
        if (delay !== null) {
          attempt += 1
          timer = window.setTimeout(load, delay)
          return
        }
        setError(reason instanceof Error ? reason.message : "workspace.load_failed")
        setLoading(false)
        const recoveryDelay = workspaceRecoveryDelay(reason, recovery)
        if (recoveryDelay === null) return
        recovery += 1
        timer = window.setTimeout(load, recoveryDelay)
      })
    }
    load()
    return () => { controller.abort(); if (timer !== undefined) window.clearTimeout(timer) }
  }, [revision])
  useErrorRecovery(error !== null, () => setRevision((value) => value + 1))
  return { sources, loading, error, reload: () => setRevision((value) => value + 1) }
}
