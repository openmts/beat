import { useState, type FormEvent } from "react"
import { Button } from "@/components/ui/button"
import { useLocale } from "@/context/locale"
import { useConfirmedOperation } from "@/hooks/use-confirmed-operation"
import { saveLifecycleSettings } from "@/lib/lifecycle-api"
import type { LifecycleSettings } from "@/types/node-lifecycle"
import { OperationConfirm } from "./operation-confirm"
import { TextField, CheckField } from "./lifecycle-fields"
import { parseDayOffsets } from "@/lib/lifecycle-validation"
export function LifecycleSettingsEditor({ value, saved }: { value: LifecycleSettings; saved: () => void }) {
 const { t } = useLocale(), flow = useConfirmedOperation()
 const [draft, setDraft] = useState(value), [lead, setLead] = useState(value.lead_days.join(",")), [post, setPost] = useState(value.post_days.join(","))
 const [error, setError] = useState("")
 const submit = (event: FormEvent) => {
  event.preventDefault(); setError("")
  try {
   new Intl.DateTimeFormat("en", { timeZone: draft.timezone }).format()
   const input = { ...draft, lead_days: parseDayOffsets(lead), post_days: parseDayOffsets(post, true) }
   flow.request({ title: t("lifecycle.save_lifecycle_defaults"), description: t("lifecycle.public_defaults_affect_every_node_using_inherite"), execute: async () => { await saveLifecycleSettings(input); saved() } })
  } catch (failure) { setError(failure instanceof Error ? failure.message : "Invalid settings") }
 }
 return <form className="space-y-4" onSubmit={submit}><fieldset className="space-y-4" disabled={flow.busy}><h2 className="font-semibold">{t("lifecycle.lifecycle_defaults")}</h2><TextField label={t("lifecycle.timezone")} value={draft.timezone} onChange={timezone => setDraft(current => ({ ...current, timezone }))} maxLength={100} /><TextField label={t("lifecycle.reminder_days_before_expiry_comma_separated")} value={lead} onChange={setLead} /><TextField label={t("lifecycle.reminder_days_after_expiry_comma_separated")} value={post} onChange={setPost} />
  <CheckField label={t("lifecycle.public_country_by_default")} checked={draft.public_country} onChange={public_country => setDraft(current => ({ ...current, public_country }))} /><CheckField label={t("lifecycle.public_expiry_by_default")} checked={draft.public_expiry} onChange={public_expiry => setDraft(current => ({ ...current, public_expiry }))} /><CheckField label={t("lifecycle.public_price_by_default")} checked={draft.public_price} onChange={public_price => setDraft(current => ({ ...current, public_price }))} /><Button type="submit">{t("lifecycle.save_defaults")}</Button></fieldset>{error && <p role="alert" className="text-destructive">{error}</p>}<OperationConfirm flow={flow} /></form>
}
