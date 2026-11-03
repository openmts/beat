import { Alert, AlertDescription } from "@/components/ui/alert";
import { Checkbox } from "@/components/ui/checkbox";
import {
  Field,
  FieldDescription,
  FieldGroup,
  FieldLabel,
} from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import { Switch } from "@/components/ui/switch";
import { useLocale } from "@/context/locale";
import type { EgressProxySettings } from "@/types/egress-proxy";
import type { EgressProxyFormState } from "./egress-proxy-form";

interface EgressProxyFieldsProps {
  form: EgressProxyFormState;
  settings: EgressProxySettings | null;
  disabled: boolean;
  onChange: (values: Partial<EgressProxyFormState>) => void;
}

export function EgressProxyFields({
  form,
  settings,
  disabled,
  onChange,
}: EgressProxyFieldsProps) {
  const { t } = useLocale();

  return (
    <FieldGroup>
      <Field orientation="horizontal">
        <FieldLabel htmlFor="egress-proxy-enabled">
          {t("settings.proxy_enable")}
        </FieldLabel>
        <Switch
          id="egress-proxy-enabled"
          checked={form.enabled}
          disabled={disabled}
          onCheckedChange={(enabled) => onChange({ enabled })}
        />
        <FieldDescription>{t("settings.proxy_enable_hint")}</FieldDescription>
      </Field>

      <Field>
        <FieldLabel htmlFor="egress-proxy-url">
          {t("settings.proxy_url")}
        </FieldLabel>
        <Input
          id="egress-proxy-url"
          type="url"
          inputMode="url"
          autoComplete="off"
          spellCheck={false}
          placeholder="http://127.0.0.1:7890"
          value={form.proxy_url}
          required={form.enabled}
          disabled={disabled}
          onChange={(event) => onChange({ proxy_url: event.target.value })}
        />
        <FieldDescription>{t("settings.proxy_url_hint")}</FieldDescription>
      </Field>

      <div className="grid gap-5 md:grid-cols-2">
        <Field>
          <FieldLabel htmlFor="egress-proxy-username">
            {t("settings.proxy_username")}
          </FieldLabel>
          <Input
            id="egress-proxy-username"
            autoComplete="username"
            value={form.username}
            disabled={disabled}
            onChange={(event) => onChange({ username: event.target.value })}
          />
          <FieldDescription>
            {t("settings.proxy_username_hint")}
          </FieldDescription>
        </Field>
        <Field>
          <FieldLabel htmlFor="egress-proxy-password">
            {t("settings.proxy_password")}
          </FieldLabel>
          <Input
            id="egress-proxy-password"
            type="password"
            autoComplete="new-password"
            value={form.password}
            disabled={disabled || form.clear_password}
            onChange={(event) => onChange({ password: event.target.value })}
          />
          <FieldDescription>
            {settings?.has_password
              ? t("settings.proxy_password_saved")
              : t("settings.proxy_password_hint")}
          </FieldDescription>
        </Field>
      </div>

      {settings?.has_password && (
        <label className="flex items-start gap-3 rounded-lg border p-3 text-sm">
          <Checkbox
            checked={form.clear_password}
            disabled={disabled}
            onCheckedChange={(checked) =>
              onChange({
                clear_password: Boolean(checked),
                password: "",
              })
            }
          />
          <span className="space-y-1">
            <span className="block font-medium">
              {t("settings.proxy_clear_password")}
            </span>
            <span className="block text-muted-foreground">
              {t("settings.proxy_clear_password_hint")}
            </span>
          </span>
        </label>
      )}

      <Alert>
        <AlertDescription>{t("settings.proxy_scope")}</AlertDescription>
      </Alert>
    </FieldGroup>
  );
}
