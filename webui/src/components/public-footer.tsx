import { Link } from "react-router"
import { useLocale } from "@/context/locale"
import { useSiteSettings } from "@/context/site-settings"
import { themeShell, usePublicTheme } from "@/lib/theme-presentation"

const footerClasses = {
  plain: "border-t py-6",
  inset: "border-t bg-card/55 py-6 backdrop-blur-sm",
  minimal: "border-t border-transparent py-4",
} as const

export function PublicFooter({ wide = false }: { wide?: boolean }) {
  const { t } = useLocale()
  const { settings } = useSiteSettings()
  const definition = usePublicTheme()?.definition
  const footer = definition ? themeShell(definition).footer : "plain"

  return (
    <footer data-theme-footer={footer} className={footerClasses[footer]}>
      <div className={`${wide ? "mx-auto w-full max-w-[1400px] px-4 sm:px-6" : "container-page"} flex min-w-0 flex-col items-center justify-between gap-3 text-sm text-muted-foreground sm:flex-row`}>
        <p className="w-full min-w-0 truncate text-center sm:w-auto sm:text-left">
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
