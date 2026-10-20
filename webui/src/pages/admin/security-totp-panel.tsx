import { useEffect, useRef, useState, type FormEvent } from "react"
import { ShieldCheckIcon, ShieldOffIcon } from "lucide-react"
import { Alert, AlertDescription } from "@/components/ui/alert"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Field, FieldGroup, FieldLabel } from "@/components/ui/field"
import { Input } from "@/components/ui/input"
import { useAuth } from "@/context/auth"
import { useLocale } from "@/context/locale"
import { beginTOTP, disableTOTP, enableTOTP, reauthenticateAdmin } from "@/lib/security-api"
import type { TOTPSetup } from "@/types/security"

export function SecurityTOTPPanel() {
  const { principal, refresh } = useAuth()
  const { t } = useLocale()
  const flow = useTOTPFlow(refresh)
  const enabled = Boolean(principal?.user.totp_enabled_at)

  return (
    <Card>
      <CardHeader>
        <CardTitle>{t("security.two_factor")}</CardTitle>
        <CardDescription>{enabled ? t("security.totp_enabled") : t("security.totp_disabled")}</CardDescription>
      </CardHeader>
      <CardContent>
        <FieldGroup>
          {flow.error && <Alert variant="destructive"><AlertDescription>{flow.error}</AlertDescription></Alert>}
          <TOTPControls enabled={enabled} flow={flow} />
        </FieldGroup>
      </CardContent>
    </Card>
  )
}

const setupLifetime = 5 * 60_000
type PendingSetup = TOTPSetup & { expiresAt: number }
type TOTPFlow = ReturnType<typeof useTOTPFlow>

function useTOTPFlow(refresh: () => Promise<void>) {
  const { t } = useLocale()
  const [setup, setSetup] = useState<PendingSetup | null>(null)
  const [action, setAction] = useState<"begin" | "disable" | null>(null)
  const [code, setCode] = useState("")
  const { loading, error, setError, run } = useTOTPRequest()
  useEffect(() => {
    if (!setup) return
    const timer = setTimeout(() => {
      setSetup(null)
      setCode("")
      setError(t("security.totp_setup_expired"))
    }, Math.max(0, setup.expiresAt - Date.now()))
    return () => clearTimeout(timer)
  }, [setup, setError, t])
  const confirm = (password: string, currentCode: string) => run(async () => {
    await reauthenticateAdmin(password, currentCode)
    if (action === "begin") {
      const result = await beginTOTP()
      setSetup({ ...result, expiresAt: Date.now() + setupLifetime })
    } else {
      await disableTOTP()
      await refresh()
    }
    setAction(null)
  })
  const enable = () => run(async () => {
    await enableTOTP(code)
    setSetup(null)
    setCode("")
    await refresh()
  })
  const cancel = () => {
    setSetup(null)
    setAction(null)
    setCode("")
    setError("")
  }
  return { setup, action, code, setAction, setCode, loading, error, cancel, confirm, enable }
}

function useTOTPRequest() {
  const { t } = useLocale()
  const busy = useRef(false)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState("")
  const run = async (action: () => Promise<void>) => {
    if (busy.current) return
    busy.current = true
    setLoading(true)
    setError("")
    try {
      await action()
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : t("app.error"))
    } finally {
      busy.current = false
      setLoading(false)
    }
  }
  return { loading, error, setError, run }
}

function TOTPControls({ enabled, flow }: { enabled: boolean; flow: TOTPFlow }) {
  const { t } = useLocale()
  if (flow.action) return <TOTPIdentityForm enabled={enabled} flow={flow} />
  if (flow.setup) return <TOTPPendingSetup flow={flow} />
  return enabled ? (
    <Button variant="destructive" disabled={flow.loading} onClick={() => flow.setAction("disable")}>
      <ShieldOffIcon data-icon="inline-start" />{t("security.disable_totp")}
    </Button>
  ) : (
    <Button disabled={flow.loading} onClick={() => flow.setAction("begin")}>
      <ShieldCheckIcon data-icon="inline-start" />{t("security.begin_totp")}
    </Button>
  )
}

function TOTPIdentityForm({ enabled, flow }: { enabled: boolean; flow: TOTPFlow }) {
  const { t } = useLocale()
  const [password, setPassword] = useState("")
  const [currentCode, setCurrentCode] = useState("")
  const submit = async (event: FormEvent) => {
    event.preventDefault()
    await flow.confirm(password, currentCode)
    setPassword("")
    setCurrentCode("")
  }
  return (
    <form onSubmit={submit}>
      <FieldGroup>
        <p className="text-sm text-muted-foreground">{t(`security.totp_confirm_${flow.action}`)}</p>
        <Field>
          <FieldLabel htmlFor="totp-current-password">{t("security.current_password")}</FieldLabel>
          <Input id="totp-current-password" type="password" autoComplete="current-password"
            value={password} disabled={flow.loading} onChange={(event) => setPassword(event.target.value)} required />
        </Field>
        {enabled && <Field>
          <FieldLabel htmlFor="totp-current-code">{t("security.totp_current_code")}</FieldLabel>
          <Input id="totp-current-code" inputMode="numeric" autoComplete="one-time-code" maxLength={6}
            value={currentCode} disabled={flow.loading} onChange={(event) => setCurrentCode(event.target.value)} required />
        </Field>}
        <div className="flex flex-wrap gap-2">
          <Button type="submit" disabled={flow.loading}>{t("security.reauthenticate")}</Button>
          <Button variant="outline" disabled={flow.loading} onClick={flow.cancel}>{t("security.cancel_totp")}</Button>
        </div>
      </FieldGroup>
    </form>
  )
}

function TOTPPendingSetup({ flow }: { flow: TOTPFlow }) {
  const { t } = useLocale()
  return (
    <>
      <Alert><AlertDescription className="break-all">
        {t("security.totp_secret")}: <span className="font-mono">{flow.setup?.secret}</span>
      </AlertDescription></Alert>
      <p className="text-sm text-muted-foreground">{t("security.totp_pending")}</p>
      <Field>
        <FieldLabel htmlFor="enable-totp">{t("security.totp_code")}</FieldLabel>
        <Input id="enable-totp" inputMode="numeric" autoComplete="one-time-code" maxLength={6}
          value={flow.code} disabled={flow.loading} onChange={(event) => flow.setCode(event.target.value)} />
      </Field>
      <div className="flex flex-wrap gap-2">
        <Button disabled={flow.loading || !/^\d{6}$/.test(flow.code)} onClick={() => void flow.enable()}>{t("security.enable_totp")}</Button>
        <Button variant="outline" disabled={flow.loading} onClick={flow.cancel}>{t("security.cancel_totp")}</Button>
      </div>
    </>
  )
}
