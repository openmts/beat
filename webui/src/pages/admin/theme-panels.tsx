import { useCallback, useEffect, useMemo, useRef, useState, type FormEvent } from "react"
import {
  CheckIcon, DownloadIcon, Link2Icon, PackageIcon, PlusIcon,
  RefreshCwIcon, Trash2Icon, UploadIcon,
} from "lucide-react"
import { Alert, AlertDescription } from "@/components/ui/alert"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { Field, FieldGroup, FieldLabel } from "@/components/ui/field"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Switch } from "@/components/ui/switch"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { useLocale } from "@/context/locale"
import {
  activateTheme, createThemeMarket, deleteTheme, deleteThemeMarket, exportTheme,
  installMarketTheme, listAdminThemes, listThemeMarkets, refreshThemeCatalog,
  rollbackTheme, updateThemeMarket, uploadTheme,
} from "@/lib/theme-api"
import type { ThemeCatalogEntry, ThemeMarketSource, ThemePackage } from "@/types/theme"

export function ThemesPanel() {
  const { t } = useLocale()
  return (
    <Tabs defaultValue="installed">
      <TabsList>
        <TabsTrigger value="installed">{t("theme.installed")}</TabsTrigger>
        <TabsTrigger value="market">{t("theme.market")}</TabsTrigger>
      </TabsList>
      <TabsContent value="installed"><InstalledThemes /></TabsContent>
      <TabsContent value="market"><MarketSources /></TabsContent>
    </Tabs>
  )
}

function InstalledThemes() {
  const { t } = useLocale()
  const [packages, setPackages] = useState<ThemePackage[] | null>(null)
  const [error, setError] = useState("")
  const [uploadOpen, setUploadOpen] = useState(false)
  const [uploading, setUploading] = useState(false)
  const fileRef = useRef<HTMLInputElement>(null)

  const load = useCallback(async () => {
    try {
      setPackages(await listAdminThemes())
      setError("")
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : t("app.error"))
    }
  }, [t])
  useEffect(() => { void load() }, [load])

  const upload = async (event: FormEvent) => {
    event.preventDefault()
    const file = fileRef.current?.files?.[0]
    if (!file) return
    setUploading(true)
    setError("")
    try {
      await uploadTheme(file)
      setUploadOpen(false)
      await load()
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : t("app.error"))
    } finally {
      setUploading(false)
    }
  }

  const activate = async (id: string) => {
    try {
      await activateTheme(id)
      await load()
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : t("app.error"))
    }
  }

  const rollback = async () => {
    try {
      await rollbackTheme()
      await load()
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : t("app.error"))
    }
  }

  const remove = async (id: string) => {
    try {
      await deleteTheme(id)
      await load()
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : t("app.error"))
    }
  }

  const exportPackage = async (id: string) => {
    try {
      const blob = await exportTheme(id)
      const link = document.createElement("a")
      link.href = URL.createObjectURL(blob)
      link.download = `${id}.theme.zip`
      link.click()
      URL.revokeObjectURL(link.href)
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : t("app.error"))
    }
  }

  return (
    <Card>
      <CardHeader className="flex-row items-center justify-between space-y-0">
        <div>
          <CardTitle>{t("theme.installed_title")}</CardTitle>
          <CardDescription>{t("theme.installed_description")}</CardDescription>
        </div>
        <div className="flex items-center gap-2">
          <Button variant="outline" onClick={() => void rollback()}>
            <RefreshCwIcon data-icon="inline-start" />{t("theme.rollback")}
          </Button>
          <Button onClick={() => setUploadOpen(true)}>
            <UploadIcon data-icon="inline-start" />{t("theme.upload")}
          </Button>
        </div>
      </CardHeader>
      <CardContent className="flex flex-col gap-3">
        {error && <Alert variant="destructive"><AlertDescription>{error}</AlertDescription></Alert>}
        {packages === null ? <p className="text-sm text-muted-foreground">{t("app.loading")}</p> :
          packages.length === 0 ? (
            <p className="text-sm text-muted-foreground">{t("theme.no_themes")}</p>
          ) : (
            <ul className="flex flex-col gap-2">
              {packages.map((pkg) => (
                <li key={pkg.id} className="flex flex-wrap items-center justify-between gap-3 rounded-md border p-3">
                  <div className="min-w-0">
                    <div className="flex items-center gap-2">
                      <PackageIcon data-icon="inline-start" className="text-muted-foreground" />
                      <span className="truncate text-sm font-medium">{pkg.name}</span>
                      <Badge variant="outline">{pkg.version}</Badge>
                      {pkg.is_active && <Badge>{t("theme.active")}</Badge>}
                      {pkg.health_state === "unhealthy" && <Badge variant="destructive">{t("theme.unhealthy")}</Badge>}
                    </div>
                    <p className="truncate text-xs text-muted-foreground">{pkg.description}</p>
                    <p className="text-xs text-muted-foreground">
                      {pkg.author} · {pkg.runtime} · {t("theme.source")}: {pkg.source_type}
                    </p>
                  </div>
                  <div className="flex items-center gap-2">
                    <Button variant="outline" size="icon" aria-label={t("theme.export")}
                      onClick={() => void exportPackage(pkg.id)}><DownloadIcon /></Button>
                    <Button variant="outline" size="icon" aria-label={t("theme.activate")}
                      disabled={pkg.is_active} onClick={() => void activate(pkg.id)}><CheckIcon /></Button>
                    <Button variant="outline" size="icon" aria-label={t("theme.delete")}
                      disabled={pkg.is_active} onClick={() => void remove(pkg.id)}><Trash2Icon /></Button>
                  </div>
                </li>
              ))}
            </ul>
          )}
      </CardContent>

      <Dialog open={uploadOpen} onOpenChange={setUploadOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>{t("theme.upload_title")}</DialogTitle>
            <DialogDescription>{t("theme.upload_description")}</DialogDescription>
          </DialogHeader>
          <form onSubmit={upload}>
            <FieldGroup>
              <Field>
                <FieldLabel htmlFor="theme-archive">{t("theme.upload")}</FieldLabel>
                <Input id="theme-archive" type="file" accept=".zip" ref={fileRef} required />
              </Field>
              <Button type="submit" disabled={uploading}>
                <UploadIcon data-icon="inline-start" />{t("theme.upload")}
              </Button>
            </FieldGroup>
          </form>
        </DialogContent>
      </Dialog>
    </Card>
  )
}

function MarketSources() {
  const { t } = useLocale()
  const [sources, setSources] = useState<ThemeMarketSource[]>([])
  const [catalogs, setCatalogs] = useState<Record<string, ThemeCatalogEntry[]>>({})
  const [error, setError] = useState("")
  const [dialogOpen, setDialogOpen] = useState(false)
  const [name, setName] = useState("")
  const [catalogURL, setCatalogURL] = useState("")
  const [publicKey, setPublicKey] = useState("")
  const [fingerprint, setFingerprint] = useState("")
  const [enabled, setEnabled] = useState(true)

  const load = useCallback(async () => {
    try {
      setSources(await listThemeMarkets())
      setError("")
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : t("app.error"))
    }
  }, [t])
  useEffect(() => { void load() }, [load])

  const saveSource = async (event: FormEvent) => {
    event.preventDefault()
    try {
      await createThemeMarket({
        name, catalog_url: catalogURL, public_key: publicKey,
        key_fingerprint: fingerprint, enabled, sort_order: 0,
      })
      setDialogOpen(false)
      setName(""); setCatalogURL(""); setPublicKey(""); setFingerprint(""); setEnabled(true)
      await load()
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : t("app.error"))
    }
  }

  const refresh = async (sourceId: string) => {
    try {
      const result = await refreshThemeCatalog(sourceId)
      if (result && !("cached" in result)) {
        setCatalogs((current) => ({ ...current, [sourceId]: result.themes }))
      }
      await load()
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : t("app.error"))
    }
  }

  const install = async (sourceId: string, themeId: string) => {
    try {
      await installMarketTheme(sourceId, themeId)
      setError("")
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : t("app.error"))
    }
  }

  const toggleEnabled = async (source: ThemeMarketSource) => {
    try {
      await updateThemeMarket(source.id, {
        name: source.name, catalog_url: source.catalog_url, public_key: "",
        key_fingerprint: source.key_fingerprint, enabled: !source.enabled, sort_order: source.sort_order,
      })
      await load()
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : t("app.error"))
    }
  }

  const removeSource = async (id: string) => {
    try {
      await deleteThemeMarket(id)
      await load()
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : t("app.error"))
    }
  }

  const mergedEntries = useMemo(() => {
    const entries = new Map<string, ThemeCatalogEntry & { source_id: string }>()
    for (const [sourceId, themes] of Object.entries(catalogs)) {
      for (const theme of themes) entries.set(`${sourceId}:${theme.theme_id}`, { ...theme, source_id: sourceId })
    }
    return [...entries.values()]
  }, [catalogs])

  return (
    <Card>
      <CardHeader className="flex-row items-center justify-between space-y-0">
        <div>
          <CardTitle>{t("theme.market_title")}</CardTitle>
          <CardDescription>{t("theme.market_description")}</CardDescription>
        </div>
        <Button onClick={() => setDialogOpen(true)}>
          <PlusIcon data-icon="inline-start" />{t("theme.add_source")}
        </Button>
      </CardHeader>
      <CardContent className="flex flex-col gap-4">
        {error && <Alert variant="destructive"><AlertDescription>{error}</AlertDescription></Alert>}
        {sources.length === 0 && <p className="text-sm text-muted-foreground">{t("theme.no_sources")}</p>}
        {sources.map((source) => (
          <div key={source.id} className="flex flex-col gap-2 rounded-md border p-3">
            <div className="flex flex-wrap items-center justify-between gap-3">
              <div className="min-w-0">
                <div className="flex items-center gap-2">
                  <Link2Icon data-icon="inline-start" className="text-muted-foreground" />
                  <span className="truncate text-sm font-medium">{source.name}</span>
                  <Badge variant={source.enabled ? "default" : "outline"}>
                    {source.enabled ? t("security.enabled") : t("security.disabled")}
                  </Badge>
                </div>
                <p className="truncate text-xs text-muted-foreground">{source.catalog_url}</p>
              </div>
              <div className="flex items-center gap-2">
                <div className="flex items-center gap-2">
                  <Label htmlFor={`market-${source.id}`} className="text-xs">{t("security.enabled")}</Label>
                  <Switch id={`market-${source.id}`} checked={source.enabled}
                    onCheckedChange={() => void toggleEnabled(source)} />
                </div>
                <Button variant="outline" size="icon" aria-label={t("theme.refresh")}
                  onClick={() => void refresh(source.id)}><RefreshCwIcon /></Button>
                <Button variant="outline" size="icon" aria-label={t("theme.delete")}
                  onClick={() => void removeSource(source.id)}><Trash2Icon /></Button>
              </div>
            </div>
            <div className="flex flex-col gap-2">
              {(catalogs[source.id] ?? []).map((entry) => (
                <div key={entry.theme_id} className="flex items-center justify-between gap-3 rounded-md bg-muted/40 p-2">
                  <div className="min-w-0">
                    <span className="truncate text-sm font-medium">{entry.name}</span>
                    <p className="truncate text-xs text-muted-foreground">{entry.version} · {entry.author}</p>
                  </div>
                  <Button variant="outline" size="sm" onClick={() => void install(source.id, entry.theme_id)}>
                    <DownloadIcon data-icon="inline-start" />{t("theme.install")}
                  </Button>
                </div>
              ))}
            </div>
          </div>
        ))}
        {mergedEntries.length > 0 && (
          <div className="flex flex-col gap-2">
            <div className="text-sm font-medium">{t("theme.catalog")}</div>
            {mergedEntries.map((entry) => (
              <div key={`${entry.source_id}:${entry.theme_id}`} className="flex items-center justify-between gap-3 rounded-md border p-2">
                <div className="min-w-0">
                  <span className="truncate text-sm font-medium">{entry.name}</span>
                  <p className="truncate text-xs text-muted-foreground">{entry.version} · {entry.author}</p>
                </div>
                <Button variant="outline" size="sm" onClick={() => void install(entry.source_id, entry.theme_id)}>
                  <DownloadIcon data-icon="inline-start" />{t("theme.install")}
                </Button>
              </div>
            ))}
          </div>
        )}
      </CardContent>

      <Dialog open={dialogOpen} onOpenChange={setDialogOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>{t("theme.add_source")}</DialogTitle>
            <DialogDescription>{t("theme.add_source_description")}</DialogDescription>
          </DialogHeader>
          <form onSubmit={saveSource}>
            <FieldGroup>
              <Field>
                <FieldLabel htmlFor="market-name">{t("theme.source_name")}</FieldLabel>
                <Input id="market-name" value={name} onChange={(e) => setName(e.target.value)} required />
              </Field>
              <Field>
                <FieldLabel htmlFor="market-url">{t("theme.source_url")}</FieldLabel>
                <Input id="market-url" type="url" placeholder="https://market.example/catalog.json"
                  value={catalogURL} onChange={(e) => setCatalogURL(e.target.value)} required />
              </Field>
              <Field>
                <FieldLabel htmlFor="market-key">{t("theme.source_key")}</FieldLabel>
                <Input id="market-key" value={publicKey} onChange={(e) => setPublicKey(e.target.value)} required />
              </Field>
              <Field>
                <FieldLabel htmlFor="market-fingerprint">{t("theme.source_fingerprint")}</FieldLabel>
                <Input id="market-fingerprint" value={fingerprint} onChange={(e) => setFingerprint(e.target.value)} />
              </Field>
              <div className="flex items-center gap-2">
                <Switch id="market-enabled" checked={enabled}
                  onCheckedChange={setEnabled} />
                <Label htmlFor="market-enabled">{t("security.enabled")}</Label>
              </div>
              <Button type="submit"><PlusIcon data-icon="inline-start" />{t("theme.add_source")}</Button>
            </FieldGroup>
          </form>
        </DialogContent>
      </Dialog>
    </Card>
  )
}