import { useState } from "react"
import { useLocale } from "@/context/locale"
import { useThemeOperation } from "@/hooks/use-theme-operation"
import type { useThemeMarkets } from "@/hooks/use-theme-markets"
import { APIError } from "@/lib/api-error"
import { createThemeMarket, deleteThemeMarket, installMarketTheme, updateThemeMarket } from "@/lib/theme-api"
import type { ThemeCatalogEntry, ThemeMarketInput, ThemeMarketSource } from "@/types/theme"

export function useThemeMarketActions(data: ReturnType<typeof useThemeMarkets>, onInstalled: () => void) {
  const { t } = useLocale()
  const flow = useThemeOperation()
  const [editor, setEditor] = useState<ThemeMarketSource | "new" | null>(null)
  const [notice, setNotice] = useState("")
  const perform = (key: string, run: () => Promise<unknown>, confirm = false) => {
    setNotice("")
    flow.request({ label: t(`theme.${key}`), description: t(`theme.confirm_${key}`), run }, confirm)
  }
  const save = (input: ThemeMarketInput) => perform(editor === "new" ? "add_source" : "edit_source", async () => {
    if (!editor) return
    if (editor === "new") await createThemeMarket(input)
    else { await updateThemeMarket(editor.id, input); data.clear(editor.id) }
    setEditor(null); await data.load()
  })
  const sourceAction = (kind: "refresh" | "edit" | "toggle" | "delete", source: ThemeMarketSource) => {
    if (kind === "edit") { setEditor(source); return }
    if (kind === "refresh") { perform("refresh", () => data.refresh(source.id)); return }
    perform(kind === "delete" ? "delete_source" : source.enabled ? "disable_source" : "enable_source", async () => {
      if (kind === "delete") await deleteThemeMarket(source.id)
      else await updateThemeMarket(source.id, { name: source.name, catalog_url: source.catalog_url,
        public_key: "", key_fingerprint: "", enabled: !source.enabled, sort_order: source.sort_order })
      data.clear(source.id); await data.load()
    }, true)
  }
  const install = (source: ThemeMarketSource, entry: ThemeCatalogEntry) => perform("install", async () => {
    try { await installMarketTheme(source.id, entry) }
    catch (cause) {
      if (cause instanceof APIError && cause.status === 409) { data.conflict(source.id, `${entry.name} ${entry.version}`); return }
      throw cause
    }
    await data.load(); onInstalled()
    setNotice(t("theme.install_ready", { name: entry.name, version: entry.version }))
  })
  return { flow, editor, setEditor, notice, save, sourceAction, install }
}
