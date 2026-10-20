import { useState, type FormEvent } from "react"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Switch } from "@/components/ui/switch"
import { Checkbox } from "@/components/ui/checkbox"
import { Field, FieldGroup, FieldLabel, FieldDescription } from "@/components/ui/field"
import { useConfirmedOperation } from "@/hooks/use-confirmed-operation"
import { acceleratorMetricKeys, acceleratorMetricLabel } from "@/lib/accelerator-metrics"
import { acceleratorPolicyWrite } from "@/lib/accelerator-policy"
import { updateAcceleratorPolicy, type AcceleratorScope } from "@/lib/accelerator-api"
import type { AcceleratorPolicy } from "@/types/accelerator"
import { useAcceleratorText } from "@/pages/accelerator-text"
import { OperationConfirm } from "./operation-confirm"
export function AcceleratorPolicyEditor({ value, scope, saved }: { value: AcceleratorPolicy; scope: AcceleratorScope; saved: () => void }) {
 const [draft, setDraft] = useState(value), [error, setError] = useState("")
 const flow = useConfirmedOperation(), t = useAcceleratorText()
 const submit = (event: FormEvent) => {
  event.preventDefault(); setError("")
  try { const input = acceleratorPolicyWrite(draft); flow.request({ title: t("save"), description: t("policyHelp"), execute: async () => { await updateAcceleratorPolicy(scope, input); saved() } }) }
  catch (failure) { setError(failure instanceof Error ? failure.message : t("error")) }
 }
 return <form onSubmit={submit} className="space-y-4"><p className="text-sm text-muted-foreground">{t("effectiveSource")}: {t(`source_${value.effective_source}`)} · {value.configured ? t("explicitPolicy") : t("noExplicitPolicy")}</p>{scope.kind === "node" && <p className="text-xs text-muted-foreground">{t("nodePolicyNote")}</p>}<fieldset disabled={flow.busy}><FieldGroup>
  <Field orientation="horizontal"><FieldLabel htmlFor="gpu-enabled">{t("enabled")}</FieldLabel><Switch id="gpu-enabled" checked={draft.enabled} onCheckedChange={enabled => setDraft(current => ({ ...current, enabled }))} /></Field>
  <Field><FieldLabel htmlFor="gpu-interval">{t("interval")}</FieldLabel><Input id="gpu-interval" type="number" min={5} max={3600} value={draft.sample_interval_seconds} onChange={event => setDraft(current => ({ ...current, sample_interval_seconds: Number(event.target.value) }))} /></Field>
  <Field><FieldLabel htmlFor="gpu-limit">{t("deviceLimit")}</FieldLabel><Input id="gpu-limit" type="number" min={1} max={32} value={draft.maximum_devices} onChange={event => setDraft(current => ({ ...current, maximum_devices: Number(event.target.value) }))} /></Field>
  <Field><FieldLabel>{t("metrics")}</FieldLabel><div className="grid gap-2 sm:grid-cols-2">{acceleratorMetricKeys.map(key => <label key={key} className="flex items-center gap-2 text-sm"><Checkbox checked={draft.metrics.includes(key)} onCheckedChange={checked => setDraft(current => ({ ...current, metrics: checked ? [...current.metrics, key] : current.metrics.filter(metric => metric !== key) }))} />{acceleratorMetricLabel(key, t)}</label>)}</div><FieldDescription>{t("policyHelp")}</FieldDescription></Field>
 </FieldGroup></fieldset>{error && <p role="alert" className="text-sm text-destructive">{error}</p>}<Button type="submit" disabled={flow.busy}>{t(flow.busy ? "saving" : "save")}</Button><OperationConfirm flow={flow} /></form>
}
