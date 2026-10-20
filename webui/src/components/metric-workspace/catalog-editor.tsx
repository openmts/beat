import { useState } from "react"
import { Button } from "@/components/ui/button"
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Switch } from "@/components/ui/switch"
import { Textarea } from "@/components/ui/textarea"
import { useAuth } from "@/context/auth"
import { useLocale } from "@/context/locale"
import { APIError } from "@/lib/api-error"
import { updateMetricPolicy } from "@/lib/metric-workspace-api"
import { WorkspaceReauthenticate } from "./confirm"
import { WorkspaceSelect } from "./select"
import type { MetricAggregation, MetricDefinition } from "@/types/metric-workspace"

export function MetricPolicyEditor(props: { metric: MetricDefinition; close: () => void; changed: () => void }) {
  const { t } = useLocale()
  const { principal } = useAuth()
  const [draft, setDraft] = useState({ ...props.metric })
  const [busy, setBusy] = useState(false)
  const [proof, setProof] = useState(false)
  const [error, setError] = useState<string | null>(null)
  async function save() {
    setBusy(true); setError(null)
    try { await updateMetricPolicy(draft); props.changed() }
    catch (error) { if (error instanceof APIError && error.status === 428) setProof(true); else setError(error instanceof Error ? error.message : "workspace.save_failed") }
    finally { setBusy(false) }
  }
  return <>
    <Dialog open onOpenChange={(open) => { if (!open && !busy) props.close() }}><DialogContent className="max-h-[90svh] overflow-auto sm:max-w-xl"><DialogHeader><DialogTitle>{t("workspace.edit_policy")} · {props.metric.label}</DialogTitle><DialogDescription>{t("workspace.retention_warning")}</DialogDescription></DialogHeader>
      <div className="grid gap-2"><Label htmlFor="metric-label">{t("workspace.variable_label")}</Label><Input id="metric-label" value={draft.label} maxLength={128} onChange={(event) => setDraft({ ...draft, label: event.target.value })} /></div>
      <div className="grid gap-2"><Label htmlFor="metric-description">{t("workspace.dashboard_description")}</Label><Textarea id="metric-description" value={draft.description} maxLength={2048} onChange={(event) => setDraft({ ...draft, description: event.target.value })} /></div>
      <div className="grid gap-2"><Label>{t("workspace.aggregation")}</Label><WorkspaceSelect label={t("workspace.aggregation")} value={draft.default_aggregation} options={draft.aggregations.map((value) => ({ value, label: t(`workspace.${value === "min" || value === "max" ? `${value}_agg` : value}`) }))} onChange={(value) => setDraft({ ...draft, default_aggregation: value as MetricAggregation })} /></div>
      <div className="grid gap-2"><Label htmlFor="metric-retention">{t("workspace.retention")}</Label><Input id="metric-retention" type="number" min="1" max="3650" value={draft.retention_override_days ?? ""} placeholder={t("workspace.inherit_retention", { days: draft.retention_days })}
        onChange={(event) => setDraft({ ...draft, retention_override_days: event.target.value === "" ? null : Number(event.target.value) })} /></div>
      <div className="flex items-center gap-3"><Switch id="metric-public" checked={draft.public} onCheckedChange={(value) => setDraft({ ...draft, public: value })} /><Label htmlFor="metric-public">{t("workspace.public_metric")}</Label></div>
      <div className="flex items-center gap-3"><Switch id="metric-enabled" checked={draft.enabled} onCheckedChange={(enabled) => setDraft({ ...draft, enabled })} /><Label htmlFor="metric-enabled">{t("workspace.enabled")}</Label></div>
      {error && <p role="alert" className="text-sm text-destructive">{error}</p>}
      <DialogFooter><Button variant="outline" disabled={busy} onClick={props.close}>{t("workspace.cancel")}</Button><Button disabled={busy || !draft.label.trim()} onClick={() => { void save() }}>{t("workspace.apply")}</Button></DialogFooter>
    </DialogContent></Dialog>
    {proof && <WorkspaceReauthenticate action={save} close={() => setProof(false)} confirmed={() => undefined} totp={!!principal?.user.totp_enabled_at} />}
  </>
}
