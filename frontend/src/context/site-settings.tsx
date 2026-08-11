// oxlint-disable react/only-export-components
import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useState,
  type ReactNode,
} from "react"
import { getSiteSettings } from "@/lib/api"
import type { SiteSettings } from "@/types"

export const defaultSiteSettings: SiteSettings = {
  site_title: "Beat Monitor",
  site_description: "Server monitoring and operations dashboard.",
  logo_url: "",
  favicon_url: "/favicon.svg",
  default_theme: "system",
  show_ip_addresses: true,
  show_network_quality: true,
  updated_at: "",
}

interface SiteSettingsContextValue {
  settings: SiteSettings
  loading: boolean
  error: string | null
  applySettings: (settings: SiteSettings) => void
  reload: () => Promise<void>
}

const fallbackContext: SiteSettingsContextValue = {
  settings: defaultSiteSettings,
  loading: false,
  error: null,
  applySettings: () => undefined,
  reload: async () => undefined,
}

const SiteSettingsContext = createContext<SiteSettingsContextValue>(fallbackContext)

export function SiteSettingsProvider({ children }: { children: ReactNode }) {
  const [settings, setSettings] = useState(defaultSiteSettings)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  const reload = useCallback(async () => {
    setLoading(true)
    try {
      setSettings(await getSiteSettings())
      setError(null)
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : "Failed to load site settings")
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    void reload()
  }, [reload])

  useEffect(() => applyDocumentMetadata(settings), [settings])

  const value = useMemo(() => ({
    settings,
    loading,
    error,
    applySettings: setSettings,
    reload,
  }), [error, loading, reload, settings])

  return <SiteSettingsContext.Provider value={value}>{children}</SiteSettingsContext.Provider>
}

function applyDocumentMetadata(settings: SiteSettings) {
  document.title = settings.site_title
  const description = ensureHeadElement<HTMLMetaElement>("meta[name='description']", "meta")
  description.name = "description"
  description.content = settings.site_description
  const favicon = ensureHeadElement<HTMLLinkElement>("link[rel~='icon']", "link")
  favicon.rel = "icon"
  favicon.href = settings.favicon_url || "/favicon.svg"
}

function ensureHeadElement<T extends HTMLElement>(selector: string, tagName: string): T {
  const existing = document.head.querySelector<T>(selector)
  if (existing) return existing
  const element = document.createElement(tagName) as T
  document.head.appendChild(element)
  return element
}

export function useSiteSettings() {
  return useContext(SiteSettingsContext)
}
