import { useTheme } from "@/context/theme"
import { useLocale } from "@/context/locale"
import { SidebarTrigger } from "@/components/ui/sidebar"
import { Button } from "@/components/ui/button"
import { SunIcon, MoonIcon, LanguagesIcon, LogOutIcon } from "lucide-react"
import { useAuth } from "@/context/auth"
import { useSiteSettings } from "@/context/site-settings"

function AdminHeader() {
  const { theme, toggleTheme } = useTheme()
  const { locale, setLocale, t } = useLocale()
  const { logout } = useAuth()
  const { settings } = useSiteSettings()

  return (
    <header className="sticky top-0 z-20 flex h-12 shrink-0 items-center gap-2 border-b bg-background/85 px-3 backdrop-blur-md supports-[backdrop-filter]:bg-background/70 sm:px-4">
      <SidebarTrigger />
      {settings.logo_url && (
        <img
          src={settings.logo_url}
          alt=""
          className="size-6 shrink-0 object-contain"
          referrerPolicy="no-referrer"
        />
      )}
      <span className="min-w-0 max-w-[38vw] truncate font-heading text-base font-medium sm:max-w-[40vw]">
        {settings.site_title}
      </span>
      <div className="ml-auto flex shrink-0 items-center gap-1">
        <Button
          variant="ghost"
          size="icon-sm"
          onClick={toggleTheme}
          aria-label={t("app.theme")}
        >
          {theme === "dark" ? (
            <SunIcon />
          ) : (
            <MoonIcon />
          )}
        </Button>
        <Button
          variant="ghost"
          size="icon-sm"
          onClick={() =>
            setLocale(locale === "en" ? "zh-CN" : "en")
          }
          aria-label={t("app.language")}
        >
          <LanguagesIcon />
        </Button>
        <Button
          variant="ghost"
          size="icon-sm"
          onClick={() => void logout()}
          aria-label={t("auth.logout")}
        >
          <LogOutIcon />
        </Button>
      </div>
    </header>
  )
}

export default AdminHeader
