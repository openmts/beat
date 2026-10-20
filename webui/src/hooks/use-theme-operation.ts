import { useRef, useState } from "react"
import { useLocale } from "@/context/locale"
import { APIError } from "@/lib/api-error"
import { reauthenticateAdmin } from "@/lib/security-api"

export interface ThemeOperation { label: string; description: string; run: () => Promise<unknown> }

export function useThemeOperation() {
  const { t } = useLocale()
  const [operation, setOperation] = useState<ThemeOperation | null>(null)
  const [proof, setProof] = useState(false)
  const [error, setError] = useState("")
  const [loading, setLoading] = useState(false)
  const busy = useRef(false)
  const execute = async (action: ThemeOperation, credentials?: { password: string; code: string }) => {
    if (busy.current) return
    busy.current = true
    setLoading(true)
    setError("")
    try {
      if (credentials) await reauthenticateAdmin(credentials.password, credentials.code)
      await action.run()
      setOperation(null)
      setProof(false)
    } catch (cause) {
      setOperation(action)
      if (cause instanceof APIError && cause.status === 428) setProof(true)
      else setError(cause instanceof Error ? cause.message : t("app.error"))
    } finally { busy.current = false; setLoading(false) }
  }
  const request = (action: ThemeOperation, confirm = true) => {
    if (busy.current) return
    setError("")
    setProof(false)
    setOperation(action)
    if (!confirm) void execute(action)
  }
  const cancel = () => { if (!busy.current) { setOperation(null); setProof(false); setError("") } }
  const submit = (password: string, code: string) => {
    if (operation) void execute(operation, proof ? { password, code } : undefined)
  }
  return { operation, proof, error, loading, request, submit, cancel }
}
