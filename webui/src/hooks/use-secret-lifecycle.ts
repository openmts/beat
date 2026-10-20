import { useCallback, useEffect, useRef, useState } from "react"
import { useLocale } from "@/context/locale"
import { getApplicationSecretStatus, rotateApplicationDataKey, type ApplicationSecretStatus } from "@/lib/application-secrets-api"
import { retryAfterDelay } from "@/lib/retry-after"
import { reauthenticateAdmin } from "@/lib/security-api"

const pollInterval = 3_000

function useSecretStatus() {
  const { t } = useLocale()
  const [status, setStatus] = useState<ApplicationSecretStatus | null>(null)
  const [readError, setReadError] = useState("")
  const [loading, setLoading] = useState(true)
  const generation = useRef(0)
  const mounted = useRef(true)
  const refresh = useCallback(async () => {
    if (!mounted.current) return 0
    const request = ++generation.current
    setLoading(true)
    try {
      const value = await getApplicationSecretStatus()
      if (mounted.current && request === generation.current) { setStatus(value); setReadError("") }
      return 0
    } catch (error) {
      if (mounted.current && request === generation.current) setReadError(t("secret.load_error"))
      return retryAfterDelay(error)
    } finally {
      if (mounted.current && request === generation.current) setLoading(false)
    }
  }, [t])
  useEffect(() => {
    mounted.current = true
    void refresh()
    return () => { mounted.current = false }
  }, [refresh])
  useEffect(() => {
    if (status?.rotation !== "running") return
    let stopped = false
    let timer: ReturnType<typeof setTimeout>
    const tick = async () => {
      const retryDelay = await refresh()
      if (!stopped) timer = setTimeout(() => void tick(), Math.max(pollInterval, retryDelay))
    }
    timer = setTimeout(() => void tick(), pollInterval)
    return () => { stopped = true; clearTimeout(timer) }
  }, [status?.rotation, refresh])
  const accept = (value: ApplicationSecretStatus) => {
    if (!mounted.current) return
    generation.current++
    setStatus(value)
    setReadError("")
    setLoading(false)
  }
  return { status, readError, loading, refresh, mounted, accept }
}

export function useSecretLifecycle() {
  const { t } = useLocale()
  const { status, readError, loading, refresh, mounted, accept } = useSecretStatus()
  const [actionError, setActionError] = useState("")
  const [pending, setPending] = useState(false)
  const [confirming, setConfirming] = useState(false)
  const busy = useRef(false)
  const confirm = async (password: string, code: string) => {
    if (busy.current) return
    busy.current = true
    setPending(true)
    setActionError("")
    try {
      await reauthenticateAdmin(password, code)
      const value = await rotateApplicationDataKey()
      if (mounted.current) { accept(value); setConfirming(false) }
    } catch (cause) {
      if (mounted.current) setActionError(t("secret.rotate_error", { reason: cause instanceof Error ? cause.message : t("app.error") }))
      await refresh()
    } finally {
      busy.current = false
      if (mounted.current) setPending(false)
    }
  }
  const canRotate = Boolean(status?.healthy && status.root_available && status.registry_valid
    && status.backfill_complete && status.rotation === "idle" && !readError)
  return { status, loading, pending, confirming, setConfirming, canRotate, refresh, confirm, error: readError || actionError }
}
