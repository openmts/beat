import {
  createContext,
  useCallback,
  useContext,
  useState,
  type ReactNode,
} from "react"
import {
  baseTranslations,
  type Locale,
} from "@/context/base-translations"
import { monitorTranslations } from "@/context/monitor-translations"
import { trafficReportTranslations } from "@/context/traffic-report-translations"
import { siteSettingsTranslations } from "@/context/site-settings-translations"
import { maintenanceTranslations } from "@/context/maintenance-translations"
import { securityTranslations } from "@/context/security-translations"
import { backupTranslations } from "@/context/backup-translations"

interface LocaleContextValue {
  locale: Locale
  setLocale: (locale: Locale) => void
  t: (key: string) => string
}

const LocaleContext = createContext<LocaleContextValue | undefined>(undefined)

export function LocaleProvider({ children }: { children: ReactNode }) {
  const [locale, setLocale] = useState<Locale>(() => {
    const stored = localStorage.getItem("locale")
    if (stored === "en" || stored === "zh-CN") return stored
    return "en"
  })

  const t = useCallback(
    (key: string): string => {
      const catalogs = [
        baseTranslations,
        monitorTranslations,
        trafficReportTranslations,
        siteSettingsTranslations,
        maintenanceTranslations,
        securityTranslations,
        backupTranslations,
      ]

      const translation = catalogs
        .map((catalog) => catalog[locale][key])
        .find((value) => value !== undefined)

      return translation ?? key
    },
    [locale],
  )

  const setLocaleAndPersist = useCallback((newLocale: Locale) => {
    localStorage.setItem("locale", newLocale)
    setLocale(newLocale)
  }, [])

  return (
    <LocaleContext.Provider value={{ locale, setLocale: setLocaleAndPersist, t }}>
      {children}
    </LocaleContext.Provider>
  )
}

export function useLocale(): LocaleContextValue {
  const ctx = useContext(LocaleContext)
  if (!ctx) throw new Error("useLocale must be used within LocaleProvider")
  return ctx
}
