import { Button } from "@/components/ui/button"
import { Field, FieldGroup, FieldLabel } from "@/components/ui/field"
import { Input } from "@/components/ui/input"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { Switch } from "@/components/ui/switch"
import { useLocale } from "@/context/locale"
import {
  configurationFieldLabel, configurationFieldValue, configurationFields, configurationOptionLabelKey, configurationPatch,
  themeRadiusMaximum, themeRadiusMinimum,
} from "@/lib/theme-configuration-schema"
import type {
  ThemeConfiguration, ThemeConfigurationField, ThemeConfigurationSchema, ThemeDefinition,
} from "@/types/theme-definition"

interface SettingsProps {
  definition: ThemeDefinition
  schema?: ThemeConfigurationSchema
  disabled: boolean
  onChange: (value: ThemeConfiguration) => void
  onSave: () => void
  onReset: () => void
}
export function ThemeSettingsForm({ definition, schema, disabled, onChange, onSave, onReset }: SettingsProps) {
  const { t } = useLocale()
  return <aside className="space-y-4 rounded-lg border p-4"><h3 className="font-semibold">{t("theme.settings")}</h3><FieldGroup>
    {configurationFields(schema).map((field) =>
      <SettingsField key={field.key} field={field} definition={definition} disabled={disabled} onChange={onChange} />)}
    <Button disabled={disabled} onClick={onSave}>{t("theme.save")}</Button><Button variant="outline" disabled={disabled} onClick={onReset}>{t("theme.reset")}</Button>
  </FieldGroup></aside>
}

interface FieldProps {
  field: ThemeConfigurationField
  definition: ThemeDefinition
  disabled: boolean
  onChange: (value: ThemeConfiguration) => void
}
function SettingsField({ field, definition, disabled, onChange }: FieldProps) {
  const { t } = useLocale()
  const label = configurationFieldLabel(field)
  const title = "text" in label ? label.text : t(label.key)
  const value = configurationFieldValue(definition, field)
  const id = `theme-setting-${field.key}`
  if (field.type === "select") {
    const items = (field.options ?? []).map((option) => ({ value: option, label: t(configurationOptionLabelKey(field.key, option)) }))
    return <Field><FieldLabel htmlFor={id}>{title}</FieldLabel>
      <Select items={items} value={String(value)} disabled={disabled}
        onValueChange={(next) => { if (next) onChange(configurationPatch(field, next)) }}>
        <SelectTrigger id={id} className="w-full"><SelectValue /></SelectTrigger>
        <SelectContent>{items.map((item) => <SelectItem key={item.value} value={item.value}>{item.label}</SelectItem>)}</SelectContent>
      </Select></Field>
  }
  if (field.type === "switch") {
    return <Field orientation="horizontal"><FieldLabel htmlFor={id}>{title}</FieldLabel>
      <Switch id={id} checked={value === true} disabled={disabled}
        onCheckedChange={(next) => onChange(configurationPatch(field, next))} /></Field>
  }
  return <Field><FieldLabel htmlFor={id}>{title}</FieldLabel>
    <Input id={id} type="number" min={themeRadiusMinimum} max={themeRadiusMaximum} step={1} value={Number(value)} disabled={disabled}
      onChange={(event) => {
        const next = Number(event.target.value)
        if (Number.isInteger(next) && next >= themeRadiusMinimum && next <= themeRadiusMaximum) onChange(configurationPatch(field, next))
      }} /></Field>
}
