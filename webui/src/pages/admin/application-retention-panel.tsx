import { useCallback, useEffect, useMemo, useState, type FormEvent } from "react"
import { PlayIcon, RefreshCwIcon, SaveIcon } from "lucide-react"

import { Alert, AlertDescription } from "@/components/ui/alert"
import { Button } from "@/components/ui/button"
import {
  Field,
  FieldContent,
  FieldDescription,
  FieldGroup,
  FieldLabel,
} from "@/components/ui/field"
import { Input } from "@/components/ui/input"
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
  type SelectOption,
} from "@/components/ui/select"
import { Switch } from "@/components/ui/switch"
import { useLocale } from "@/context/locale"
import {
  getApplicationRetention,
  runApplicationRetention,
  updateApplicationRetention,
} from "@/lib/application-retention-api"
import type {
  ApplicationRetentionOverview,
  ApplicationRetentionSettings,
} from "@/types/application-retention"
import { retryAfterDelay } from "@/lib/retry-after"
import { formatDateTime } from "@/lib/intl-format"

const policyFields = "application_retention"

function ApplicationRetentionPanel() {
  const { t } = useLocale()
  const [overview, setOverview] = useState<ApplicationRetentionOverview | null>(null)
  const [form, setForm] = useState<ApplicationRetentionSettings | null>(null)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [message, setMessage] = useState<string | null>(null)

  const load = useCallback(async () => {
    try {
      const next = await getApplicationRetention()
      setOverview(next)
      setForm(next.settings)
      setError(null)
      return 0
    } catch (reason) {
      setError(errorMessage(reason, t("app.error")))
      return retryAfterDelay(reason)
    }
  }, [t])

  useEffect(() => { void load() }, [load])
  useEffect(() => {
    if (!overview?.status.running) return
    let stopped = false
    let timer: ReturnType<typeof setTimeout>
    const tick = async () => {
      const retryDelay = await load()
      if (!stopped) timer = setTimeout(() => void tick(), Math.max(2000, retryDelay))
    }
    timer = setTimeout(() => void tick(), 2000)
    return () => { stopped = true; clearTimeout(timer) }
  }, [load, overview?.status.running])

  const patch = useCallback((next: Partial<ApplicationRetentionSettings>) => {
    setForm((current) => (current ? { ...current, ...next } : current))
  }, [])

  const save = async (event: FormEvent) => {
    event.preventDefault()
    if (!form) return
    setBusy(true)
    setMessage(null)
    try {
      const settings = await updateApplicationRetention(form)
      setForm(settings)
      setOverview((current) => (current ? { ...current, settings } : current))
      setMessage(t(`${policyFields}.saved`))
      setError(null)
    } catch (reason) {
      setError(errorMessage(reason, t("app.error")))
    } finally {
      setBusy(false)
    }
  }

  const run = async () => {
    if (!window.confirm(t(`${policyFields}.confirm`))) return
    setBusy(true)
    setMessage(null)
    try {
      await runApplicationRetention()
      setMessage(t(`${policyFields}.started`))
      await load()
    } catch (reason) {
      setError(errorMessage(reason, t("app.error")))
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="space-y-5">
      {error && <Alert variant="destructive"><AlertDescription>{error}</AlertDescription></Alert>}
      {message && <Alert><AlertDescription>{message}</AlertDescription></Alert>}
      <FixedWindows overview={overview} />
      <RunSummary overview={overview} />
      <form onSubmit={(event) => void save(event)} className="space-y-5">
        <PolicyFields form={form} busy={busy} onChange={patch} />
        <div className="flex flex-wrap justify-end gap-2 border-t pt-5">
          <Button
            type="button"
            variant="destructive"
            disabled={busy || !overview || overview.status.running}
            onClick={() => void run()}
          >
            {overview?.status.running ? <RefreshCwIcon className="animate-spin" /> : <PlayIcon />}
            {t(overview?.status.running ? `${policyFields}.running` : `${policyFields}.run`)}
          </Button>
          <Button type="submit" disabled={busy || !form}><SaveIcon />{t("app.save")}</Button>
        </div>
      </form>
    </div>
  )
}

function PolicyFields(props: {
  form: ApplicationRetentionSettings | null
  busy: boolean
  onChange: (patch: Partial<ApplicationRetentionSettings>) => void
}) {
  const { t } = useLocale()
  const { form, busy, onChange } = props
  const hours = useMemo<SelectOption[]>(
    () => Array.from({ length: 24 }, (_, hour) => ({
      value: String(hour),
      label: `${String(hour).padStart(2, "0")}:00 UTC`,
    })),
    [],
  )
  return (
    <FieldGroup>
      <Field>
        <FieldLabel htmlFor="alert-event-days">{t(`${policyFields}.alert_days`)}</FieldLabel>
        <DayInput
          id="alert-event-days"
          value={form?.alert_event_days ?? 180}
          disabled={!form || busy}
          onChange={(alert_event_days) => onChange({ alert_event_days })}
        />
        <FieldDescription>{t(`${policyFields}.alert_hint`)}</FieldDescription>
      </Field>

      <Field>
        <FieldLabel htmlFor="audit-event-days">{t(`${policyFields}.audit_days`)}</FieldLabel>
        <DayInput
          id="audit-event-days"
          value={form?.audit_event_days ?? 180}
          disabled={!form || busy}
          onChange={(audit_event_days) => onChange({ audit_event_days })}
        />
        <FieldDescription>{t(`${policyFields}.audit_hint`)}</FieldDescription>
      </Field>

      <Field orientation="horizontal">
        <FieldContent>
          <FieldLabel htmlFor="automatic-cleanup">{t(`${policyFields}.automatic`)}</FieldLabel>
          <FieldDescription>{t(`${policyFields}.automatic_hint`)}</FieldDescription>
        </FieldContent>
        <Switch
          id="automatic-cleanup"
          checked={form?.auto_cleanup_enabled ?? false}
          disabled={!form || busy}
          onCheckedChange={(auto_cleanup_enabled) => onChange({ auto_cleanup_enabled })}
        />
      </Field>

      <Field>
        <FieldLabel htmlFor="cleanup-hour">{t(`${policyFields}.hour`)}</FieldLabel>
        <Select
          items={hours}
          value={String(form?.cleanup_hour_utc ?? 3)}
          disabled={!form || busy || !form.auto_cleanup_enabled}
          onValueChange={(value) => onChange({ cleanup_hour_utc: Number(value) })}
        >
          <SelectTrigger id="cleanup-hour" className="w-full max-w-xs"><SelectValue /></SelectTrigger>
          <SelectContent>
            <SelectGroup>
              {hours.map((hour) => (
                <SelectItem key={hour.value} value={hour.value}>{hour.label}</SelectItem>
              ))}
            </SelectGroup>
          </SelectContent>
        </Select>
        <FieldDescription>{t(`${policyFields}.hour_hint`)}</FieldDescription>
      </Field>
    </FieldGroup>
  )
}

function DayInput(props: {
  id: string
  value: number
  disabled: boolean
  onChange: (value: number) => void
}) {
  const { t } = useLocale()
  return (
    <div className="flex max-w-xs items-center gap-2">
      <Input
        id={props.id}
        type="number"
        min={1}
        max={3650}
        value={props.value}
        disabled={props.disabled}
        onChange={(event) => props.onChange(Number(event.target.value))}
      />
      <span className="text-sm text-muted-foreground">{t(`${policyFields}.days`)}</span>
    </div>
  )
}

function FixedWindows({ overview }: { overview: ApplicationRetentionOverview | null }) {
  const { t } = useLocale()
  return (
    <dl className="grid overflow-hidden rounded-md border sm:grid-cols-2">
      <div className="border-b p-3 sm:border-b-0 sm:border-r">
        <dt className="text-xs text-muted-foreground">{t(`${policyFields}.sessions`)}</dt>
        <dd className="mt-1 text-sm font-medium">
          {t(`${policyFields}.sessions_value`, { days: overview?.session_retention_days ?? 30 })}
        </dd>
        <p className="mt-1 text-xs text-muted-foreground">{t(`${policyFields}.sessions_hint`)}</p>
      </div>
      <div className="p-3">
        <dt className="text-xs text-muted-foreground">{t(`${policyFields}.deleted`)}</dt>
        <dd className="mt-1 text-sm font-medium">
          {t(`${policyFields}.deleted_value`, {
            alerts: overview?.status.deleted_alert_events ?? 0,
            audits: overview?.status.deleted_audit_events ?? 0,
            sessions: overview?.status.deleted_sessions ?? 0,
          })}
        </dd>
      </div>
    </dl>
  )
}

function RunSummary({ overview }: { overview: ApplicationRetentionOverview | null }) {
  const { locale, t } = useLocale()
  const status = overview?.status
  if (!status) return null
  const state = t(`${policyFields}.${status.last_status}`)
  const trigger = status.last_trigger === "manual" ? t(`${policyFields}.manual`)
    : status.last_trigger === "automatic" ? t(`${policyFields}.automatic_trigger`) : ""
  return (
    <div className="grid gap-2 text-sm text-muted-foreground sm:grid-cols-3">
      <p>
        <span className="text-foreground">{t(`${policyFields}.last_run`)}:</span>{" "}
        {state}{trigger ? ` · ${trigger}` : ""}
      </p>
      <p><span className="text-foreground">{t(`${policyFields}.started_at`)}:</span> {formatDate(status.last_started_at, locale)}</p>
      <p><span className="text-foreground">{t(`${policyFields}.completed_at`)}:</span> {formatDate(status.last_completed_at, locale)}</p>
      {status.last_error_code && (
        <p className="text-destructive sm:col-span-3">
          {t(`${policyFields}.error_${status.last_error_code}`)}
        </p>
      )}
    </div>
  )
}

function formatDate(value: string | null, locale: string): string {
  if (!value) return "--"
  return formatDateTime(locale, value)
}

function errorMessage(reason: unknown, fallback: string): string {
  return reason instanceof Error ? reason.message : fallback
}

export default ApplicationRetentionPanel
export { ApplicationRetentionPanel }
