import { Input } from "@/components/ui/input"
import { useLocale } from "@/context/locale"
import type { OperationPolicy } from "@/types/operations"
import { operationInputNumber, operationInteger, operationRetentionLimits } from "./operation-policy-validation"
export function OperationRetention({ value, change }: { value: OperationPolicy["retention"]; change: (value: OperationPolicy["retention"]) => void }) {
 const { t } = useLocale()
 const effective = operationRetentionLimits(value)
 const fields = [['task_days',90,30,t("operations.task_records_days")],['output_days',Math.min(30,effective.task_days),Math.min(7,effective.task_days),t("operations.output_days")],['artifact_hours',24,24,t("operations.files_hours")],['queue_hours',24,24,t("operations.queue_hours")],['upload_hours',6,6,t("operations.upload_hours")]] as const
 return <fieldset className="grid gap-3 rounded-md border p-3 sm:grid-cols-3"><legend className="px-1 text-sm">{t("operations.retention_and_expiry")}</legend>
  <p className="text-xs text-muted-foreground sm:col-span-3">{t("operations.0_uses_bounded_defaults_output_retention_cannot")}</p>
  {fields.map(([key,max,fallback,label]) => { const number = value?.[key] ?? fallback; const valid = operationInteger(effective[key],max); return <label key={key} className="space-y-1 text-sm">{label}<Input type="number" min={0} max={Number.isFinite(max) ? max : 30} step={1} aria-invalid={!valid} value={Number.isFinite(number) ? number : ""} onChange={event => change({ ...value, [key]: operationInputNumber(event.target.value) })} /></label> })}
 </fieldset>
}
