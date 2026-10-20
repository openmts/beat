import { DownloadIcon, PencilIcon, RefreshCwIcon, KeyRoundIcon, Trash2Icon } from "lucide-react"
import { Alert, AlertDescription } from "@/components/ui/alert"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Label } from "@/components/ui/label"
import { Switch } from "@/components/ui/switch"
import { useLocale } from "@/context/locale"
import { configurationSummary } from "@/lib/theme-declaration"
import { formatDateTime } from "@/lib/intl-format"
import type { ThemeCatalog, ThemeCatalogEntry, ThemeMarketSource, ThemePackage } from "@/types/theme"

interface SourceCardProps {
  source: ThemeMarketSource
  catalog?: ThemeCatalog
  installed: ThemePackage[]
  conflict?: string
  busy: boolean
  onAction: (kind: "refresh" | "edit" | "toggle" | "delete", source: ThemeMarketSource) => void
  onInstall: (source: ThemeMarketSource, entry: ThemeCatalogEntry) => void
}

export function ThemeMarketSourceCard({ source, catalog, installed, conflict, busy, onAction, onInstall }: SourceCardProps) {
  const { t } = useLocale()
  return <article aria-label={source.name} className="min-w-0 rounded-lg border">
    <div className="flex flex-col gap-4 p-4 sm:flex-row sm:items-start sm:justify-between">
      <div className="min-w-0 flex-1"><h3 className="flex flex-wrap items-center gap-2 text-sm font-semibold"><KeyRoundIcon className="size-4 text-muted-foreground" />{source.name}
        <Badge variant={source.enabled ? "secondary" : "outline"}>{t(source.enabled ? "security.enabled" : "security.disabled")}</Badge></h3>
        <p className="mt-1 break-all text-xs text-muted-foreground">{source.catalog_url}</p>
        <p className="mt-3 text-xs text-muted-foreground">{t("theme.public_identity")}</p><code className="block break-all text-xs">{source.key_fingerprint}</code>
      </div><div className="flex flex-wrap items-center gap-2">
        <Label htmlFor={`market-${source.id}`} className="text-xs">{t("security.enabled")}</Label>
        <Switch id={`market-${source.id}`} checked={source.enabled} disabled={busy} onCheckedChange={() => onAction("toggle", source)} />
        <Button variant="outline" size="icon" disabled={busy || !source.enabled} aria-label={t("theme.refresh")} onClick={() => onAction("refresh", source)}><RefreshCwIcon /></Button>
        <Button variant="outline" size="icon" disabled={busy} aria-label={t("theme.edit_source")} onClick={() => onAction("edit", source)}><PencilIcon /></Button>
        <Button variant="outline" size="icon" disabled={busy} aria-label={t("theme.delete")} onClick={() => onAction("delete", source)}><Trash2Icon /></Button>
      </div>
    </div>
    {conflict && <div className="px-4 pb-4"><Alert variant="destructive"><AlertDescription>{t("theme.catalog_changed", { selection: conflict })}</AlertDescription></Alert></div>}
    {catalog ? <div className="space-y-4 border-t p-4"><MarketCatalogStatus catalog={catalog} />
      <MarketCatalogEntries source={source} catalog={catalog} installed={installed} disabled={busy || !source.enabled || !!conflict} onInstall={onInstall} /></div> :
      <p className="border-t p-4 text-xs text-muted-foreground">{t(source.last_status === "failed" ? "theme.catalog_unavailable" : "theme.catalog_hint")}</p>}
  </article>
}

function MarketCatalogStatus({ catalog }: { catalog: ThemeCatalog }) {
  const { t, locale } = useLocale()
  return <div className="space-y-2"><div className="flex flex-wrap items-center gap-2 text-xs text-muted-foreground">
    <Badge variant={catalog.stale ? "outline" : "secondary"}>{t(catalog.stale ? "theme.catalog_stale" : catalog.cached ? "theme.catalog_cached" : "theme.catalog_verified")}</Badge>
    <span>{t("theme.catalog_fetched")} {marketDate(catalog.fetched_at, locale)}</span>
    <span>{t("theme.cache_valid_until")} {marketDate(catalog.cache_expires_at, locale)}</span>
    <span>{t("theme.catalog_valid_until")} {marketDate(catalog.expires_at, locale)}</span>
  </div>{catalog.stale && <p role="status" className="text-sm text-muted-foreground">{t("theme.stale_explanation")}</p>}</div>
}

function MarketCatalogEntries({ source, catalog, installed, disabled, onInstall }: {
  source: ThemeMarketSource; catalog: ThemeCatalog; installed: ThemePackage[]; disabled: boolean
  onInstall: SourceCardProps["onInstall"]
}) {
  const { t } = useLocale()
  if (catalog.themes.length === 0) return <p className="text-sm text-muted-foreground">{t("theme.catalog_empty")}</p>
  return <div className="grid gap-3 lg:grid-cols-2">{catalog.themes.map((entry) => {
    const versions = installed.filter((pkg) => pkg.theme_id === entry.theme_id && pkg.state === "installed")
    const exists = versions.some((pkg) => pkg.version === entry.version)
    return <div key={entry.theme_id} className="flex min-w-0 flex-col gap-3 rounded-md border bg-muted/20 p-3">
      <div className="flex flex-wrap items-baseline justify-between gap-2"><h4 className="text-sm font-semibold">{entry.name}</h4><code className="text-xs">{entry.version}</code></div>
      <p className="break-words text-xs text-muted-foreground">{entry.description}</p>
      <p className="text-xs text-muted-foreground">{t(entry.runtime === "declarative" ? "theme.runtime_declarative" : "theme.runtime_sandboxed")} · {entry.author}</p>
      {/* 目录里带声明配置的条目在安装前就说明会放开哪些开关；没有该项的旧目录不显示这一行。 */}
      {entry.configuration?.items?.length ? <p className="text-xs text-muted-foreground">{t("theme.configurable")}: {configurationSummary(entry.configuration, t).join(" · ")}</p> : null}
      {versions.length > 0 && <p className="text-xs text-muted-foreground">{t("theme.installed_versions", { versions: versions.map((pkg) => pkg.version).join(", ") })}</p>}
      <div className="mt-auto flex flex-wrap items-center justify-between gap-2"><span className="text-xs text-muted-foreground">Beat ≥ {entry.minimum_beat_version}{entry.maximum_beat_version && ` · ≤ ${entry.maximum_beat_version}`}</span>
        <Button variant="outline" size="sm" disabled={disabled || exists} onClick={() => onInstall(source, entry)}><DownloadIcon />{t(exists ? "theme.installed" : "theme.install")}</Button></div>
    </div>
  })}</div>
}

function marketDate(value: string, locale: string): string {
  return formatDateTime(locale, value)
}
