import { AdminSelectField } from "@/components/admin-select-field"
import { Button } from "@/components/ui/button"
import { Dialog, DialogContent, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { Field, FieldGroup, FieldLabel } from "@/components/ui/field"
import { Input } from "@/components/ui/input"
import { useLocale } from "@/context/locale"
import {
  availabilityRuleDefaults,
  buildAlertMetricOptions,
  buildAlertOperatorOptions,
  buildAlertSeverityOptions,
  isAvailabilityMetric,
} from "@/pages/admin/alert-utils"

export interface AlertRuleForm {
  name: string
  description: string
  metric: string
  operator: string
  threshold: number
  duration: number
  severity: string
}

interface AlertRuleDialogProps {
  open: boolean
  editing: boolean
  form: AlertRuleForm
  loading: boolean
  onOpenChange: (open: boolean) => void
  onChange: (form: AlertRuleForm) => void
  onSave: () => void
}

export function AlertRuleDialog({
  open,
  editing,
  form,
  loading,
  onOpenChange,
  onChange,
  onSave,
}: AlertRuleDialogProps) {
  const { t } = useLocale()
  const availability = isAvailabilityMetric(form.metric)
  const metricOptions = buildAlertMetricOptions(t)
  const operatorOptions = buildAlertOperatorOptions(t("alert.greater_than"), t("alert.less_than"))
  const severityOptions = buildAlertSeverityOptions(t("alert.critical"), t("alert.warning"), t("alert.info"))

  const changeMetric = (metric: string) => {
    const defaults = isAvailabilityMetric(metric) ? availabilityRuleDefaults() : {}
    onChange({ ...form, metric, ...defaults })
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-md">
        <DialogHeader>
          <DialogTitle>{editing ? t("app.edit") : t("app.create")} {t("alert.rules")}</DialogTitle>
        </DialogHeader>
        <FieldGroup>
          <Field>
            <FieldLabel htmlFor="alert-rule-name">{t("node.name")}</FieldLabel>
            <Input id="alert-rule-name" value={form.name} onChange={(event) => onChange({ ...form, name: event.target.value })} />
          </Field>
          <Field>
            <FieldLabel htmlFor="alert-rule-description">{t("app.description")}</FieldLabel>
            <Input id="alert-rule-description" value={form.description} onChange={(event) => onChange({ ...form, description: event.target.value })} />
          </Field>
          <div className="grid grid-cols-2 gap-3">
            <AdminSelectField id="alert-rule-metric" label={t("alert.metric")} options={metricOptions} value={form.metric} onValueChange={changeMetric} />
            <AdminSelectField id="alert-rule-operator" label={t("alert.operator")} options={operatorOptions} value={form.operator} disabled={availability} onValueChange={(operator) => onChange({ ...form, operator })} />
          </div>
          <div className="grid grid-cols-2 gap-3">
            <Field>
              <FieldLabel htmlFor="alert-rule-threshold">
                {availability ? t("alert.offline_after") : t("alert.threshold")}
              </FieldLabel>
              <Input id="alert-rule-threshold" type="number" min={0} value={form.threshold} onChange={(event) => onChange({ ...form, threshold: Number(event.target.value) })} />
            </Field>
            <Field>
              <FieldLabel htmlFor="alert-rule-duration">
                {availability ? t("alert.debounce") : t("alert.duration")}
              </FieldLabel>
              <Input id="alert-rule-duration" type="number" min={0} value={form.duration} onChange={(event) => onChange({ ...form, duration: Number(event.target.value) })} />
            </Field>
          </div>
          <AdminSelectField id="alert-rule-severity" label={t("alert.severity")} options={severityOptions} value={form.severity} onValueChange={(severity) => onChange({ ...form, severity })} />
        </FieldGroup>
        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)}>{t("app.cancel")}</Button>
          <Button onClick={onSave} disabled={loading || !form.name.trim()}>{t("app.save")}</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
