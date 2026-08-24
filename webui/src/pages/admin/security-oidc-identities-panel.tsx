import { useCallback, useEffect, useState } from "react"
import { Link2Icon, UnlinkIcon } from "lucide-react"
import { Alert, AlertDescription } from "@/components/ui/alert"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Skeleton } from "@/components/ui/skeleton"
import { useLocale } from "@/context/locale"
import {
  beginOIDCLink,
  listMyExternalIdentities,
  listPublicOIDCProviders,
  unlinkExternalIdentity,
} from "@/lib/security-api"
import type { ExternalIdentity, OIDCProviderPublic } from "@/types/security"

export function SecurityOIDCIdentitiesPanel() {
  const { t } = useLocale()
  const [identities, setIdentities] = useState<ExternalIdentity[] | null>(null)
  const [providers, setProviders] = useState<OIDCProviderPublic[]>([])
  const [error, setError] = useState("")
  const [busyId, setBusyId] = useState<string | null>(null)

  const load = useCallback(async () => {
    try {
      const [found, enabledProviders] = await Promise.all([
        listMyExternalIdentities(),
        listPublicOIDCProviders(),
      ])
      setIdentities(found)
      setProviders(enabledProviders)
      setError("")
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : t("app.error"))
    }
  }, [t])

  useEffect(() => { void load() }, [load])

  const unlink = async (id: string) => {
    setBusyId(id)
    setError("")
    try {
      await unlinkExternalIdentity(id)
      await load()
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : t("app.error"))
    } finally {
      setBusyId(null)
    }
  }

  const link = async (providerId: string) => {
    setBusyId(providerId)
    setError("")
    try {
      const authorizationURL = await beginOIDCLink(providerId, "/admin/security")
      window.location.assign(authorizationURL)
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : t("app.error"))
      setBusyId(null)
    }
  }

  return (
    <Card>
      <CardHeader>
        <CardTitle>{t("security.oidc_identities")}</CardTitle>
        <CardDescription>{t("security.oidc_identities_description")}</CardDescription>
      </CardHeader>
      <CardContent className="flex flex-col gap-4">
        {error && <Alert variant="destructive"><AlertDescription>{error}</AlertDescription></Alert>}
        {identities === null ? (
          <div className="flex flex-col gap-2"><Skeleton className="h-10 w-full" /><Skeleton className="h-10 w-full" /></div>
        ) : (
          <ul className="flex flex-col gap-2">
            {identities.length === 0 && (
              <li className="text-sm text-muted-foreground">{t("security.oidc_no_identities")}</li>
            )}
            {identities.map((identity) => (
              <li key={identity.id} className="flex items-center justify-between gap-3 rounded-md border p-3">
                <div className="min-w-0">
                  <div className="flex items-center gap-2">
                    <span className="truncate text-sm font-medium">{identity.provider_name}</span>
                    {identity.email_verified && <Badge variant="outline">{t("security.oidc_verified")}</Badge>}
                  </div>
                  <p className="truncate text-xs text-muted-foreground">
                    {identity.display_name || identity.preferred_username || identity.email}
                  </p>
                  <p className="text-xs text-muted-foreground">
                    {t("security.oidc_last_login")} {new Date(identity.last_login_at).toLocaleString()}
                  </p>
                </div>
                <Button variant="outline" size="sm" disabled={busyId === identity.id}
                  onClick={() => void unlink(identity.id)}>
                  <UnlinkIcon data-icon="inline-start" />{t("security.oidc_unlink")}
                </Button>
              </li>
            ))}
          </ul>
        )}
        {providers.length > 0 && (
          <div className="flex flex-col gap-2">
            <div className="text-sm font-medium">{t("security.oidc_link")}</div>
            {providers.map((provider) => {
              const already = identities?.some((identity) => identity.provider_id === provider.id)
              return (
                <Button key={provider.id} variant="outline" className="w-full"
                  disabled={already || busyId === provider.id}
                  onClick={() => void link(provider.id)}>
                  <Link2Icon data-icon="inline-start" />
                  {already ? t("security.oidc_already_linked") : `${t("security.oidc_link")} · ${provider.display_name}`}
                </Button>
              )
            })}
          </div>
        )}
      </CardContent>
    </Card>
  )
}