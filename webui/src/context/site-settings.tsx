// oxlint-disable react/only-export-components
import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useRef,
  useState,
  type ReactNode,
} from "react"
import { getSiteSettings } from "@/lib/api"
import {
  createPublicTelemetryReporter,
  publicTelemetryConfig,
  type PublicTelemetryEvent,
  type PublicTelemetrySurface,
} from "@/lib/public-telemetry"
import type { SiteSettings } from "@/types"

export const defaultSiteSettings: SiteSettings = {
  site_title: "Beat Monitor",
  site_description: "Server monitoring and operations dashboard.",
  logo_url: "",
  favicon_url: "/favicon.svg",
  default_theme: "system",
  public_address_mode: "hidden",
  show_network_quality: true,
  show_accelerators: true,
  updated_at: "",
}

export type SiteSettingsCache =
  | { status: "loading" }
  | { status: "ready"; settings: SiteSettings }
  | { status: "failed"; error: string }

let settingsCache: SiteSettingsCache = { status: "loading" }
let settingsPending: Promise<void> | null = null

export function cachedSiteSettings(): SiteSettingsCache {
  return settingsCache
}

export function resetSiteSettingsCache(): void {
  settingsCache = { status: "loading" }
  settingsPending = null
}

export async function preloadSiteSettings(): Promise<void> {
  if (settingsCache.status !== "loading") return
  if (!settingsPending) {
    settingsPending = getSiteSettings()
      .then((settings) => { settingsCache = { status: "ready", settings } })
      .catch((reason: unknown) => {
        settingsCache = { status: "failed", error: reason instanceof Error ? reason.message : "Failed to load site settings" }
      })
      .finally(() => { settingsPending = null })
  }
  await settingsPending
}

interface SiteSettingsContextValue {
  settings: SiteSettings
  loading: boolean
  error: string | null
  applySettings: (settings: SiteSettings) => void
  reload: () => Promise<void>
  reportInteraction: (event: PublicTelemetryEvent, surface: PublicTelemetrySurface) => void
}

const fallbackContext: SiteSettingsContextValue = {
  settings: defaultSiteSettings,
  loading: false,
  error: null,
  applySettings: () => undefined,
  reload: async () => undefined,
  reportInteraction: () => undefined,
}

const SiteSettingsContext = createContext<SiteSettingsContextValue>(fallbackContext)

export function SiteSettingsProvider({ children }: { children: ReactNode }) {
  const initial = cachedSiteSettings()
  const [settings, setSettings] = useState(initial.status === "ready" ? initial.settings : defaultSiteSettings)
  const [loading, setLoading] = useState(initial.status === "loading")
  const [error, setError] = useState<string | null>(initial.status === "failed" ? initial.error : null)
  const reporter = useRef(createPublicTelemetryReporter())

  const reload = useCallback(async () => {
    setLoading(true)
    try {
      const next = await getSiteSettings()
      settingsCache = { status: "ready", settings: next }
      setSettings(next)
      setError(null)
    } catch (reason) {
      const message = reason instanceof Error ? reason.message : "Failed to load site settings"
      settingsCache = { status: "failed", error: message }
      setError(message)
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    if (initial.status !== "loading") return
    let cancelled = false
    void preloadSiteSettings().then(() => {
      if (cancelled) return
      const current = cachedSiteSettings()
      setLoading(false)
      if (current.status === "ready") { setSettings(current.settings); setError(null) }
      if (current.status === "failed") setError(current.error)
    })
    return () => { cancelled = true }
  }, [initial.status])

  useEffect(() => applyDocumentMetadata(settings), [settings])

  useEffect(() => {
    reporter.current.configure(publicTelemetryConfig(settings))
  }, [settings])

  const reportInteraction = useCallback((event: PublicTelemetryEvent, surface: PublicTelemetrySurface) => {
    reporter.current.report(event, surface)
  }, [])

  const applySettings = useCallback((next: SiteSettings) => {
    settingsCache = { status: "ready", settings: next }
    setSettings(next)
    setError(null)
  }, [])

  const value = useMemo(() => ({
    settings,
    loading,
    error,
    applySettings,
    reload,
    reportInteraction,
  }), [applySettings, error, loading, reload, reportInteraction, settings])

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
