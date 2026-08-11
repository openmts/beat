import { useEffect, useState, type FormEvent, type ReactNode } from "react"
import { SaveIcon } from "lucide-react"
import { Alert, AlertDescription } from "@/components/ui/alert"
import { Button } from "@/components/ui/button"
import {
  Field,
  FieldContent,
  FieldDescription,
  FieldGroup,
  FieldLabel,
} from "@/components/ui/field"
import { Input } from "@/components/ui/input"
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
  type SelectOption,
} from "@/components/ui/select"
import { Switch } from "@/components/ui/switch"
import { Textarea } from "@/components/ui/textarea"
import { useLocale } from "@/context/locale"
import { useSiteSettings } from "@/context/site-settings"
import { updateSiteSettings } from "@/lib/api"
import type { SiteSettings } from "@/types"
import { PageHeader } from "@/components/page-header"
import MaintenancePanel from "@/pages/admin/maintenance-panel"

function Settings() {
  const { t } = useLocale()
  const { settings, loading, error: loadError, applySettings } = useSiteSettings()
  const [form, setForm] = useState(settings)
  const [saving, setSaving] = useState(false)
  const [message, setMessage] = useState<string | null>(null)
  const [saveError, setSaveError] = useState<string | null>(null)

  useEffect(() => setForm(settings), [settings])

  const patch = (values: Partial<SiteSettings>) => setForm((current) => ({ ...current, ...values }))
  const submit = async (event: FormEvent) => {
    event.preventDefault()
    setSaving(true)
    setMessage(null)
    setSaveError(null)
    try {
      const updated = await updateSiteSettings(form)
      applySettings(updated)
      setMessage(t("settings.saved"))
    } catch (reason) {
      setSaveError(reason instanceof Error ? reason.message : t("app.error"))
    } finally {
      setSaving(false)
    }
  }

  return (
    <div className="mx-auto w-full max-w-5xl">
      <PageHeader title={t("settings.site")} description={t("settings.site_description")} />
      {(loadError || saveError) && (
        <Alert variant="destructive" className="mb-5">
          <AlertDescription>{saveError || loadError}</AlertDescription>
        </Alert>
      )}
      {message && (
        <Alert className="mb-5"><AlertDescription>{message}</AlertDescription></Alert>
      )}
      <form onSubmit={(event) => void submit(event)} className="space-y-8" aria-busy={loading || saving}>
        <SettingsSection title={t("settings.identity")}>
          <FieldGroup>
            <Field>
              <FieldLabel htmlFor="site-title">{t("settings.site_title")}</FieldLabel>
              <Input
                id="site-title"
                value={form.site_title}
                maxLength={80}
                required
                disabled={loading}
                onChange={(event) => patch({ site_title: event.target.value })}
              />
              <FieldDescription>{t("settings.site_title_hint")}</FieldDescription>
            </Field>
            <Field>
              <FieldLabel htmlFor="site-description">{t("settings.description")}</FieldLabel>
              <Textarea
                id="site-description"
                value={form.site_description}
                maxLength={240}
                disabled={loading}
                onChange={(event) => patch({ site_description: event.target.value })}
              />
              <FieldDescription>{t("settings.description_hint")}</FieldDescription>
            </Field>
          </FieldGroup>
        </SettingsSection>

        <SettingsSection title={t("settings.branding")}>
          <div className="grid gap-6 md:grid-cols-[1fr_12rem]">
            <FieldGroup>
              <URLField
                id="site-logo"
                label={t("settings.logo_url")}
                hint={t("settings.logo_hint")}
                value={form.logo_url}
                disabled={loading}
                onChange={(logo_url) => patch({ logo_url })}
              />
              <URLField
                id="site-favicon"
                label={t("settings.favicon_url")}
                hint={t("settings.favicon_hint")}
                value={form.favicon_url}
                disabled={loading}
                onChange={(favicon_url) => patch({ favicon_url })}
              />
            </FieldGroup>
            <div>
              <p className="mb-2 text-sm font-medium">{t("settings.preview")}</p>
              <div className="flex h-28 items-center justify-center overflow-hidden rounded-md border bg-muted/20 p-3">
                {form.logo_url ? (
                  <img
                    src={form.logo_url}
                    alt=""
                    className="max-h-full max-w-full object-contain"
                    referrerPolicy="no-referrer"
                  />
                ) : (
                  <span className="break-words text-center text-sm font-semibold">
                    {form.site_title}
                  </span>
                )}
              </div>
            </div>
          </div>
        </SettingsSection>

        <SettingsSection title={t("settings.appearance")}>
          <ThemeField
            value={form.default_theme}
            disabled={loading}
            onChange={(default_theme) => patch({ default_theme })}
          />
        </SettingsSection>

        <SettingsSection title={t("settings.public_display")}>
          <FieldGroup>
            <SwitchField
              id="show-ip"
              label={t("settings.show_ip")}
              hint={t("settings.show_ip_hint")}
              checked={form.show_ip_addresses}
              disabled={loading}
              onChange={(show_ip_addresses) => patch({ show_ip_addresses })}
            />
            <SwitchField
              id="show-network"
              label={t("settings.show_network")}
              hint={t("settings.show_network_hint")}
              checked={form.show_network_quality}
              disabled={loading}
              onChange={(show_network_quality) => patch({ show_network_quality })}
            />
          </FieldGroup>
        </SettingsSection>

        <div className="flex justify-end border-t pt-5">
          <Button type="submit" disabled={loading || saving}><SaveIcon />{t("app.save")}</Button>
        </div>
      </form>
      <MaintenancePanel />
    </div>
  )
}

function SettingsSection({ title, children }: { title: string; children: ReactNode }) {
  return (
    <section className="grid gap-5 border-t pt-5 md:grid-cols-[12rem_1fr]">
      <h2 className="text-sm font-semibold">{title}</h2>
      <div>{children}</div>
    </section>
  )
}

interface URLFieldProps {
  id: string
  label: string
  hint: string
  value: string
  disabled: boolean
  onChange: (value: string) => void
}

function URLField(props: URLFieldProps) {
  return (
    <Field>
      <FieldLabel htmlFor={props.id}>{props.label}</FieldLabel>
      <Input
        id={props.id}
        inputMode="url"
        value={props.value}
        maxLength={2048}
        disabled={props.disabled}
        placeholder="https://example.com/brand.svg"
        onChange={(event) => props.onChange(event.target.value)}
      />
      <FieldDescription>{props.hint}</FieldDescription>
    </Field>
  )
}

function ThemeField(props: { value: SiteSettings["default_theme"]; disabled: boolean; onChange: (value: SiteSettings["default_theme"]) => void }) {
  const { t } = useLocale()
  const items: SelectOption[] = [
    { value: "system", label: t("settings.theme_system") },
    { value: "light", label: t("settings.theme_light") },
    { value: "dark", label: t("settings.theme_dark") },
  ]
  return (
    <Field>
      <FieldLabel htmlFor="default-theme">{t("settings.default_theme")}</FieldLabel>
      <Select
        items={items}
        value={props.value}
        disabled={props.disabled}
        onValueChange={(value) => props.onChange(value as SiteSettings["default_theme"])}
      >
        <SelectTrigger id="default-theme" className="w-full"><SelectValue /></SelectTrigger>
        <SelectContent>
          <SelectGroup>
            {items.map((item) => (
              <SelectItem key={item.value} value={item.value}>{item.label}</SelectItem>
            ))}
          </SelectGroup>
        </SelectContent>
      </Select>
      <FieldDescription>{t("settings.default_theme_hint")}</FieldDescription>
    </Field>
  )
}

function SwitchField(props: { id: string; label: string; hint: string; checked: boolean; disabled: boolean; onChange: (value: boolean) => void }) {
  return (
    <Field orientation="horizontal">
      <FieldContent>
        <FieldLabel htmlFor={props.id}>{props.label}</FieldLabel>
        <FieldDescription>{props.hint}</FieldDescription>
      </FieldContent>
      <Switch
        id={props.id}
        checked={props.checked}
        disabled={props.disabled}
        onCheckedChange={props.onChange}
      />
    </Field>
  )
}

export default Settings
