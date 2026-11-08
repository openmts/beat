import type { ReactNode } from "react"
import { PublicThemeProvider } from "@/components/public-theme-provider"
import { publicThemeStyles } from "@/lib/theme-presentation"
import { SandboxedThemeFrame } from "@/components/sandboxed-theme-frame"
import { useLocale } from "@/context/locale"
import { useTheme } from "@/context/theme"
import { useSiteSettings } from "@/context/site-settings"
import { useActiveThemeState } from "@/hooks/use-active-theme"
import { usePublicNetworkQuality } from "@/hooks/use-network"
import { buildThemeSnapshot } from "@/lib/theme-snapshot"
import type { ActiveThemeDescriptor } from "@/types/theme"
import type { ThemeSnapshot, ThemeSnapshotInput } from "@/types/theme-runtime"
import type { WorkspaceRange } from "@/types/metric-workspace"

type PageData = Pick<ThemeSnapshotInput, "route" | "nodes" | "groups" | "history" | "networkHistory" | "customMetricNames" | "historyMetricNames" | "state">
/** 调用方已经取得网络质量时直接复用，避免整页主题边界再发起一份相同轮询。 */
interface BoundaryNetwork { data: ThemeSnapshotInput["network"]; state: ThemeSnapshot["state"]["network"] }
interface BoundaryProps extends PageData { children: ReactNode; surface?: "page" | "embedded"; network?: BoundaryNetwork; range?: WorkspaceRange; onRangeChange?: (range: WorkspaceRange) => void }
interface RuntimeProps { input: ThemeSnapshotInput; descriptor: ActiveThemeDescriptor; fallback: ReactNode; surface?: "page" | "embedded"; onRangeChange?: (range: WorkspaceRange) => void }

export function PublicThemeBoundary({ children, surface = "page", network, range, onRangeChange, ...data }: BoundaryProps) {
  const active = useActiveThemeState()
  const { settings, loading, error } = useSiteSettings()
  const { locale } = useLocale()
  const { theme } = useTheme()
  if (active.status === "loading" || loading) return <PublicThemeBoot />
  const descriptor = active.descriptor
  if (active.status === "failed" || !descriptor || descriptor.built_in || error) return <>{children}</>
  const input: ThemeSnapshotInput = {
    ...data, site: settings, locale, theme, configuration: descriptor.configuration, ...(range ? { range } : {}),
    ...(network ? { network: network.data, state: { ...data.state, network: network.state } } : {}),
  }
  const props = { input, descriptor, fallback: children, surface, onRangeChange }
  return settings.show_network_quality && !network ? <NetworkThemeRuntime {...props} /> : <ThemeRuntime {...props} />
}

function NetworkThemeRuntime(props: RuntimeProps) {
  const network = usePublicNetworkQuality()
  const input: ThemeSnapshotInput = { ...props.input, network: network.data,
    state: { ...props.input.state, network: network.error ? "error" : network.loading ? "loading" : "ready" } }
  return <ThemeRuntime {...props} input={input} />
}

function ThemeRuntime({ descriptor, input, fallback, surface, onRangeChange }: RuntimeProps) {
  if (descriptor.runtime === "sandboxed") return <SandboxedThemeFrame descriptor={descriptor} snapshot={() => buildThemeSnapshot(input)} fallback={fallback}
    onSetRange={input.route.kind === "node" ? onRangeChange : undefined} />
  if (!descriptor.declarative) return <>{fallback}</>
  try {
    const snapshot = buildThemeSnapshot(input)
    publicThemeStyles(descriptor.declarative, input.theme)
    return <PublicThemeProvider definition={descriptor.declarative} snapshot={snapshot} surface={surface}>{fallback}</PublicThemeProvider>
  } catch { return <>{fallback}</> }
}

function PublicThemeBoot() {
  const { t } = useLocale()
  return <div data-public-theme-boot role="status" aria-label={t("app.loading_page")} className="min-h-svh w-full bg-background" />
}
