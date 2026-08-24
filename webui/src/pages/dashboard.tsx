import { useState, useEffect } from "react"
import { ServerOffIcon } from "lucide-react"
import { useLiveNodes, useGroups } from "@/hooks/use-api"
import { Header } from "@/components/header"
import { PublicFooter } from "@/components/public-footer"
import { GroupTabs } from "@/components/group-tabs"
import { NodeCard } from "@/components/node-card"
import { NetworkQualityBand } from "@/components/network-quality-band"
import { SandboxedThemeFrame } from "@/components/sandboxed-theme-frame"
import { useLocale } from "@/context/locale"
import { useSiteSettings } from "@/context/site-settings"
import { getActiveTheme } from "@/lib/theme-api"
import type { ActiveThemeDescriptor } from "@/types/theme"

function Dashboard() {
  const { t } = useLocale()
  const { settings } = useSiteSettings()
  const [selectedGroup, setSelectedGroup] = useState("")
  const [activeTheme, setActiveTheme] = useState<ActiveThemeDescriptor | null>(null)
  const {
    data: nodes,
    loading: nodesLoading,
    error: nodesError,
    refresh: refreshNodes,
  } = useLiveNodes(selectedGroup || undefined)
  const {
    data: groups,
    loading: groupsLoading,
    error: groupsError,
  } = useGroups()

  useEffect(() => {
    let cancelled = false
    getActiveTheme()
      .then((descriptor) => {
        if (!cancelled) setActiveTheme(descriptor)
      })
      .catch(() => {
        if (!cancelled) setActiveTheme(null)
      })
    return () => {
      cancelled = true
    }
  }, [])

  useEffect(() => {
    const interval = setInterval(() => {
      void refreshNodes({ silent: true })
    }, 30000)
    return () => clearInterval(interval)
  }, [refreshNodes])

  const skeletonCount = 8

  const builtInDashboard = (
    <div className="flex min-h-svh flex-col">
      <Header />
      <main className="flex-1">
        <div className="container-page safe-pb py-4 sm:py-6">
          <div className="mb-4 -mx-1 px-1">
            {groupsLoading ? (
              <div className="h-8" />
            ) : (
              <GroupTabs
                groups={groups ?? []}
                value={selectedGroup}
                onChange={setSelectedGroup}
              />
            )}
          </div>
          {groupsError != null && (
            <div className="mb-4 rounded-lg border border-destructive/50 bg-destructive/10 p-3 text-sm text-destructive">
              {groupsError}
            </div>
          )}
          {nodesError != null && (
            <div className="mb-4 rounded-lg border border-destructive/50 bg-destructive/10 p-3 text-sm text-destructive">
              {nodesError}
            </div>
          )}
          {nodesLoading ? (
            <div className="grid grid-cols-1 gap-4 md:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4">
              {Array.from({ length: skeletonCount }).map((_, i) => (
                <NodeCard key={i} loading />
              ))}
            </div>
          ) : (nodes ?? []).length === 0 ? (
            <div className="flex min-h-64 flex-col items-center justify-center gap-3 rounded-xl border border-dashed text-center">
              <ServerOffIcon className="size-8 text-muted-foreground" aria-hidden />
              <p className="text-sm text-muted-foreground">
                {selectedGroup ? t("node.empty_group") : t("node.empty")}
              </p>
            </div>
          ) : (
            <div className="grid grid-cols-1 gap-4 md:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4">
              {(nodes ?? []).map((node) => (
                <NodeCard key={node.id} node={node} />
              ))}
            </div>
          )}
          {settings.show_network_quality && <NetworkQualityBand />}
        </div>
      </main>
      <PublicFooter />
    </div>
  )

  const sandboxActive = activeTheme != null &&
    activeTheme.runtime === "sandboxed" &&
    activeTheme.sandbox_entry != null &&
    activeTheme.archive_digest != null

  if (sandboxActive) {
    return (
      <SandboxedThemeFrame
        descriptor={activeTheme}
        snapshot={() => ({
          timestamp: new Date().toISOString(),
          nodes: (nodes ?? []).map((node) => ({
            id: node.id,
            name: node.name,
            status: node.status,
          })),
        })}
        fallback={builtInDashboard}
      />
    )
  }

  return builtInDashboard
}

export default Dashboard
