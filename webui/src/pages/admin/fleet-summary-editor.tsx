import { useState, type FormEvent } from "react"
import { Button } from "@/components/ui/button"
import { Dialog, DialogContent, DialogHeader, DialogTitle, DialogDescription } from "@/components/ui/dialog"
import { useLocale } from "@/context/locale"
import { useNodes, useGroups, useAlertChannels } from "@/hooks/use-api"
import { createFleetSummary, updateFleetSummary } from "@/lib/fleet-summary-api"
import type { FleetSummarySchedule, FleetSummaryWrite } from "@/types/fleet-summary"
import { TextField, ChoiceField, CheckField } from "./lifecycle-fields"
import { emptyFleetSummary, validateFleetSummary } from "@/lib/fleet-summary-state"
import { dateTimeFormatter } from "@/lib/intl-format"
export function FleetSummaryEditor({ value, close, saved }: { value: FleetSummarySchedule | null; close: () => void; saved: () => void }) {
 const { t, locale } = useLocale(), nodes = useNodes(), groups = useGroups(), channels = useAlertChannels()
 const [draft, setDraft] = useState<FleetSummaryWrite>(() => value ? Object.fromEntries(Object.keys(emptyFleetSummary).map(key => [key, value[key as keyof FleetSummaryWrite]])) as unknown as FleetSummaryWrite : emptyFleetSummary), [error, setError] = useState(""), [busy, setBusy] = useState(false)
 const change = <K extends keyof FleetSummaryWrite>(key: K, next: FleetSummaryWrite[K]) => setDraft(current => ({ ...current, [key]: next }))
 const submit = async (event: FormEvent) => {
  event.preventDefault(); if (busy) return; setBusy(true); setError("")
  try { validateFleetSummary(draft); if (value) await updateFleetSummary(value.id, { ...draft, expected_revision: value.revision }); else await createFleetSummary(draft); saved(); close() }
  catch (failure) { setError(failure instanceof Error ? failure.message : "Save failed") } finally { setBusy(false) }
 }
 const weekdays = Array.from({ length: 7 }, (_, index) => dateTimeFormatter(locale, { weekday: "long", timeZone: "UTC" }).format(Date.UTC(2024, 0, 7 + index)))
 return <Dialog open onOpenChange={open => { if (!open && !busy) close() }}><DialogContent className="max-h-[90dvh] overflow-y-auto sm:max-w-2xl"><DialogHeader><DialogTitle>{t("fleet.summary.title")}</DialogTitle><DialogDescription>{t("fleet.summary.restart_note")}</DialogDescription></DialogHeader>
  <form className="space-y-4" onSubmit={submit}><fieldset disabled={busy} className="grid gap-4 sm:grid-cols-2"><TextField label={t("fleet.name")} value={draft.name} onChange={v => change("name", v)} maxLength={100} /><TextField label={t("fleet.summary.timezone")} value={draft.timezone} onChange={v => change("timezone", v)} maxLength={100} />
   <ChoiceField label={t("fleet.summary.cadence")} value={draft.cadence} onChange={v => { change("cadence", v as FleetSummaryWrite["cadence"]); change("weekday", v === "weekly" ? 1 : null) }}><option value="daily">{t("fleet.summary.daily")}</option><option value="weekly">{t("fleet.summary.weekly")}</option></ChoiceField><TextField label={t("fleet.summary.local_time")} value={draft.local_time} type="time" onChange={v => change("local_time", v)} />
   {draft.cadence === "weekly" && <ChoiceField label={t("fleet.summary.weekday")} value={String(draft.weekday)} onChange={v => change("weekday", Number(v))}>{weekdays.map((day, index) => <option key={day} value={index}>{day}</option>)}</ChoiceField>}<TextField label={t("fleet.summary.name_threshold")} value={String(draft.name_threshold)} type="number" onChange={v => change("name_threshold", Number(v))} /><CheckField label={t("fleet.summary.enabled")} checked={draft.enabled} onChange={v => change("enabled", v)} />
   <ScopeChoices label={t("fleet.summary.nodes_all")} options={(nodes.data ?? []).map(node => ({ id: node.id, name: node.alias || node.name }))} selected={draft.node_ids} change={v => change("node_ids", v)} /><ScopeChoices label={t("fleet.summary.groups_all")} options={(groups.data ?? []).map(group => ({ id: group.id, name: group.name }))} selected={draft.group_ids} change={v => change("group_ids", v)} /><ScopeChoices label={t("fleet.summary.notification_channels")} options={(channels.data ?? []).map(channel => ({ id: channel.id, name: channel.name }))} selected={draft.channel_ids} change={v => change("channel_ids", v)} />
  </fieldset>{(error || nodes.error || groups.error || channels.error) && <p role="alert" className="text-destructive">{error || nodes.error || groups.error || channels.error}</p>}<Button type="submit" disabled={busy || nodes.loading || groups.loading || channels.loading || Boolean(nodes.error || groups.error || channels.error)}>{t("fleet.summary.save")}</Button></form>
 </DialogContent></Dialog>
}
function ScopeChoices({ label, options, selected, change }: { label: string; options: { id: string; name: string }[]; selected: string[]; change: (ids: string[]) => void }) {
 return <fieldset className="space-y-2"><legend className="mb-2 text-sm font-medium">{label}</legend><div className="max-h-40 space-y-2 overflow-y-auto rounded-md border p-3">{options.map(option => <CheckField key={option.id} label={option.name} checked={selected.includes(option.id)} onChange={checked => change(checked ? [...selected, option.id] : selected.filter(id => id !== option.id))} />)}</div></fieldset>
}
