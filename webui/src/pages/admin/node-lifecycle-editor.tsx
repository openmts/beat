import { useState, type FormEvent } from "react"
import { Button } from "@/components/ui/button"
import { Textarea } from "@/components/ui/textarea"
import { useLocale } from "@/context/locale"
import { saveLifecycle } from "@/lib/lifecycle-api"
import { validateLifecycleDraft } from "@/lib/lifecycle-validation"
import { microsToPrice } from "@/lib/lifecycle-format"
import type { NodeLifecycle, LifecycleWrite } from "@/types/node-lifecycle"
import { TextField, ChoiceField, CheckField, VisibilityOptions } from "./lifecycle-fields"

export function NodeLifecycleEditor({ value, writable, saved }: { value: NodeLifecycle; writable: boolean; saved: () => void }) {
 const { t } = useLocale()
 const [draft, setDraft] = useState<LifecycleWrite>({ ...value, confirm_auto_advance: false })
 const [price, setPrice] = useState(() => microsToPrice(value.price_micros))
 const [error, setError] = useState(""), [busy, setBusy] = useState(false)
 const change = <K extends keyof LifecycleWrite>(key: K, next: LifecycleWrite[K]) => setDraft(current => ({ ...current, [key]: next }))
 const submit = async (event: FormEvent) => {
  event.preventDefault(); if (busy || !writable) return
  setError(""); setBusy(true)
  try { const input = validateLifecycleDraft(draft, price, value.renewal_mode); await saveLifecycle(value.node_id, input); saved() }
  catch (failure) { setError(failure instanceof Error ? failure.message : "Save failed") }
  finally { setBusy(false) }
 }
 return <form className="space-y-4" onSubmit={submit}><fieldset disabled={!writable || busy} className="space-y-4">
  <h2 className="font-semibold">{t("lifecycle.lifecycle")}</h2><p className="text-sm text-muted-foreground">{value.status} · {value.days_remaining ?? "—"} {t("lifecycle.days")}</p>
  <div className="grid gap-4 sm:grid-cols-2"><TextField label={t("lifecycle.vendor")} value={draft.provider} onChange={v => change("provider", v)} maxLength={100} /><TextField label={t("lifecycle.plan")} value={draft.plan} onChange={v => change("plan", v)} maxLength={100} />
   <TextField label={t("lifecycle.price_up_to_6_decimals")} value={price} onChange={setPrice} maxLength={30} /><TextField label={t("lifecycle.currency_iso_4217")} value={draft.currency} onChange={v => change("currency", v.toUpperCase())} maxLength={3} />
   <ChoiceField label={t("lifecycle.interval_unit")} value={draft.interval_unit} onChange={v => change("interval_unit", v as LifecycleWrite["interval_unit"])}><option value="day">{t("lifecycle.day")}</option><option value="month">{t("lifecycle.month")}</option><option value="year">{t("lifecycle.year")}</option></ChoiceField>
   <TextField label={t("lifecycle.interval_count_1_120")} type="number" value={String(draft.interval_count)} onChange={v => change("interval_count", Number(v))} />
   <TextField label={t("lifecycle.expiry_date")} type="date" value={draft.expires_on} onChange={v => change("expires_on", v)} />
   <ChoiceField label={t("lifecycle.date_advancement")} value={draft.renewal_mode} onChange={v => { change("renewal_mode", v as LifecycleWrite["renewal_mode"]); change("confirm_auto_advance", false) }}><option value="manual">{t("lifecycle.manual")}</option><option value="calendar_auto_advance">{t("lifecycle.automatic_calendar_advancement")}</option></ChoiceField>
   <ChoiceField label={t("lifecycle.expiry_visibility")} value={draft.expiry_visibility} onChange={v => change("expiry_visibility", v as LifecycleWrite["expiry_visibility"])}><VisibilityOptions /></ChoiceField><ChoiceField label={t("lifecycle.price_visibility")} value={draft.price_visibility} onChange={v => change("price_visibility", v as LifecycleWrite["price_visibility"])}><VisibilityOptions /></ChoiceField>
  </div><label className="grid gap-2 text-sm">{t("lifecycle.private_notes")}<Textarea maxLength={2048} value={draft.private_notes} onChange={event => change("private_notes", event.target.value)} /></label>
  {draft.renewal_mode === "calendar_auto_advance" && value.renewal_mode !== "calendar_auto_advance" && <CheckField checked={draft.confirm_auto_advance} onChange={v => change("confirm_auto_advance", v)} label={t("lifecycle.i_understand_date_advancement_does_not_constitut")} />}
  <Button type="submit" disabled={draft.renewal_mode === "calendar_auto_advance" && value.renewal_mode !== "calendar_auto_advance" && !draft.confirm_auto_advance}>{t("lifecycle.save_lifecycle")}</Button>
 </fieldset>{error && <p role="alert" className="text-sm text-destructive">{error}</p>}</form>
}
