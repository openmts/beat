import { useCallback, useEffect, useState, type FormEvent } from "react"
import { KeyRoundIcon, PencilIcon, PlusIcon, ShieldCheckIcon, Trash2Icon } from "lucide-react"
import { Alert, AlertDescription } from "@/components/ui/alert"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { Field, FieldGroup, FieldLabel } from "@/components/ui/field"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Switch } from "@/components/ui/switch"
import { Skeleton } from "@/components/ui/skeleton"
import { useLocale } from "@/context/locale"
import {
  createOIDCProvider,
  deleteOIDCProvider,
  listAdminOIDCProviders,
  reauthenticateAdmin,
  updateOIDCProvider,
} from "@/lib/security-api"
import type { OIDCProvider } from "@/types/security"

interface ProviderForm {
  id?: string
  slug: string
  display_name: string
  issuer_url: string
  client_id: string
  client_secret: string
  scopes: string
  enabled: boolean
  sort_order: number
}

const emptyForm: ProviderForm = {
  slug: "", display_name: "", issuer_url: "", client_id: "",
  client_secret: "", scopes: "openid profile email", enabled: true, sort_order: 0,
}

export function SecurityOIDCProvidersPanel() {
  const { t } = useLocale()
  const [providers, setProviders] = useState<OIDCProvider[] | null>(null)
  const [error, setError] = useState("")
  const [dialogOpen, setDialogOpen] = useState(false)
  const [form, setForm] = useState<ProviderForm>(emptyForm)
  const [editing, setEditing] = useState(false)
  const [reauthOpen, setReauthOpen] = useState(false)
  const [reauthPassword, setReauthPassword] = useState("")
  const [reauthTOTP, setReauthTOTP] = useState("")
  const [saving, setSaving] = useState(false)

  const load = useCallback(async () => {
    try {
      setProviders(await listAdminOIDCProviders())
      setError("")
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : t("app.error"))
    }
  }, [t])

  useEffect(() => { void load() }, [load])

  const requiresReauth = (cause: unknown) => {
    const message = cause instanceof Error ? cause.message : ""
    return message.toLowerCase().includes("recent authentication")
  }

  const reauthenticate = async (event: FormEvent) => {
    event.preventDefault()
    try {
      await reauthenticateAdmin(reauthPassword, reauthTOTP)
      setReauthOpen(false)
      setReauthPassword("")
      setReauthTOTP("")
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : t("app.error"))
    }
  }

  const openCreate = () => {
    setEditing(false)
    setForm(emptyForm)
    setDialogOpen(true)
  }

  const openEdit = (provider: OIDCProvider) => {
    setEditing(true)
    setForm({
      id: provider.id, slug: provider.slug, display_name: provider.display_name,
      issuer_url: provider.issuer_url, client_id: provider.client_id,
      client_secret: "", scopes: provider.scopes.join(" "),
      enabled: provider.enabled, sort_order: provider.sort_order,
    })
    setDialogOpen(true)
  }

  const save = async (event: FormEvent) => {
    event.preventDefault()
    setSaving(true)
    setError("")
    try {
      const scopes = form.scopes.split(/\s+/).filter(Boolean)
      if (editing && form.id) {
        await updateOIDCProvider(form.id, {
          slug: form.slug, display_name: form.display_name, issuer_url: form.issuer_url,
          client_id: form.client_id, client_secret: form.client_secret || undefined,
          scopes, enabled: form.enabled, sort_order: form.sort_order,
        })
      } else {
        await createOIDCProvider({
          slug: form.slug, display_name: form.display_name, issuer_url: form.issuer_url,
          client_id: form.client_id, client_secret: form.client_secret,
          scopes, enabled: form.enabled, sort_order: form.sort_order,
        })
      }
      setDialogOpen(false)
      await load()
    } catch (cause) {
      if (requiresReauth(cause)) {
        setDialogOpen(false)
        setReauthOpen(true)
      }
      setError(cause instanceof Error ? cause.message : t("app.error"))
    } finally {
      setSaving(false)
    }
  }

  const remove = async (id: string) => {
    setError("")
    try {
      await deleteOIDCProvider(id)
      await load()
    } catch (cause) {
      if (requiresReauth(cause)) setReauthOpen(true)
      setError(cause instanceof Error ? cause.message : t("app.error"))
    }
  }

  const toggleEnabled = async (provider: OIDCProvider) => {
    setError("")
    try {
      await updateOIDCProvider(provider.id, {
        slug: provider.slug, display_name: provider.display_name, issuer_url: provider.issuer_url,
        client_id: provider.client_id, scopes: provider.scopes,
        enabled: !provider.enabled, sort_order: provider.sort_order,
      })
      await load()
    } catch (cause) {
      if (requiresReauth(cause)) setReauthOpen(true)
      setError(cause instanceof Error ? cause.message : t("app.error"))
    }
  }

  return (
    <Card>
      <CardHeader className="flex-row items-center justify-between space-y-0">
        <div>
          <CardTitle>{t("security.oidc_providers")}</CardTitle>
          <CardDescription>{t("security.oidc_providers_description")}</CardDescription>
        </div>
        <Button onClick={openCreate}><PlusIcon data-icon="inline-start" />{t("security.oidc_add_provider")}</Button>
      </CardHeader>
      <CardContent className="flex flex-col gap-3">
        {error && <Alert variant="destructive"><AlertDescription>{error}</AlertDescription></Alert>}
        {providers === null ? (
          <div className="flex flex-col gap-2"><Skeleton className="h-12 w-full" /><Skeleton className="h-12 w-full" /></div>
        ) : providers.length === 0 ? (
          <p className="text-sm text-muted-foreground">{t("security.oidc_no_providers")}</p>
        ) : (
          <ul className="flex flex-col gap-2">
            {providers.map((provider) => (
              <li key={provider.id} className="flex flex-wrap items-center justify-between gap-3 rounded-md border p-3">
                <div className="min-w-0">
                  <div className="flex items-center gap-2">
                    <span className="truncate text-sm font-medium">{provider.display_name}</span>
                    <Badge variant={provider.enabled ? "default" : "outline"}>
                      {provider.enabled ? t("security.enabled") : t("security.disabled")}
                    </Badge>
                    {provider.client_secret_configured && (
                      <Badge variant="outline">{t("security.oidc_secret_configured")}</Badge>
                    )}
                  </div>
                  <p className="truncate text-xs text-muted-foreground">{provider.issuer_url}</p>
                </div>
                <div className="flex items-center gap-2">
                  <div className="flex items-center gap-2">
                    <Label htmlFor={`enable-${provider.id}`} className="text-xs">{t("security.enabled")}</Label>
                    <Switch id={`enable-${provider.id}`} checked={provider.enabled}
                      onCheckedChange={() => void toggleEnabled(provider)} />
                  </div>
                  <Button variant="outline" size="icon" aria-label={t("security.oidc_edit_provider")}
                    onClick={() => openEdit(provider)}><PencilIcon /></Button>
                  <Button variant="outline" size="icon" aria-label={t("security.oidc_delete")}
                    onClick={() => void remove(provider.id)}><Trash2Icon /></Button>
                </div>
              </li>
            ))}
          </ul>
        )}
      </CardContent>

      <Dialog open={dialogOpen} onOpenChange={setDialogOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>{editing ? t("security.oidc_edit_provider") : t("security.oidc_add_provider")}</DialogTitle>
            <DialogDescription>{t("security.oidc_provider_form_description")}</DialogDescription>
          </DialogHeader>
          <form onSubmit={save}>
            <FieldGroup>
              <Field>
                <FieldLabel htmlFor="oidc-slug">{t("security.oidc_slug")}</FieldLabel>
                <Input id="oidc-slug" value={form.slug} onChange={(e) => setForm({ ...form, slug: e.target.value })} required />
              </Field>
              <Field>
                <FieldLabel htmlFor="oidc-display-name">{t("security.oidc_display_name")}</FieldLabel>
                <Input id="oidc-display-name" value={form.display_name}
                  onChange={(e) => setForm({ ...form, display_name: e.target.value })} required />
              </Field>
              <Field>
                <FieldLabel htmlFor="oidc-issuer">{t("security.oidc_issuer")}</FieldLabel>
                <Input id="oidc-issuer" type="url" placeholder="https://issuer.example.com" value={form.issuer_url}
                  onChange={(e) => setForm({ ...form, issuer_url: e.target.value })} required />
              </Field>
              <Field>
                <FieldLabel htmlFor="oidc-client-id">{t("security.oidc_client_id")}</FieldLabel>
                <Input id="oidc-client-id" value={form.client_id}
                  onChange={(e) => setForm({ ...form, client_id: e.target.value })} required />
              </Field>
              <Field>
                <FieldLabel htmlFor="oidc-client-secret">
                  {editing ? t("security.oidc_replace_secret") : t("security.oidc_client_secret")}
                </FieldLabel>
                <Input id="oidc-client-secret" type="password" value={form.client_secret}
                  onChange={(e) => setForm({ ...form, client_secret: e.target.value })}
                  required={!editing} />
                {editing && <p className="text-xs text-muted-foreground">{t("security.oidc_secret_blank_keeps")}</p>}
              </Field>
              <Field>
                <FieldLabel htmlFor="oidc-scopes">{t("security.oidc_scopes")}</FieldLabel>
                <Input id="oidc-scopes" value={form.scopes}
                  onChange={(e) => setForm({ ...form, scopes: e.target.value })} required />
              </Field>
              <Field>
                <FieldLabel htmlFor="oidc-sort">{t("security.oidc_sort_order")}</FieldLabel>
                <Input id="oidc-sort" type="number" min={0} value={form.sort_order}
                  onChange={(e) => setForm({ ...form, sort_order: Number(e.target.value) })} />
              </Field>
              <div className="flex items-center gap-2">
                <Switch id="oidc-enabled" checked={form.enabled}
                  onCheckedChange={(checked) => setForm({ ...form, enabled: checked })} />
                <Label htmlFor="oidc-enabled">{t("security.enabled")}</Label>
              </div>
              <Button type="submit" disabled={saving}>
                <KeyRoundIcon data-icon="inline-start" />{editing ? t("security.oidc_update") : t("security.oidc_create")}
              </Button>
            </FieldGroup>
          </form>
        </DialogContent>
      </Dialog>

      <Dialog open={reauthOpen} onOpenChange={setReauthOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>{t("security.reauthenticate")}</DialogTitle>
            <DialogDescription>{t("security.reauthenticated")}</DialogDescription>
          </DialogHeader>
          <form onSubmit={reauthenticate}>
            <FieldGroup>
              <Field>
                <FieldLabel htmlFor="oidc-reauth-password">{t("security.current_password")}</FieldLabel>
                <Input id="oidc-reauth-password" type="password" value={reauthPassword}
                  onChange={(e) => setReauthPassword(e.target.value)} required />
              </Field>
              <Field>
                <FieldLabel htmlFor="oidc-reauth-totp">{t("security.totp_code")}</FieldLabel>
                <Input id="oidc-reauth-totp" inputMode="numeric" maxLength={6} value={reauthTOTP}
                  onChange={(e) => setReauthTOTP(e.target.value)} />
              </Field>
              <Button type="submit"><ShieldCheckIcon data-icon="inline-start" />{t("security.reauthenticate")}</Button>
            </FieldGroup>
          </form>
        </DialogContent>
      </Dialog>
    </Card>
  )
}