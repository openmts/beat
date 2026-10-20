import { useEffect, useState, type FormEvent, type ReactNode } from "react"
import { SaveIcon } from "lucide-react"
import { Alert, AlertDescription } from "@/components/ui/alert"
import { Badge } from "@/components/ui/badge"
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
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group"
import { Textarea } from "@/components/ui/textarea"
import { useLocale } from "@/context/locale"
import { useSiteSettings } from "@/context/site-settings"
import { getDeploymentAccess, updateSiteSettings } from "@/lib/api"
import type { DeploymentAccessStatus, PublicAddressMode, SiteSettings } from "@/types"
import { PageHeader } from "@/components/page-header"
import DataStorageSection from "@/pages/admin/data-storage-section"
import { PublicTelemetryPanel } from "@/pages/admin/public-telemetry-panel"
import { ThemesPanel } from "@/pages/admin/theme-panels"

function Settings() {
  const { t } = useLocale()
  const { settings, loading, error: loadError, applySettings } = useSiteSettings()
  const [form, setForm] = useState(settings)
  const [saving, setSaving] = useState(false)
  const [message, setMessage] = useState<string | null>(null)
  const [saveError, setSaveError] = useState<string | null>(null)
  const [deployment, setDeployment] = useState<DeploymentAccessStatus | null>(null)
  const [deploymentError, setDeploymentError] = useState(false)

  useEffect(() => {
    let active = true
    void (async () => {
      try {
        const status = await getDeploymentAccess()
        if (active) setDeployment(status)
      } catch {
        if (active) setDeploymentError(true)
      }
    })()
    return () => {
      active = false
    }
  }, [])

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
          <div className="grid gap-6 md:grid-cols-[minmax(0,1fr)_12rem]">
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
            <Field>
              <FieldLabel>{t("settings.address_mode")}</FieldLabel>
              <ToggleGroup
                variant="outline"
                spacing={0}
                value={[form.public_address_mode]}
                disabled={loading}
                onValueChange={(value) => value[0] && patch({ public_address_mode: value[0] as PublicAddressMode })}
              >
                {addressModes.map((mode) => (
                  <ToggleGroupItem key={mode} value={mode} aria-label={t(`settings.address_${mode}`)}>
                    {t(`settings.address_${mode}`)}
                  </ToggleGroupItem>
                ))}
              </ToggleGroup>
              <FieldDescription>{t("settings.address_hint")}</FieldDescription>
            </Field>
            <SwitchField
              id="show-network"
              label={t("settings.show_network")}
              hint={t("settings.show_network_hint")}
              checked={form.show_network_quality}
              disabled={loading}
              onChange={(show_network_quality) => patch({ show_network_quality })}
            />
            <SwitchField
              id="show-accelerators"
              label={t("settings.show_accelerators")}
              hint={t("settings.show_accelerators_hint")}
              checked={form.show_accelerators}
              disabled={loading}
              onChange={(show_accelerators) => patch({ show_accelerators })}
            />
            <Alert>
              <AlertDescription>{t("settings.address_privacy_note")}</AlertDescription>
            </Alert>
          </FieldGroup>
        </SettingsSection>

        <SettingsSection title={t("settings.deployment")}>
          <FieldGroup>
            <FieldDescription>{t("settings.deployment_hint")}</FieldDescription>
            <DeploymentRow label={t("settings.external_url")} value={deployment?.public_base_url || t("settings.external_url_unset")} />
            <DeploymentRow label={t("settings.api_origins")} value={deploymentError ? t("settings.deployment_unavailable") : originSummary(t, deployment, "api")} />
            <DeploymentRow label={t("settings.ws_origins")} value={deploymentError ? t("settings.deployment_unavailable") : originSummary(t, deployment, "ws")} />
          </FieldGroup>
        </SettingsSection>

        <div className="flex justify-end border-t pt-5">
          <Button type="submit" disabled={loading || saving}><SaveIcon />{t("app.save")}</Button>
        </div>
      </form>
      <PublicTelemetryPanel />
      <DataStorageSection />
      <ThemesPanel />
    </div>
  )
}

const addressModes: PublicAddressMode[] = ["hidden", "masked", "full"]

type Translate = (key: string) => string

function DeploymentRow({ label, value }: { label: string; value: string }) {
  return (
    <div className="flex flex-wrap items-center justify-between gap-2 border-t pt-2">
      <span className="text-sm text-muted-foreground">{label}</span>
      <Badge variant="outline">{value}</Badge>
    </div>
  )
}

// originSummary 只汇总模式与数量；来源列表永远不进入浏览器。
function originSummary(t: Translate, status: DeploymentAccessStatus | null, scope: "api" | "ws"): string {
  if (!status) return t("settings.deployment_unavailable")
  const mode = scope === "api" ? status.api_origin_mode : status.ws_origin_mode
  const count = scope === "api" ? status.api_origin_count : status.ws_origin_count
  if (mode === "all") return t("settings.origins_all")
  if (mode === "configured") return t("settings.origins_configured").replace("{count}", String(count))
  return t("settings.origins_same")
}

function SettingsSection({ title, children }: { title: string; children: ReactNode }) {
  return (
    <section className="grid gap-5 border-t pt-5 lg:grid-cols-[12rem_minmax(0,1fr)]">
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
