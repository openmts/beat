import { Link } from "react-router"
import { useLocale } from "@/context/locale"
import { useSiteSettings } from "@/context/site-settings"

export function PublicFooter() {
  const { t } = useLocale()
  const { settings } = useSiteSettings()

  return (
    <footer className="border-t py-6">
      <div className="container-page flex flex-col items-center justify-between gap-3 text-sm text-muted-foreground sm:flex-row">
        <p className="truncate">
          {settings.site_title}
          {settings.site_description ? ` — ${settings.site_description}` : ""}
        </p>
        <Link to="/admin" className="shrink-0 hover:text-foreground">
          {t("app.admin")}
        </Link>
      </div>
    </footer>
  )
}
