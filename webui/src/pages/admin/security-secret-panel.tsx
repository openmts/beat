import { useState, type FormEvent } from "react"
import { CheckIcon, KeyRoundIcon, RefreshCwIcon } from "lucide-react"
import { Alert, AlertDescription } from "@/components/ui/alert"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Field, FieldGroup, FieldLabel } from "@/components/ui/field"
import { Input } from "@/components/ui/input"
import { useAuth } from "@/context/auth"
import { useLocale } from "@/context/locale"
import { useSecretLifecycle } from "@/hooks/use-secret-lifecycle"
import type { ApplicationSecretStatus } from "@/lib/application-secrets-api"

export function SecuritySecretPanel() {
  const { principal } = useAuth()
  if (principal?.user.role !== "owner") return null
  return <SecretPanelContent hasTOTP={Boolean(principal.user.totp_enabled_at)} />
}

function SecretPanelContent({ hasTOTP }: { hasTOTP: boolean }) {
  const { t } = useLocale()
  const flow = useSecretLifecycle()
  return (
    <Card>
      <CardHeader>
        <CardTitle>{t("secret.title")}</CardTitle>
        <CardDescription>{t("secret.description")}</CardDescription>
      </CardHeader>
      <CardContent className="flex flex-col gap-5">
        {flow.error && <Alert variant="destructive" role="alert"><AlertDescription>{flow.error}</AlertDescription></Alert>}
        {flow.status ? <SecretStatus status={flow.status} /> : <p role="status" className="text-sm text-muted-foreground">{t("secret.loading")}</p>}
        {flow.status?.rotation === "running" && <Alert><AlertDescription>{t("secret.running_help")}</AlertDescription></Alert>}
        {flow.status?.rotation === "degraded" && <Alert variant="destructive"><AlertDescription>{t("secret.recovery_help")}</AlertDescription></Alert>}
        <p className="max-w-2xl text-sm text-muted-foreground">{t("secret.rotate_help")}</p>
        {flow.confirming && flow.canRotate ? <SecretIdentityForm hasTOTP={hasTOTP} pending={flow.pending}
          confirm={flow.confirm} cancel={() => flow.setConfirming(false)} /> : <div className="flex flex-wrap gap-2">
          <Button disabled={!flow.canRotate || flow.pending || flow.loading} onClick={() => flow.setConfirming(true)}>
            <KeyRoundIcon data-icon="inline-start" />{t("secret.rotate")}
          </Button>
          <Button variant="outline" disabled={flow.loading || flow.pending} onClick={() => void flow.refresh()}>
            <RefreshCwIcon data-icon="inline-start" />{t("secret.refresh")}
          </Button>
        </div>}
      </CardContent>
    </Card>
  )
}

function SecretStatus({ status }: { status: ApplicationSecretStatus }) {
  const { t } = useLocale()
  const state = status.rotation === "degraded" ? "degraded" : !status.backfill_complete ? "migrating"
    : status.rotation === "running" ? "running" : status.healthy ? "ready" : "unavailable"
  const checks = [
    { key: "root", ok: status.root_available }, { key: "registry", ok: status.registry_valid },
    { key: "migration", ok: status.backfill_complete },
  ]
  return (
    <div className="flex flex-col gap-4" aria-live="polite">
      <div className="flex flex-wrap items-center justify-between gap-4 rounded-md border bg-muted/30 p-4">
        <Badge variant={status.healthy ? "secondary" : "destructive"}>{t(`secret.state_${state}`)}</Badge>
        <dl className="flex gap-8 text-sm">
          <div><dt className="text-muted-foreground">{t("secret.generation")}</dt><dd className="mt-1 font-mono tabular-nums">{status.generation}</dd></div>
          <div><dt className="text-muted-foreground">{t("secret.key_count")}</dt><dd className="mt-1 font-mono tabular-nums">{status.data_keys}</dd></div>
        </dl>
      </div>
      <dl className="grid gap-3 text-sm sm:grid-cols-3">
        {checks.map(check => <div key={check.key} className="flex items-center gap-2">
          {check.ok && <CheckIcon className="size-4 shrink-0 text-muted-foreground" aria-hidden="true" />}
          <dt>{t(`secret.${check.key}`)}</dt><dd className={check.ok ? "text-muted-foreground" : "text-destructive"}>
            {t(check.ok ? "secret.check_ok" : "secret.check_failed")}
          </dd>
        </div>)}
      </dl>
    </div>
  )
}

function SecretIdentityForm({ hasTOTP, pending, confirm, cancel }: {
  hasTOTP: boolean; pending: boolean; confirm: (password: string, code: string) => Promise<void>; cancel: () => void
}) {
  const { t } = useLocale()
  const [password, setPassword] = useState("")
  const [code, setCode] = useState("")
  const submit = async (event: FormEvent) => {
    event.preventDefault()
    await confirm(password, code)
    setPassword("")
    setCode("")
  }
  return (
    <form onSubmit={submit} className="max-w-md rounded-md border p-4">
      <FieldGroup>
        <p className="text-sm text-muted-foreground">{t("secret.confirm_help")}</p>
        <Field>
          <FieldLabel htmlFor="secret-current-password">{t("security.current_password")}</FieldLabel>
          <Input id="secret-current-password" type="password" autoComplete="current-password" required
            value={password} disabled={pending} onChange={event => setPassword(event.target.value)} />
        </Field>
        {hasTOTP && <Field>
          <FieldLabel htmlFor="secret-totp-code">{t("security.totp_code")}</FieldLabel>
          <Input id="secret-totp-code" inputMode="numeric" autoComplete="one-time-code" maxLength={6} required
            value={code} disabled={pending} onChange={event => setCode(event.target.value)} />
        </Field>}
        <div className="flex flex-wrap gap-2">
          <Button type="submit" disabled={pending || !password || (hasTOTP && !/^\d{6}$/.test(code))}>{t("secret.confirm")}</Button>
          <Button type="button" variant="outline" disabled={pending} onClick={cancel}>{t("secret.cancel")}</Button>
        </div>
      </FieldGroup>
    </form>
  )
}
