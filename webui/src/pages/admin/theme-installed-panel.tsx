import { useCallback, useEffect, useState } from "react"
import { RefreshCwIcon, UploadIcon } from "lucide-react"
import { Alert, AlertDescription } from "@/components/ui/alert"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { useLocale } from "@/context/locale"
import { useThemeDeletion } from "@/hooks/use-theme-deletion"
import { ThemeCleanupNotices } from "@/pages/admin/theme-cleanup-notices"
import { useThemeOperation } from "@/hooks/use-theme-operation"
import { activateTheme, exportTheme, listAdminThemes, rollbackTheme, uploadTheme, useBuiltInTheme } from "@/lib/theme-api"
import { ThemeOperationDialog } from "@/pages/admin/theme-operation-dialog"
import { ThemePackageCard, type ThemePackageAction } from "@/pages/admin/theme-package-card"
import { ThemeExampleLinks } from "@/pages/admin/theme-example-links"
import { readExampleArchive } from "@/lib/theme-examples"
import { ThemeUploadDialog } from "@/pages/admin/theme-upload-dialog"
import { ThemePreview } from "@/pages/admin/theme-preview"
import type { ThemePackage } from "@/types/theme"

export function InstalledThemes({ revision = 0 }: { revision?: number }) {
  const { t } = useLocale()
  const { packages, error, load } = useInstalledThemes(revision)
  const flow = useThemeOperation()
  const deletion = useThemeDeletion()
  const [uploadOpen, setUploadOpen] = useState(false)
  const [preview, setPreview] = useState<ThemePackage | null>(null)
  const perform = (key: string, run: () => Promise<unknown>, confirm = true) => flow.request({
    label: t(`theme.${key}`), description: t(`theme.confirm_${key}`), run: async () => { await run(); await load() },
  }, confirm)
  const action = (kind: ThemePackageAction, theme: ThemePackage) => {
    if (kind === "preview") { setPreview(theme); return }
    const actions = { activate: () => activateTheme(theme.id), delete: () => deletion.remove(theme.id), export: () => downloadTheme(theme.id) }
    perform(kind, actions[kind], kind !== "export")
  }
  return <div className="space-y-5"><Card><CardHeader className="flex flex-wrap items-start justify-between gap-3">
    <div><CardTitle><h2>{t("theme.installed_title")}</h2></CardTitle><CardDescription>{t("theme.installed_description")}</CardDescription></div>
    <div className="flex flex-wrap gap-2"><Button variant="outline" disabled={flow.loading} onClick={() => perform("rollback", rollbackTheme)}><RefreshCwIcon />{t("theme.rollback")}</Button>
      <Button disabled={flow.loading} onClick={() => setUploadOpen(true)}><UploadIcon />{t("theme.upload")}</Button></div>
  </CardHeader><CardContent className="space-y-5">
    <ThemeCleanupNotices pending={deletion.pending} disabled={flow.loading} onRetry={(id) => perform("retry_cleanup", () => deletion.remove(id), false)} />
    {error && <Alert variant="destructive"><AlertDescription>{error}</AlertDescription></Alert>}
    <BuiltInTheme active={packages !== null && !packages.some((theme) => theme.is_active)} disabled={flow.loading || packages === null}
      onActivate={() => perform("use_builtin", useBuiltInTheme)} />
    {packages === null ? <p className="text-sm text-muted-foreground">{t("app.loading")}</p> : packages.length === 0 ?
      <p className="text-sm text-muted-foreground">{t("theme.no_themes")}</p> : <div className="grid gap-4 lg:grid-cols-2 xl:grid-cols-3">
        {packages.map((theme) => <ThemePackageCard key={theme.id} theme={theme} disabled={flow.loading} onAction={action} />)}</div>}
  </CardContent></Card><ThemeExampleLinks disabled={flow.loading} onInstall={(id) => perform("install", async () => uploadTheme(await readExampleArchive(id)), false)} />
    <ThemeUploadDialog open={uploadOpen} onOpenChange={setUploadOpen} onUpload={(file) => perform("upload", () => uploadTheme(file), false)} />
    {preview && <ThemePreview theme={preview} onClose={() => setPreview(null)} onSaved={load} />}
    <ThemeOperationDialog flow={flow} />
  </div>
}

function useInstalledThemes(revision: number) {
  const { t } = useLocale()
  const [packages, setPackages] = useState<ThemePackage[] | null>(null)
  const [error, setError] = useState("")
  const load = useCallback(async () => {
    try { setPackages(await listAdminThemes()); setError("") }
    catch (cause) { setError(cause instanceof Error ? cause.message : t("app.error")) }
  }, [t])
  useEffect(() => { void load() }, [load, revision])
  return { packages, error, load }
}

function BuiltInTheme({ active, disabled, onActivate }: { active: boolean; disabled: boolean; onActivate: () => void }) {
  const { t } = useLocale()
  return <div className="flex flex-wrap items-center justify-between gap-3 rounded-lg border p-4">
    <div><h3 className="text-sm font-semibold">{t("theme.builtin")}</h3><p className="mt-1 text-xs text-muted-foreground">{t("theme.builtin_description")}</p></div>
    <Button variant="outline" disabled={disabled || active} onClick={onActivate}>{t(active ? "theme.builtin_active" : "theme.use_builtin")}</Button>
  </div>
}

async function downloadTheme(id: string) {
  const archive = await exportTheme(id)
  const url = URL.createObjectURL(archive)
  try {
    const link = document.createElement("a")
    link.href = url
    link.download = `${id}.theme.zip`
    link.click()
  } finally { URL.revokeObjectURL(url) }
}
