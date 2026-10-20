import { useCallback, useRef, useState } from "react"
import { useAuth } from "@/context/auth"
import { useLocale } from "@/context/locale"
import { APIError } from "@/lib/api-error"
import { reauthenticateAdmin } from "@/lib/security-api"

interface PendingAction { run: () => Promise<unknown> }

/**
 * 管理端敏感操作统一处理 428「需要近期重认证」：被拦下的动作整体（含成功后的刷新与关闭）
 * 存起来，确认身份后原样重放。动作自身的错误仍抛给调用方，保持各面板原有的错误展示。
 */
export function useReauthRetry() {
  const { principal } = useAuth()
  const { t } = useLocale()
  const [pending, setPending] = useState<PendingAction | null>(null)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState("")
  const current = useRef<PendingAction | null>(null)
  current.current = pending

  const run = useCallback(async <T,>(action: () => Promise<T>): Promise<T | undefined> => {
    try {
      return await action()
    } catch (failure) {
      if (failure instanceof APIError && failure.status === 428) {
        setError("")
        setPending({ run: action })
        return undefined
      }
      throw failure
    }
  }, [])

  const confirm = useCallback(async (password: string, code: string) => {
    setBusy(true)
    setError("")
    try {
      await reauthenticateAdmin(password, code)
      const next = current.current
      setPending(null)
      await next?.run()
    } catch (failure) {
      setError(failure instanceof Error ? failure.message : t("reauth.failed"))
    } finally {
      setBusy(false)
    }
  }, [t])

  return {
    run, confirm, pending: pending !== null, busy, error,
    close: () => { if (!busy) { setPending(null); setError("") } },
    codeRequired: Boolean(principal?.user.totp_enabled_at),
  }
}
