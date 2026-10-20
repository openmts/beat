import { useEffect, useRef, useState } from "react"
import { useAuth } from "@/context/auth"
import { APIError } from "@/lib/api-error"
import { reauthenticateAdmin } from "@/lib/security-api"
export interface ConfirmedOperation { title: string; description: string; execute: () => Promise<void>; uncertain?: boolean; requiresRecent?: boolean; forceProof?: boolean }
export function useConfirmedOperation() {
 const { principal } = useAuth()
 const [action, setAction] = useState<ConfirmedOperation | null>(null)
 const [busy, setBusy] = useState(false)
 const [error, setError] = useState("")
 const [forcedProof, setForcedProof] = useState(false)
 const pending = useRef(false), mounted = useRef(true)
 const currentIdentity = useRef("")
 const identityKey = `${principal?.user.id ?? ""}/${principal?.session.id ?? ""}`
 currentIdentity.current = identityKey
 useEffect(() => { setAction(null); setError(""); setForcedProof(false) }, [identityKey])
 const until = Date.parse(principal?.session.reauthenticated_until ?? "") || 0
 useEffect(() => { mounted.current = true; return () => { mounted.current = false } }, [])
 const confirm = async (password: string, code: string) => {
  if (!action || pending.current) return
  const identity = currentIdentity.current
  pending.current = true; setBusy(true); setError("")
  try {
   if (action.requiresRecent !== false && (forcedProof || until <= Date.now())) { await reauthenticateAdmin(password, code); setForcedProof(false) }
   if (!mounted.current || currentIdentity.current !== identity) return
   await action.execute()
   if (mounted.current) setAction(null)
  } catch (error) { if (mounted.current) { if (error instanceof APIError && error.status === 428) setForcedProof(true); setError(error instanceof Error ? error.message : "Operation failed") } }
  finally { pending.current = false; if (mounted.current) setBusy(false) }
 }
 return { action, busy, error, needsProof: action?.requiresRecent !== false && (forcedProof || until <= Date.now()), totp: Boolean(principal?.user.totp_enabled_at), confirm,
  request: (next: ConfirmedOperation) => { if (!pending.current) { setError(""); setForcedProof(Boolean(next.forceProof)); setAction(next) } },
  close: () => { if (!pending.current) { setAction(null); setError("") } } }
}
