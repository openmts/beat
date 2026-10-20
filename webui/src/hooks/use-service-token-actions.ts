import { useCallback, useEffect, useRef, useState } from "react"
import { useAuth } from "@/context/auth"
import { reauthenticateAdmin } from "@/lib/security-api"
import type { ServiceTokenCredential } from "@/types/service-tokens"

export interface ServiceTokenAction {
  title: string
  description: string
  execute: () => Promise<ServiceTokenCredential | void>
}

// useServiceTokenActions 在近期重认证过期时先收集身份证明，再执行管理动作。
export function useServiceTokenActions() {
  const { principal } = useAuth()
  const [action, setAction] = useState<ServiceTokenAction | null>(null)
  const [result, setResult] = useState<ServiceTokenCredential | null>(null)
  const [error, setError] = useState("")
  const [busy, setBusy] = useState(false)
  const mounted = useRef(true)
  const pending = useRef(false)
  const proofUntil = useRef(0)
  useEffect(() => {
    mounted.current = true
    return () => { mounted.current = false }
  }, [])
  const owner = principal?.user.role === "owner"
  const until = Math.max(proofUntil.current, Date.parse(principal?.session.reauthenticated_until ?? "") || 0)
  const needsProof = until <= Date.now()
  const execute = useCallback(async (next: ServiceTokenAction, proof?: { password: string; code: string }) => {
    if (pending.current) return
    pending.current = true
    setBusy(true)
    setError("")
    try {
      if (proof) {
        const session = await reauthenticateAdmin(proof.password, proof.code)
        proofUntil.current = Date.parse(session.reauthenticated_until ?? "") || 0
      }
      const outcome = await next.execute()
      if (mounted.current) {
        setResult(outcome ?? null)
        setAction(null)
      }
    } catch (failure) {
      if (mounted.current) setError(failure instanceof Error ? failure.message : "security.tokens.error")
    } finally {
      pending.current = false
      if (mounted.current) setBusy(false)
    }
  }, [])
  const request = useCallback((next: ServiceTokenAction, confirm: boolean) => {
    if (!owner || pending.current) return
    setError("")
    setResult(null)
    if (needsProof || confirm) setAction(next)
    else void execute(next)
  }, [execute, needsProof, owner])
  const confirm = useCallback(async (password: string, code: string) => {
    if (action) await execute(action, needsProof ? { password, code } : undefined)
  }, [action, execute, needsProof])
  return {
    owner, action, result, error, busy, needsProof, totp: Boolean(principal?.user.totp_enabled_at),
    request, confirm,
    close: () => { if (!pending.current) setAction(null) },
    dismissResult: () => setResult(null),
  }
}
