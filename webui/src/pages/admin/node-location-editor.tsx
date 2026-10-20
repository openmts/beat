import { useState, type FormEvent } from "react"
import { Button } from "@/components/ui/button"
import { useLocale } from "@/context/locale"
import { saveLocation, refreshLocation } from "@/lib/lifecycle-api"
import type { NodeLocation, LocationWrite } from "@/types/node-lifecycle"
import { TextField, ChoiceField, CheckField, VisibilityOptions } from "./lifecycle-fields"
export function NodeLocationEditor({ value, writable, saved }: { value: NodeLocation; writable: boolean; saved: () => void }) {
 const { t } = useLocale()
 const [draft, setDraft] = useState<LocationWrite>({ mode: value.mode, scope: value.scope, country_code: value.country_code ?? "", visibility: value.visibility, managed_target: "", purge: false, confirm_managed_target: false })
 const [error, setError] = useState(""), [busy, setBusy] = useState(false)
 const change = <K extends keyof LocationWrite>(key: K, next: LocationWrite[K]) => setDraft(current => ({ ...current, [key]: next }))
 const run = async (refresh: boolean) => {
  if (busy || !writable) return
  setBusy(true); setError("")
  try { if (refresh) await refreshLocation(value.node_id); else await saveLocation(value.node_id, draft); saved() }
  catch (failure) { setError(failure instanceof Error ? failure.message : "Save failed") }
  finally { setBusy(false) }
 }
 const submit = (event: FormEvent) => { event.preventDefault(); void run(false) }
 return <form className="space-y-4" onSubmit={submit}><fieldset disabled={!writable || busy} className="space-y-4">
  <h2 className="font-semibold">{t("lifecycle.country_location")}</h2><p className="text-sm text-muted-foreground">{value.country_code || "—"} · {value.provider || "—"} · {value.resolved_at || "—"}{value.stale ? (t("lifecycle.stale")) : ""}</p>
  {value.error_code && <p role="status">{value.error_code}</p>}
  <div className="grid gap-4 sm:grid-cols-2"><ChoiceField label={t("lifecycle.location_source")} value={draft.mode} onChange={v => { change("mode", v as LocationWrite["mode"]); change("confirm_managed_target", false) }}><option value="observed">{t("lifecycle.observed_agent_address")}</option><option value="managed_target">{t("lifecycle.managed_target")}</option><option value="manual">{t("lifecycle.manual")}</option><option value="disabled">{t("lifecycle.disabled")}</option></ChoiceField>
   <ChoiceField label={t("lifecycle.visibility")} value={draft.visibility} onChange={v => change("visibility", v as LocationWrite["visibility"])}><VisibilityOptions /></ChoiceField>
   {draft.mode === "manual" && <><ChoiceField label={t("lifecycle.address_scope")} value={draft.scope} onChange={v => { change("scope", v); if (v !== "public") change("country_code", "") }}>{["public", "private", "loopback", "link_local", "carrier_grade_nat", "documentation", "benchmark", "multicast", "unspecified", "invalid"].map(scope => <option key={scope}>{scope}</option>)}</ChoiceField>{draft.scope === "public" && <TextField label={t("lifecycle.country_code_iso_3166_1")} value={draft.country_code} onChange={v => change("country_code", v.toUpperCase())} maxLength={2} />}</>}
   {draft.mode === "managed_target" && <TextField label={t("lifecycle.target_ip_or_hostname")} value={draft.managed_target ?? ""} onChange={v => change("managed_target", v)} maxLength={253} />}
  </div>{draft.mode === "managed_target" && (Boolean(draft.managed_target) || value.mode !== "managed_target") && <CheckField checked={draft.confirm_managed_target} onChange={v => change("confirm_managed_target", v)} label={t("lifecycle.i_authorize_resolving_this_target_and_submitting")} />}
  <CheckField checked={draft.purge} onChange={v => change("purge", v)} label={t("lifecycle.purge_stored_location_source_and_result")} />
  <div className="flex gap-2"><Button type="submit" disabled={draft.mode === "managed_target" && (Boolean(draft.managed_target) || value.mode !== "managed_target") && (!draft.confirm_managed_target || !draft.managed_target)}>{t("lifecycle.save_location")}</Button><Button type="button" variant="outline" onClick={() => void run(true)}>{t("lifecycle.refresh_location")}</Button></div>
 </fieldset>{error && <p role="alert" className="text-sm text-destructive">{error}</p>}</form>
}
