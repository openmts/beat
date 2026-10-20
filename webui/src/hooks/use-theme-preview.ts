import { useGroups, useNodeMetrics, useNodes } from "@/hooks/use-api"
import { useLocale } from "@/context/locale"
import { useSiteSettings } from "@/context/site-settings"
import { buildThemeSnapshot } from "@/lib/theme-snapshot"
import type { ActiveThemeDescriptor } from "@/types/theme"
import type { ThemeSnapshot } from "@/types/theme-runtime"

export function useThemePreview(descriptor: ActiveThemeDescriptor, mode: "light" | "dark", selected: string | undefined) {
  const nodes = useNodes()
  const groups = useGroups()
  const { settings, loading, error } = useSiteSettings()
  const { locale } = useLocale()
  const metrics = useNodeMetrics(selected, ["cpu", "memory", "disk"], 1)
  const failure = error || nodes.error || groups.error
  let snapshot: ThemeSnapshot | null = null
  if (!loading && !nodes.loading && !groups.loading && !failure) {
    try {
      snapshot = buildThemeSnapshot({ site: settings, nodes: nodes.data ?? [], groups: groups.data ?? [], locale, theme: mode,
        route: selected ? { kind: "node", node_id: selected } : { kind: "dashboard" }, configuration: descriptor.configuration,
        history: selected ? { from: metrics.from, to: metrics.to, series: metrics.data } : undefined,
        state: { metrics: metrics.error ? "error" : metrics.loading ? "loading" : "ready", network: "loading" } })
    } catch { return { snapshot, loading: false, error: "invalid_data" } }
  }
  return { snapshot, loading: loading || nodes.loading || groups.loading, error: failure }
}
