import { useCallback, useEffect, useState } from "react"
import { Alert, AlertDescription } from "@/components/ui/alert"
import { Button } from "@/components/ui/button"
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { Field, FieldContent, FieldDescription, FieldGroup, FieldLabel } from "@/components/ui/field"
import { Input } from "@/components/ui/input"
import { Switch } from "@/components/ui/switch"
import { useLocale } from "@/context/locale"
import { getPublicTelemetryPolicy, updatePublicTelemetryPolicy } from "@/lib/api"
import { APIError } from "@/lib/api-error"
import { reauthenticateAdmin } from "@/lib/security-api"
import type { PublicTelemetryPolicy } from "@/types"

interface TelemetryProof {
  password: string
  code: string
}

// PublicTelemetryPanel 是 owner 唯一能开关匿名聚合遥测的地方；保存需要近期重认证。
// 非 owner 读取策略会得到 403，此时整个区块不渲染，避免出现无意义的错误提示。
export function PublicTelemetryPanel() {
  const { t } = useLocale()
  const [policy, setPolicy] = useState<PublicTelemetryPolicy | null>(null)
  const [allowed, setAllowed] = useState(true)
  const [message, setMessage] = useState("")
  const [error, setError] = useState("")
  const [loading, setLoading] = useState(true)
  const [proof, setProof] = useState<TelemetryProof | null>(null)

  const reload = useCallback(async () => {
    setLoading(true)
    try {
      setPolicy(await getPublicTelemetryPolicy())
      setAllowed(true)
      setError("")
    } catch (reason) {
      if (reason instanceof APIError && (reason.status === 401 || reason.status === 403)) {
        setAllowed(false)
        return
      }
      setError(t("settings.telemetry_unavailable"))
    } finally {
      setLoading(false)
    }
  }, [t])

  useEffect(() => {
    void reload()
  }, [reload])

  const save = useCallback(async (credentials?: TelemetryProof) => {
    if (!policy) return
    setMessage("")
    setError("")
    try {
      if (credentials) await reauthenticateAdmin(credentials.password, credentials.code)
      const updated = await updatePublicTelemetryPolicy({ ...policy, revision: policy.revision + 1 })
      setPolicy(updated)
      setMessage(t("settings.telemetry_saved"))
      setProof(null)
    } catch (reason) {
      if (reason instanceof APIError && reason.status === 428) {
        setProof({ password: "", code: "" })
        return
      }
      setProof(null)
      setError(reason instanceof Error ? reason.message : t("settings.telemetry_unavailable"))
    }
  }, [policy, t])

  if (!allowed) return null
  return (
    <section className="mt-8 rounded-lg border p-5">
      <h2 className="text-lg font-semibold">{t("settings.public_telemetry")}</h2>
      <p className="mt-1 text-sm text-muted-foreground">{t("settings.public_telemetry_hint")}</p>
      {(error || message) && (
        <Alert variant={error ? "destructive" : "default"} className="mt-4">
          <AlertDescription>{error || message}</AlertDescription>
        </Alert>
      )}
      <FieldGroup className="mt-4">
        <Field orientation="horizontal">
          <FieldContent><FieldLabel>{t("settings.telemetry_enabled")}</FieldLabel></FieldContent>
          <Switch
            checked={Boolean(policy?.enabled)}
            disabled={loading || !policy}
            aria-label={t("settings.telemetry_enabled")}
            onCheckedChange={(enabled) => setPolicy((current) => (current ? { ...current, enabled } : current))}
          />
        </Field>
        <TelemetryNumber
          id="telemetry-ceiling"
          value={policy?.sample_ceiling ?? 1}
          minimum={1}
          maximum={100000}
          label={t("settings.telemetry_ceiling")}
          hint={t("settings.telemetry_ceiling_hint")}
          disabled={loading || !policy}
          onChange={(sample_ceiling) => setPolicy((current) => (current ? { ...current, sample_ceiling } : current))}
        />
        <TelemetryNumber
          id="telemetry-retention"
          value={policy?.retention_days ?? 1}
          minimum={1}
          maximum={3650}
          label={t("settings.telemetry_retention")}
          hint={t("settings.telemetry_retention_hint")}
          disabled={loading || !policy}
          onChange={(retention_days) => setPolicy((current) => (current ? { ...current, retention_days } : current))}
        />
      </FieldGroup>
      <div className="mt-4 flex justify-end">
        <Button type="button" aria-label={t("settings.telemetry_save")} disabled={loading || !policy}
          onClick={() => void save()}>{t("app.save")}</Button>
      </div>
      {proof && (
        <TelemetryProofDialog
          proof={proof}
          change={setProof}
          cancel={() => setProof(null)}
          submit={(credentials) => void save(credentials)}
        />
      )}
    </section>
  )
}

function TelemetryProofDialog({ proof, change, cancel, submit }: {
  proof: TelemetryProof
  change: (next: TelemetryProof) => void
  cancel: () => void
  submit: (credentials: TelemetryProof) => void
}) {
  const { t } = useLocale()
  return (
    <Dialog open onOpenChange={(open) => { if (!open) cancel() }}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>{t("settings.public_telemetry")}</DialogTitle>
          <DialogDescription>{t("settings.telemetry_proof_hint")}</DialogDescription>
        </DialogHeader>
        <FieldGroup>
          <Field>
            <FieldLabel htmlFor="telemetry-proof-password">{t("settings.telemetry_password")}</FieldLabel>
            <Input id="telemetry-proof-password" type="password" autoComplete="current-password"
              value={proof.password} onChange={(event) => change({ ...proof, password: event.target.value })} />
          </Field>
          <Field>
            <FieldLabel htmlFor="telemetry-proof-code">{t("settings.telemetry_code")}</FieldLabel>
            <Input id="telemetry-proof-code" inputMode="numeric" autoComplete="one-time-code" maxLength={6}
              value={proof.code} onChange={(event) => change({ ...proof, code: event.target.value })} />
          </Field>
        </FieldGroup>
        <DialogFooter>
          <Button type="button" variant="outline" onClick={cancel}>{t("settings.telemetry_cancel")}</Button>
          <Button type="button" disabled={!proof.password} onClick={() => submit(proof)}>{t("settings.telemetry_confirm_action")}</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

function TelemetryNumber({ id, value, minimum, maximum, label, hint, disabled, onChange }: {
  id: string
  value: number
  minimum: number
  maximum: number
  label: string
  hint: string
  disabled: boolean
  onChange: (value: number) => void
}) {
  return (
    <Field>
      <FieldLabel htmlFor={id}>{label}</FieldLabel>
      <Input
        id={id}
        type="number"
        min={minimum}
        max={maximum}
        value={value}
        disabled={disabled}
        onChange={(event) => {
          const parsed = Number.parseInt(event.target.value, 10)
          if (Number.isFinite(parsed)) onChange(Math.min(Math.max(parsed, minimum), maximum))
        }}
      />
      <FieldDescription>{hint}</FieldDescription>
    </Field>
  )
}
