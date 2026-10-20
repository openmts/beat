import { acceleratorRatioMetrics, evaluationMetricChange, isAcceleratorAlertMetric } from "@/lib/alert-accelerator"
import { EvaluationAcceleratorScope } from "./evaluation-accelerator-scope"
import { AdminSelectField } from "@/components/admin-select-field"
import { Field, FieldLabel } from "@/components/ui/field"
import { Input } from "@/components/ui/input"
import { Switch } from "@/components/ui/switch"
import type { EvaluationDraft } from "@/lib/alert-evaluation-form"
import { currentOnlyMetric } from "@/lib/alert-evaluation-form"
import type { EvaluationSources } from "@/lib/alert-evaluation-api"
import { EvaluationTargets } from "@/pages/admin/evaluation-targets"
import { useEvaluationText } from "@/pages/admin/evaluation-text"

export interface EvaluationFieldsProps { draft: EvaluationDraft; sources: EvaluationSources; locked: boolean; onChange: (draft: EvaluationDraft) => void }
export function EvaluationNumericFields({ draft, sources, locked, onChange }: EvaluationFieldsProps) {
  const t = useEvaluationText()
  const metrics = sources.metrics.filter(metric => metric.enabled || metric.key === draft.metric).map(metric => ({ value: metric.key, label: metric.label }))
  metrics.push({ value: "heartbeat_age_seconds", label: t("heartbeat") }, { value: "traffic_usage_percent", label: t("traffic") })
  metrics.push({ value: "gpu_memory_percent", label: t("gpuMemoryUsage") }, { value: "gpu_power_limit_percent", label: t("gpuPowerLimitUsage") })
  const aggregation = acceleratorRatioMetrics.includes(draft.metric as typeof acceleratorRatioMetrics[number]) ? ["last", "avg", "min", "max"] : sources.metrics.find(metric => metric.key === draft.metric)?.aggregations ?? ["last"]
  const fields = [["threshold", "threshold"], ["duration", "duration"], ["recovery_threshold", "recovery"], ["recovery_duration", "recoveryDuration"]] as const
  return <fieldset disabled={locked} className="space-y-4 disabled:opacity-65">
    <div className="grid gap-3 sm:grid-cols-2">
      <AdminSelectField id="evaluation-metric" label={t("metric")} options={metrics} value={draft.metric} disabled={locked} onValueChange={metric => onChange(evaluationMetricChange(draft, metric))} />
      <AdminSelectField id="evaluation-operator" label={t("operator")} options={[{ value: "gt", label: ">" }, { value: "gte", label: "≥" }, { value: "lt", label: "<" }, { value: "lte", label: "≤" }, { value: "eq", label: "=" }, { value: "ne", label: "≠" }]} value={draft.operator} disabled={locked} onValueChange={operator => onChange({ ...draft, operator })} />
      {fields.map(([key, label]) => <Field key={key}><FieldLabel htmlFor={`evaluation-${key}`}>{t(label)}</FieldLabel><Input id={`evaluation-${key}`} type="number" step={key.includes("duration") ? 1 : "any"} value={draft[key]} onChange={event => onChange({ ...draft, [key]: event.target.value })} /></Field>)}
      <Field><FieldLabel htmlFor="evaluation-window">{t("window")}</FieldLabel><Input id="evaluation-window" type="number" min={0} max={86400} value={draft.window_seconds} disabled={currentOnlyMetric(draft.metric)} onChange={event => onChange({ ...draft, window_seconds: event.target.value, aggregation: event.target.value === "0" ? "last" : draft.aggregation })} /></Field>
      <AdminSelectField id="evaluation-aggregation" label={t("aggregation")} options={(draft.window_seconds === "0" ? ["last"] : aggregation).map(value => ({ value, label: value }))} value={draft.aggregation} disabled={locked || currentOnlyMetric(draft.metric)} onValueChange={aggregation => onChange({ ...draft, aggregation })} />
    </div>
  </fieldset>
}
export function EvaluationScopeFields({ draft, sources, locked, onChange }: EvaluationFieldsProps) {
  const t = useEvaluationText()
  if (isAcceleratorAlertMetric(draft.metric)) return <EvaluationAcceleratorScope draft={draft} sources={sources} locked={locked} onChange={onChange} />
  return <div className="space-y-3">
    <AdminSelectField id="evaluation-scope" label={t("scope")} options={["all", "nodes", "groups"].map(value => ({ value, label: t(value) }))} value={draft.scope.kind} onValueChange={kind => onChange({ ...draft, scope: kind === "nodes" ? { kind, node_ids: [] } : kind === "groups" ? { kind, group_ids: [] } : { kind: "all" } })} />
    {draft.scope.kind === "nodes" && <EvaluationTargets items={sources.nodes} value={draft.scope.node_ids ?? []} limit={1000} onChange={node_ids => onChange({ ...draft, scope: { kind: "nodes", node_ids } })} />}
    {draft.scope.kind === "groups" && <EvaluationTargets items={sources.groups} value={draft.scope.group_ids ?? []} limit={128} onChange={group_ids => onChange({ ...draft, scope: { kind: "groups", group_ids } })} />}
  </div>
}
export function EvaluationHealthFields({ draft, onChange }: EvaluationFieldsProps) {
  const t = useEvaluationText()
  return <div className="space-y-3 border-t pt-4"><h3 className="font-medium">{t("health")}</h3>
    {(["no_data", "error"] as const).map(kind => <div key={kind} className="grid gap-3 sm:grid-cols-2">
      <AdminSelectField id={`evaluation-${kind}-policy`} label={t(kind === "no_data" ? "noData" : "errorPolicy")} options={[{ value: "keep", label: t("keep") }, { value: "notify", label: t("notify") }]} value={draft[`${kind}_policy`]} onValueChange={value => onChange({ ...draft, [`${kind}_policy`]: value })} />
      <Field><FieldLabel htmlFor={`evaluation-${kind}-delay`}>{t("delay")}</FieldLabel><Input id={`evaluation-${kind}-delay`} type="number" min={0} max={86400} value={draft[`${kind}_delay_seconds`]} onChange={event => onChange({ ...draft, [`${kind}_delay_seconds`]: event.target.value })} /></Field>
    </div>)}
    <label className="flex items-center gap-2 text-sm"><Switch checked={draft.enabled} onCheckedChange={enabled => onChange({ ...draft, enabled })} />{t("enabled")}</label>
  </div>
}
