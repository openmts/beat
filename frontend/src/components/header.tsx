import { Link } from "react-router"
import { Moon, Sun, Settings } from "lucide-react"
import { Button } from "@/components/ui/button"
import { useTheme } from "@/context/theme"
import { useLocale } from "@/context/locale"
import { useSiteSettings } from "@/context/site-settings"

export function Header() {
  const { theme, toggleTheme } = useTheme()
  const { locale, setLocale, t } = useLocale()
  const { settings } = useSiteSettings()

  const toggleLocale = () => {
    setLocale(locale === "en" ? "zh-CN" : "en")
  }

  return (
    <header className="sticky top-0 z-30 flex h-12 shrink-0 items-center justify-between border-b bg-background/85 px-4 py-2 text-foreground backdrop-blur-md supports-[backdrop-filter]:bg-background/70">
      <Link to="/" className="flex min-w-0 items-center gap-2 text-lg font-bold">
        {settings.logo_url && (
          <img
            src={settings.logo_url}
            alt=""
            className="size-7 shrink-0 object-contain"
            referrerPolicy="no-referrer"
          />
        )}
        <span className="max-w-[50vw] truncate sm:max-w-sm">{settings.site_title}</span>
      </Link>
      <div className="flex shrink-0 items-center gap-0.5 sm:gap-1">
        <Button variant="ghost" size="icon" onClick={toggleTheme} title={t("app.theme")}>
          {theme === "dark" ? <Sun className="size-4" /> : <Moon className="size-4" />}
        </Button>
        <Button
          variant="ghost"
          size="icon"
          onClick={toggleLocale}
          title={t("app.language")}
          className="font-medium"
        >
          <span className="text-xs">{locale === "en" ? "中文" : "EN"}</span>
        </Button>
        <Link to="/admin">
          <Button variant="ghost" size="icon" title={t("app.admin")}>
            <Settings className="size-4" />
          </Button>
        </Link>
      </div>
    </header>
  )
}
