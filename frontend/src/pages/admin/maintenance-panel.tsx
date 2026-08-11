import { useCallback, useEffect, useMemo, useState, type FormEvent } from "react"
import { DatabaseIcon, PlayIcon, RefreshCwIcon, SaveIcon } from "lucide-react"
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
  getMaintenanceOverview,
  startMaintenance,
  updateMaintenanceSettings,
} from "@/lib/api"
import { formatBytes } from "@/lib/metric-format"
import type { MaintenanceOverview, MaintenanceSettings } from "@/types"

function MaintenancePanel() {
  const { locale, t } = useLocale()
  const [overview, setOverview] = useState<MaintenanceOverview | null>(null)
  const [form, setForm] = useState<MaintenanceSettings | null>(null)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [message, setMessage] = useState<string | null>(null)

  const load = useCallback(async () => {
    try {
      const next = await getMaintenanceOverview()
      setOverview(next)
      setForm(next.settings)
      setError(null)
    } catch (reason) {
      setError(errorMessage(reason, t("app.error")))
    }
  }, [t])

  useEffect(() => { void load() }, [load])
  useEffect(() => {
    if (!overview?.status.running) return
    const timer = window.setInterval(() => void load(), 2000)
    return () => window.clearInterval(timer)
  }, [load, overview?.status.running])

  const hours = useMemo<SelectOption[]>(() => Array.from({ length: 24 }, (_, hour) => ({
    value: String(hour), label: `${String(hour).padStart(2, "0")}:00 UTC`,
  })), [])

  const save = async (event: FormEvent) => {
    event.preventDefault()
    if (!form) return
    setBusy(true)
    setMessage(null)
    try {
      const settings = await updateMaintenanceSettings(form)
      setForm(settings)
      setOverview((current) => current ? { ...current, settings } : current)
      setMessage(t("maintenance.saved"))
      setError(null)
    } catch (reason) {
      setError(errorMessage(reason, t("app.error")))
    } finally {
      setBusy(false)
    }
  }

  const run = async () => {
    if (!window.confirm(t("maintenance.confirm"))) return
    setBusy(true)
    setMessage(null)
    try {
      await startMaintenance()
      setMessage(t("maintenance.started"))
      await load()
    } catch (reason) {
      setError(errorMessage(reason, t("app.error")))
    } finally {
      setBusy(false)
    }
  }

  return (
    <section className="border-t pt-7">
      <div className="mb-5 flex items-start gap-3">
        <DatabaseIcon className="mt-0.5 size-5 text-muted-foreground" />
        <div>
          <h2 className="text-base font-semibold">{t("maintenance.title")}</h2>
          <p className="mt-1 text-sm text-muted-foreground">{t("maintenance.description")}</p>
        </div>
      </div>

      {error && <Alert variant="destructive" className="mb-5"><AlertDescription>{error}</AlertDescription></Alert>}
      {message && <Alert className="mb-5"><AlertDescription>{message}</AlertDescription></Alert>}

      <StorageSummary overview={overview} />
      <RunSummary overview={overview} locale={locale} />

      <form onSubmit={(event) => void save(event)} className="mt-6 space-y-5">
        <FieldGroup>
          <Field>
            <FieldLabel htmlFor="retention-days">{t("maintenance.retention")}</FieldLabel>
            <div className="flex max-w-xs items-center gap-2">
              <Input
                id="retention-days"
                type="number"
                min={1}
                max={3650}
                value={form?.retention_days ?? 30}
                disabled={!form || busy}
                onChange={(event) => patchForm(setForm, { retention_days: Number(event.target.value) })}
              />
              <span className="text-sm text-muted-foreground">{t("maintenance.days")}</span>
            </div>
            <FieldDescription>{t("maintenance.retention_hint")}</FieldDescription>
          </Field>

          <Field orientation="horizontal">
            <FieldContent>
              <FieldLabel htmlFor="automatic-maintenance">{t("maintenance.automatic")}</FieldLabel>
              <FieldDescription>{t("maintenance.automatic_hint")}</FieldDescription>
            </FieldContent>
            <Switch
              id="automatic-maintenance"
              checked={form?.auto_cleanup_enabled ?? false}
              disabled={!form || busy}
              onCheckedChange={(auto_cleanup_enabled) => patchForm(setForm, { auto_cleanup_enabled })}
            />
          </Field>

          <Field>
            <FieldLabel htmlFor="cleanup-hour">{t("maintenance.hour")}</FieldLabel>
            <Select
              items={hours}
              value={String(form?.cleanup_hour_utc ?? 3)}
              disabled={!form || busy || !form.auto_cleanup_enabled}
              onValueChange={(value) => patchForm(setForm, { cleanup_hour_utc: Number(value) })}
            >
              <SelectTrigger id="cleanup-hour" className="w-full max-w-xs"><SelectValue /></SelectTrigger>
              <SelectContent>
                <SelectGroup>
                  {hours.map((hour) => <SelectItem key={hour.value} value={hour.value}>{hour.label}</SelectItem>)}
                </SelectGroup>
              </SelectContent>
            </Select>
            <FieldDescription>{t("maintenance.hour_hint")}</FieldDescription>
          </Field>
        </FieldGroup>

        <div className="flex flex-wrap justify-end gap-2 border-t pt-5">
          <Button
            type="button"
            variant="destructive"
            disabled={busy || !overview || overview.status.running}
            onClick={() => void run()}
          >
            {overview?.status.running ? <RefreshCwIcon className="animate-spin" /> : <PlayIcon />}
            {t(overview?.status.running ? "maintenance.running" : "maintenance.run")}
          </Button>
          <Button type="submit" disabled={busy || !form}><SaveIcon />{t("app.save")}</Button>
        </div>
      </form>
    </section>
  )
}

function StorageSummary({ overview }: { overview: MaintenanceOverview | null }) {
  const { t } = useLocale()
  const values = overview ? [
    [t("maintenance.mts_storage"), formatBytes(overview.storage.mts_bytes)],
    [t("maintenance.sqlite_storage"), formatBytes(overview.storage.sqlite_bytes)],
    [t("maintenance.total_storage"), formatBytes(overview.storage.total_bytes)],
    [t("maintenance.mts_health"), t(overview.storage.mts_healthy ? "maintenance.healthy" : "maintenance.unhealthy")],
  ] : []
  return (
    <dl className="grid overflow-hidden rounded-md border sm:grid-cols-2 lg:grid-cols-4">
      {values.map(([label, value]) => (
        <div key={label} className="border-b p-3 last:border-b-0 sm:border-r sm:[&:nth-child(n+3)]:border-b-0 lg:border-b-0">
          <dt className="text-xs text-muted-foreground">{label}</dt>
          <dd className="mt-1 text-sm font-medium">{value}</dd>
        </div>
      ))}
      {!overview && <div className="col-span-full h-16 animate-pulse bg-muted/30" />}
    </dl>
  )
}

function RunSummary(props: { overview: MaintenanceOverview | null; locale: string }) {
  const { t } = useLocale()
  const status = props.overview?.status
  if (!status) return null
  const state = t(`maintenance.${status.last_status}`)
  const trigger = status.last_trigger ? t(status.last_trigger === "manual" ? "maintenance.manual" : "maintenance.automatic_trigger") : ""
  return (
    <div className="mt-4 grid gap-2 text-sm text-muted-foreground sm:grid-cols-3">
      <p><span className="text-foreground">{t("maintenance.last_run")}:</span> {state}{trigger ? ` · ${trigger}` : ""}</p>
      <p><span className="text-foreground">{t("maintenance.cutoff")}:</span> {formatDate(status.last_cutoff_at, props.locale)}</p>
      <p><span className="text-foreground">{t("maintenance.integrity")}:</span> {status.sqlite_integrity || "--"}</p>
      {status.last_error && <p className="sm:col-span-3 text-destructive">{status.last_error}</p>}
    </div>
  )
}

function patchForm(
  setForm: React.Dispatch<React.SetStateAction<MaintenanceSettings | null>>,
  patch: Partial<MaintenanceSettings>,
) {
  setForm((current) => current ? { ...current, ...patch } : current)
}

function formatDate(value: string | null, locale: string): string {
  if (!value) return "--"
  return new Intl.DateTimeFormat(locale, { dateStyle: "medium", timeStyle: "short" }).format(new Date(value))
}

function errorMessage(reason: unknown, fallback: string): string {
  return reason instanceof Error ? reason.message : fallback
}

export default MaintenancePanel
