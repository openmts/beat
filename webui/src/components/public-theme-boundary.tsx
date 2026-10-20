import type { ReactNode } from "react"
import { PublicThemeProvider } from "@/components/public-theme-provider"
import { publicThemeStyles } from "@/lib/theme-presentation"
import { SandboxedThemeFrame } from "@/components/sandboxed-theme-frame"
import { useLocale } from "@/context/locale"
import { useTheme } from "@/context/theme"
import { useSiteSettings } from "@/context/site-settings"
import { useActiveTheme } from "@/hooks/use-active-theme"
import { usePublicNetworkQuality } from "@/hooks/use-network"
import { buildThemeSnapshot } from "@/lib/theme-snapshot"
import type { ActiveThemeDescriptor } from "@/types/theme"
import type { ThemeSnapshotInput } from "@/types/theme-runtime"

type PageData = Pick<ThemeSnapshotInput, "route" | "nodes" | "groups" | "history" | "customMetricNames" | "historyMetricNames" | "state">
interface BoundaryProps extends PageData { children: ReactNode }
interface RuntimeProps { input: ThemeSnapshotInput; descriptor: ActiveThemeDescriptor; fallback: ReactNode }

export function PublicThemeBoundary({ children, ...data }: BoundaryProps) {
  const descriptor = useActiveTheme()
  const { settings, loading, error } = useSiteSettings()
  const { locale } = useLocale()
  const { theme } = useTheme()
  if (!descriptor || descriptor.built_in || loading || error) return <>{children}</>
  const input: ThemeSnapshotInput = { ...data, site: settings, locale, theme, configuration: descriptor.configuration }
  const props = { input, descriptor, fallback: children }
  return settings.show_network_quality ? <NetworkThemeRuntime {...props} /> : <ThemeRuntime {...props} />
}

function NetworkThemeRuntime(props: RuntimeProps) {
  const network = usePublicNetworkQuality()
  const input: ThemeSnapshotInput = { ...props.input, network: network.data,
    state: { ...props.input.state, network: network.error ? "error" : network.loading ? "loading" : "ready" } }
  return <ThemeRuntime {...props} input={input} />
}

function ThemeRuntime({ descriptor, input, fallback }: RuntimeProps) {
  if (descriptor.runtime === "sandboxed") return <SandboxedThemeFrame descriptor={descriptor} snapshot={() => buildThemeSnapshot(input)} fallback={fallback} />
  if (!descriptor.declarative) return <>{fallback}</>
  try {
    const snapshot = buildThemeSnapshot(input)
    publicThemeStyles(descriptor.declarative, input.theme)
    return <PublicThemeProvider definition={descriptor.declarative} snapshot={snapshot}>{fallback}</PublicThemeProvider>
  } catch { return <>{fallback}</> }
}
