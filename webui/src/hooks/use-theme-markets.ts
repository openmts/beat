import { useCallback, useEffect, useState } from "react"
import { useLocale } from "@/context/locale"
import { listAdminThemes, listThemeMarkets, refreshThemeCatalog } from "@/lib/theme-api"
import type { ThemeCatalog, ThemeMarketSource, ThemePackage } from "@/types/theme"

export function useThemeMarkets() {
  const { t } = useLocale()
  const [sources, setSources] = useState<ThemeMarketSource[] | null>(null)
  const [installed, setInstalled] = useState<ThemePackage[]>([])
  const [catalogs, setCatalogs] = useState<Record<string, ThemeCatalog>>({})
  const [conflicts, setConflicts] = useState<Record<string, string>>({})
  const [error, setError] = useState("")
  const load = useCallback(async () => {
    try {
      const [next, packages] = await Promise.all([listThemeMarkets(), listAdminThemes()])
      setSources(next); setInstalled(packages); setError("")
    } catch (cause) { setError(cause instanceof Error ? cause.message : t("app.error")) }
  }, [t])
  useEffect(() => { void load() }, [load])
  const clear = (id: string) => {
    setCatalogs((current) => { const next = { ...current }; delete next[id]; return next })
    setConflicts((current) => { const next = { ...current }; delete next[id]; return next })
  }
  const refresh = async (id: string) => {
    try {
      const catalog = await refreshThemeCatalog(id)
      setCatalogs((current) => ({ ...current, [id]: catalog }))
      setConflicts((current) => { const next = { ...current }; delete next[id]; return next })
    } catch (cause) { clear(id); throw cause }
    finally { await load() }
  }
  const conflict = (id: string, selection: string) => setConflicts((current) => ({ ...current, [id]: selection }))
  return { sources, installed, catalogs, conflicts, error, load, clear, refresh, conflict }
}
