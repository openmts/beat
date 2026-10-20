import { AdminSelectField } from "@/components/admin-select-field"
import { Checkbox } from "@/components/ui/checkbox"
import { Field, FieldGroup, FieldLabel } from "@/components/ui/field"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Textarea } from "@/components/ui/textarea"
import { useLocale } from "@/context/locale"
import type { EnrollmentGrantWrite } from "@/types/enrollment"
import type { Group } from "@/types"

interface EnrollmentGrantFieldsProps {
  draft: EnrollmentGrantWrite
  groups: Group[]
  disabled: boolean
  change: <K extends keyof EnrollmentGrantWrite>(key: K, value: EnrollmentGrantWrite[K]) => void
}

function CheckLine({ label, checked, onChange }: { label: string; checked: boolean; onChange: (value: boolean) => void }) {
  return (
    <Label className="flex items-start gap-3 rounded-lg border p-3 leading-relaxed">
      <Checkbox checked={checked} className="mt-0.5" onCheckedChange={(value) => onChange(value === true)} />
      <span>{label}</span>
    </Label>
  )
}

function Hint({ children }: { children: string }) {
  return <p className="text-xs leading-relaxed text-muted-foreground">{children}</p>
}

export function EnrollmentGrantFields({ draft, groups, disabled, change }: EnrollmentGrantFieldsProps) {
  const { t } = useLocale()
  return (
    <fieldset disabled={disabled} className="py-4">
      <FieldGroup>
        <Field>
          <FieldLabel htmlFor="grant-name">{t("discovery.grant_name")}</FieldLabel>
          <Input id="grant-name" value={draft.name} maxLength={200} required
            onChange={(event) => change("name", event.target.value)} />
        </Field>
        <AdminSelectField id="grant-mode" label={t("discovery.mode")} value={draft.mode}
          options={[{ value: "manual", label: t("discovery.mode_manual") },
            { value: "automatic", label: t("discovery.mode_automatic") }]}
          onValueChange={(value) => change("mode", value as EnrollmentGrantWrite["mode"])} />
        <Hint>{t(draft.mode === "automatic" ? "discovery.mode_automatic_hint" : "discovery.mode_manual_hint")}</Hint>
        <AdminSelectField id="grant-group" label={t("discovery.default_group")} value={draft.default_group_id}
          options={groups.map((group) => ({ value: group.id, label: group.name }))}
          onValueChange={(value) => change("default_group_id", value)} />
        <Field>
          <FieldLabel htmlFor="grant-expiry">{t("discovery.expiry")}</FieldLabel>
          <Input id="grant-expiry" type="datetime-local" required value={draft.expires_at}
            onChange={(event) => change("expires_at", event.target.value)} />
        </Field>
        <Field>
          <FieldLabel htmlFor="grant-capacity">{t("discovery.maximum_enrollments")}</FieldLabel>
          <Input id="grant-capacity" inputMode="numeric" value={String(draft.maximum_enrollments)}
            onChange={(event) => change("maximum_enrollments", Number(event.target.value.replace(/\D/g, "")) || 0)} />
        </Field>
        <Field>
          <FieldLabel htmlFor="grant-prefix">{t("discovery.node_name_prefix")}</FieldLabel>
          <Input id="grant-prefix" value={draft.node_name_prefix} maxLength={64}
            onChange={(event) => change("node_name_prefix", event.target.value)} />
          <Hint>{t("discovery.node_name_prefix_hint")}</Hint>
        </Field>
        <Field>
          <FieldLabel htmlFor="grant-labels">{t("discovery.labels")}</FieldLabel>
          <Input id="grant-labels" value={draft.tags.join(", ")}
            onChange={(event) => change("tags", event.target.value.split(",").map((entry) => entry.trim()).filter(Boolean))} />
          <Hint>{t("discovery.labels_hint")}</Hint>
        </Field>
        <Field>
          <FieldLabel htmlFor="grant-remarks">{t("discovery.remarks")}</FieldLabel>
          <Textarea id="grant-remarks" value={draft.remarks} maxLength={2000}
            onChange={(event) => change("remarks", event.target.value)} />
        </Field>
        <Field>
          <FieldLabel htmlFor="grant-ssh-port">{t("discovery.ssh_port")}</FieldLabel>
          <Input id="grant-ssh-port" inputMode="numeric" value={String(draft.ssh_port)}
            onChange={(event) => change("ssh_port", Number(event.target.value.replace(/\D/g, "")) || 0)} />
        </Field>
        <Field>
          <FieldLabel htmlFor="grant-cidrs">{t("discovery.source_cidrs")}</FieldLabel>
          <Textarea id="grant-cidrs" value={draft.source_cidrs.join("\n")}
            onChange={(event) => change("source_cidrs", event.target.value.split("\n").map((line) => line.trim()).filter(Boolean))} />
          <Hint>{t("discovery.source_cidrs_hint")}</Hint>
        </Field>
        <Field>
          <FieldLabel htmlFor="grant-hostname">{t("discovery.hostname_pattern")}</FieldLabel>
          <Input id="grant-hostname" value={draft.hostname_pattern} maxLength={256}
            onChange={(event) => change("hostname_pattern", event.target.value)} />
          <Hint>{t("discovery.hostname_pattern_hint")}</Hint>
        </Field>
        <CheckLine label={t("discovery.enabled")} checked={draft.enabled}
          onChange={(value) => change("enabled", value)} />
        <CheckLine label={t("discovery.public")} checked={draft.public}
          onChange={(value) => change("public", value)} />
        <CheckLine label={t("discovery.use_source_ip_as_host")} checked={draft.source_ip_as_host}
          onChange={(value) => change("source_ip_as_host", value)} />
        <Hint>{t("discovery.use_source_ip_as_host_hint")}</Hint>
        <CheckLine label={t("discovery.claim_recovery")} checked={draft.claim_recovery}
          onChange={(value) => change("claim_recovery", value)} />
        <Hint>{t("discovery.claim_recovery_hint")}</Hint>
      </FieldGroup>
    </fieldset>
  )
}
