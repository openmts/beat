import { CheckIcon, DownloadIcon, EyeIcon, PackageIcon, Trash2Icon } from "lucide-react"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card, CardContent } from "@/components/ui/card"
import { useLocale } from "@/context/locale"
import { packageSizeLabel } from "@/lib/theme-declaration"
import { ThemeDeclaration } from "@/pages/admin/theme-declaration"
import type { ThemePackage } from "@/types/theme"

export type ThemePackageAction = "preview" | "activate" | "delete" | "export"
export function ThemePackageCard({ theme, disabled, onAction }: {
  theme: ThemePackage; disabled: boolean; onAction: (action: ThemePackageAction, theme: ThemePackage) => void
}) {
  const { t } = useLocale()
  return <Card className="overflow-hidden p-0">
    {theme.preview_url ? <img src={theme.preview_url} alt={`${theme.name} ${t("theme.preview_alt")}`} className="aspect-video w-full border-b object-cover object-top" loading="lazy" /> :
      <div className="flex aspect-video items-center justify-center border-b bg-muted"><PackageIcon className="size-10 text-muted-foreground" aria-hidden /></div>}
    <CardContent className="space-y-4 p-4"><div className="space-y-2">
      <div className="flex flex-wrap items-center gap-2"><h3 className="font-semibold">{theme.name}</h3><Badge variant="outline">{theme.version}</Badge>
        {theme.is_active && <Badge>{t("theme.active")}</Badge>}{theme.health_state === "unhealthy" && <Badge variant="destructive">{t("theme.unhealthy")}</Badge>}</div>
      <p className="text-sm text-muted-foreground">{theme.description}</p><p className="text-xs text-muted-foreground">{theme.author} · {t(`theme.runtime_${theme.runtime}`)}</p>
      <p className="text-xs text-muted-foreground">{t("theme.license")} {theme.license} · {t("theme.minimum_version")} {theme.minimum_beat_version} · {t("theme.package_size")} {packageSizeLabel(theme.archive_bytes)}</p>
    </div><div className="flex flex-wrap gap-2">
      <Button variant="outline" size="sm" disabled={disabled} onClick={() => onAction("preview", theme)}><EyeIcon />{t("theme.preview")}</Button>
      <Button size="sm" disabled={disabled || theme.is_active} onClick={() => onAction("activate", theme)}><CheckIcon />{t("theme.activate")}</Button>
      <Button variant="outline" size="icon-sm" aria-label={t("theme.export")} disabled={disabled} onClick={() => onAction("export", theme)}><DownloadIcon /></Button>
      <Button variant="outline" size="icon-sm" aria-label={t("theme.delete")} disabled={disabled || theme.is_active} onClick={() => onAction("delete", theme)}><Trash2Icon /></Button>
    </div><ThemeDeclaration theme={theme} /></CardContent>
  </Card>
}
