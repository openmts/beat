import { useEffect, useState } from "react"
import { getPublicWorkspaceSources, type PublicWorkspaceSources } from "@/lib/public-workspace-api"
import { useErrorRecovery } from "@/hooks/use-error-recovery"
import { workspaceRecoveryDelay, workspaceRetryDelay } from "@/lib/workspace-retry"

interface SourceState { nodeId?: string; data: PublicWorkspaceSources | null; loading: boolean; error: string | null }

export function usePublicWorkspaceSources(nodeId?: string) {
  const [state, setState] = useState<SourceState>({ data: null, loading: Boolean(nodeId), error: null })
  const [revision, setRevision] = useState(0)
  useEffect(() => {
    if (!nodeId) return
    const controller = new AbortController()
    let attempt = 0
    let recovery = 0
    let timer: number | undefined
    setState({ nodeId, data: null, loading: true, error: null })
    const load = () => {
      void getPublicWorkspaceSources({ nodeId, signal: controller.signal, attempt }).then((data) => {
        if (!controller.signal.aborted) setState({ nodeId, data, loading: false, error: null })
      }).catch((reason: unknown) => {
        if (controller.signal.aborted) return
        // 公共页面常在恢复网络或服务重启后继续驻留；先按退避重试，再落到错误态，随后按慢速节奏
        // 继续重试：升级窗口与限流冷却可能长于退避预算，页面不能停在必须手动刷新的错误态。
        const delay = workspaceRetryDelay(reason, attempt)
        if (delay !== null) {
          attempt += 1
          timer = window.setTimeout(load, delay)
          return
        }
        setState({ nodeId, data: null, loading: false, error: "workspace.load_failed" })
        const recoveryDelay = workspaceRecoveryDelay(reason, recovery)
        if (recoveryDelay === null) {
          controller.abort()
          return
        }
        recovery += 1
        timer = window.setTimeout(load, recoveryDelay)
      })
    }
    load()
    return () => { controller.abort(); if (timer !== undefined) window.clearTimeout(timer) }
  }, [nodeId, revision])
  const visible = state.nodeId === nodeId ? state : { nodeId, data: null, loading: Boolean(nodeId), error: null }
  useErrorRecovery(visible.error !== null, () => setRevision((value) => value + 1))
  return { ...visible, reload: () => setRevision((value) => value + 1) }
}
