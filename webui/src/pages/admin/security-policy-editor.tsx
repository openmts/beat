import { useState } from "react"
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Checkbox } from "@/components/ui/checkbox"
import { Field, FieldDescription, FieldGroup, FieldLabel } from "@/components/ui/field"
import { Input } from "@/components/ui/input"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { Skeleton } from "@/components/ui/skeleton"
import { useConfirmedOperation } from "@/hooks/use-confirmed-operation"
import { updateSecurityPolicy } from "@/lib/security-api"
import { OperationConfirm } from "@/pages/admin/operation-confirm"
import { useSecurityEventsText, useSecurityLabel } from "@/pages/admin/security-events-text"
import type { SecurityNotificationSettings, SecurityNotificationSettingsWrite } from "@/types/security"

const bounds = {
  lookback: [1, 365], failures: [1, 100], window: [60, 86_400], totp: [1, 100], retention: [30, 730],
} as const

function toWrite(settings: SecurityNotificationSettings): SecurityNotificationSettingsWrite {
  return {
    expected_revision: settings.revision,
    enabled_event_kinds: [...settings.enabled_event_kinds],
    privacy_level: settings.privacy_level,
    lookback_days: settings.lookback_days,
    threshold_failures: settings.threshold_failures,
    threshold_window_seconds: settings.threshold_window_seconds,
    invalid_totp_threshold: settings.invalid_totp_threshold,
    retention_days: settings.retention_days,
  }
}

export function SecurityPolicyEditor({ settings, error, onSaved }: {
  settings: SecurityNotificationSettings | null
  error: string
  onSaved: () => void
}) {
  const t = useSecurityEventsText()
  const [notice, setNotice] = useState("")
  return <Card>
    <CardHeader>
      <CardTitle>{t("policy_title")}</CardTitle>
      <CardDescription>{t("policy_description")}</CardDescription>
    </CardHeader>
    <CardContent className="space-y-4">
      {error && <Alert variant="destructive" role="alert"><AlertDescription>{error}</AlertDescription></Alert>}
      {notice && <Alert role="status"><AlertDescription>{notice}</AlertDescription></Alert>}
      {settings
        ? <PolicyForm settings={settings} onSaved={() => { setNotice(t("saved")); onSaved() }} />
        : <Skeleton className="h-48 w-full" />}
    </CardContent>
  </Card>
}

function PolicyForm({ settings, onSaved }: { settings: SecurityNotificationSettings; onSaved: (next: SecurityNotificationSettings) => void }) {
  const t = useSecurityEventsText()
  const label = useSecurityLabel()
  const flow = useConfirmedOperation()
  const [draft, setDraft] = useState<SecurityNotificationSettingsWrite>(() => toWrite(settings))
  const [error, setError] = useState("")
  const change = (update: Partial<SecurityNotificationSettingsWrite>) => setDraft(current => ({ ...current, ...update }))
  const toggleKind = (kind: string, enabled: boolean) => change({
    enabled_event_kinds: enabled
      ? [...draft.enabled_event_kinds.filter(value => value !== kind), kind]
      : draft.enabled_event_kinds.filter(value => value !== kind),
  })
  const privacyItems = ["coarse", "full_source"].map(value => ({ value, label: label("privacy", value) }))
  const save = () => {
    if (!draft.enabled_event_kinds.length) { setError(t("enabled_required")); return }
    setError("")
    flow.request({
      title: t("save"), description: t("policy_description"),
      execute: async () => { onSaved(await updateSecurityPolicy(draft)) },
    })
  }
  return <div className="space-y-5">
    <FieldGroup>
      <Field>
        <FieldLabel>{t("enabled_kinds")}</FieldLabel>
        {["security.login_succeeded", "security.login_suspicious"].map(kind => (
          <Field key={kind} orientation="horizontal">
            <Checkbox checked={draft.enabled_event_kinds.includes(kind)} disabled={flow.busy}
              onCheckedChange={checked => toggleKind(kind, checked === true)} />
            <FieldLabel>{label("kind", kind)}</FieldLabel>
          </Field>
        ))}
      </Field>
      <Field>
        <FieldLabel htmlFor="security-privacy">{t("privacy")}</FieldLabel>
        <Select items={privacyItems} value={draft.privacy_level}
          onValueChange={value => change({ privacy_level: value ?? "coarse" })}>
          <SelectTrigger id="security-privacy" className="w-full"><SelectValue /></SelectTrigger>
          <SelectContent>{privacyItems.map(item => <SelectItem key={item.value} value={item.value}>{item.label}</SelectItem>)}</SelectContent>
        </Select>
      </Field>
      <div className="grid gap-5 sm:grid-cols-2">
        <NumberField id="security-lookback" labelKey="lookback" value={draft.lookback_days} bounds={bounds.lookback} disabled={flow.busy} onChange={lookback_days => change({ lookback_days })} />
        <NumberField id="security-failures" labelKey="threshold_failures" value={draft.threshold_failures} bounds={bounds.failures} disabled={flow.busy} onChange={threshold_failures => change({ threshold_failures })} />
        <NumberField id="security-window" labelKey="threshold_window" value={draft.threshold_window_seconds} bounds={bounds.window} disabled={flow.busy} onChange={threshold_window_seconds => change({ threshold_window_seconds })} />
        <NumberField id="security-totp" labelKey="totp_threshold" value={draft.invalid_totp_threshold} bounds={bounds.totp} disabled={flow.busy} onChange={invalid_totp_threshold => change({ invalid_totp_threshold })} />
        <NumberField id="security-retention" labelKey="retention" value={draft.retention_days} bounds={bounds.retention} disabled={flow.busy} onChange={retention_days => change({ retention_days })} />
      </div>
    </FieldGroup>
    <Alert>
      <AlertTitle>{t("preview_title")}</AlertTitle>
      <AlertDescription>{draft.privacy_level === "full_source" ? t("preview_full") : t("preview_coarse")}</AlertDescription>
    </Alert>
    {error && <Alert variant="destructive" role="alert"><AlertDescription>{error}</AlertDescription></Alert>}
    <Button disabled={flow.busy} onClick={save}>{t("save")}</Button>
    <OperationConfirm flow={flow} />
  </div>
}

function NumberField({ id, labelKey, value, bounds, disabled, onChange }: {
  id: string
  labelKey: Parameters<ReturnType<typeof useSecurityEventsText>>[0]
  value: number
  bounds: readonly [number, number]
  disabled: boolean
  onChange: (value: number) => void
}) {
  const t = useSecurityEventsText()
  const [low, high] = bounds
  return <Field>
    <FieldLabel htmlFor={id}>{t(labelKey)}</FieldLabel>
    <Input id={id} type="number" min={low} max={high} value={value} disabled={disabled}
      onChange={event => {
        const parsed = Number.parseInt(event.target.value, 10)
        if (Number.isFinite(parsed)) onChange(Math.min(high, Math.max(low, parsed)))
      }} />
    <FieldDescription>{low}–{high}</FieldDescription>
  </Field>
}
