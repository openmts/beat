import { useEffect, useRef, useState } from "react"
import { useAuth } from "@/context/auth"
import { reauthenticateAdmin } from "@/lib/security-api"

export interface NotificationOwnerAction {
  title: string
  description: string
  execute: () => Promise<void>
  confirm?: boolean
  duplicate?: boolean
}

export function useNotificationOwnerAction() {
  const { principal } = useAuth()
  const [action, setAction] = useState<NotificationOwnerAction | null>(null)
  const [error, setError] = useState("")
  const [busy, setBusy] = useState(false)
  const mounted = useRef(true)
  const pending = useRef(false)
  const proofUntil = useRef(0)
  useEffect(() => { mounted.current = true; return () => { mounted.current = false } }, [])
  const owner = principal?.user.role === "owner"
  const until = Math.max(proofUntil.current, Date.parse(principal?.session.reauthenticated_until ?? "") || 0)
  const needsProof = until <= Date.now()
  const execute = async (next: NotificationOwnerAction, proof?: { password: string; code: string }) => {
    if (pending.current) return
    pending.current = true; setBusy(true); setError("")
    try {
      if (proof) {
        const session = await reauthenticateAdmin(proof.password, proof.code)
        proofUntil.current = Date.parse(session.reauthenticated_until ?? "") || 0
      }
      await next.execute()
      if (mounted.current) setAction(null)
    } catch (error) {
      if (mounted.current) setError(error instanceof Error ? error.message : "notification.action_failed")
    } finally { pending.current = false; if (mounted.current) setBusy(false) }
  }
  const request = (next: NotificationOwnerAction) => {
    if (!owner || pending.current) return
    setError("")
    if (until <= Date.now() || next.confirm || next.duplicate) setAction(next)
    else void execute(next)
  }
  const confirm = async (password: string, code: string) => {
    if (action) await execute(action, needsProof ? { password, code } : undefined)
  }
  return { owner, action, error, busy, needsProof, request, confirm, close: () => { if (!pending.current) setAction(null) }, totp: Boolean(principal?.user.totp_enabled_at) }
}
