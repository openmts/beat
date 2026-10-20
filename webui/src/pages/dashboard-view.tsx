import { ServerOffIcon } from "lucide-react"
import { Header } from "@/components/header"
import { PublicFooter } from "@/components/public-footer"
import { GroupTabs } from "@/components/group-tabs"
import { ThemedNodeCollection } from "@/components/themed-node-collection"
import { usePublicTheme } from "@/lib/theme-presentation"
import { NodeCard } from "@/components/node-card"
import { NetworkQualityBand } from "@/components/network-quality-band"
import { useLocale } from "@/context/locale"
import { useSiteSettings } from "@/context/site-settings"
import type { Node, Group } from "@/types"

interface DashboardViewProps {
  nodes: Node[]
  groups: Group[]
  nodesLoading: boolean
  groupsLoading: boolean
  nodesError: string | null
  groupsError: string | null
  selectedGroup: string
  onGroupChange: (group: string) => void
}
export function DashboardView(props: DashboardViewProps) {
  const { settings } = useSiteSettings()
  return <div className="flex min-h-svh flex-col"><Header /><main className="flex-1">
    <div className="container-page safe-pb py-4 sm:py-6">
      <div className="mb-4 -mx-1 px-1">{props.groupsLoading ? <div className="h-8" /> : <GroupTabs groups={props.groups} value={props.selectedGroup} onChange={props.onGroupChange} />}</div>
      {[props.groupsError, props.nodesError].filter((error): error is string => error != null).map((error, index) =>
        <div key={index} className="mb-4 rounded-lg border border-destructive/50 bg-destructive/10 p-3 text-sm text-destructive">{error}</div>)}
      <DashboardNodes {...props} />
      {settings.show_network_quality && <NetworkQualityBand />}
    </div>
  </main><PublicFooter /></div>
}
function DashboardNodes({ nodes, nodesLoading, selectedGroup }: DashboardViewProps) {
  const { t } = useLocale()
  const theme = usePublicTheme()
  if (nodesLoading) return <div className="grid grid-cols-1 gap-4 md:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4">{Array.from({ length: 8 }).map((_, index) => <NodeCard key={index} loading />)}</div>
  if (theme) return <ThemedNodeCollection selectedGroup={selectedGroup} />
  const visible = nodes.filter((node) => !selectedGroup || node.group_id === selectedGroup)
  if (!visible.length) return <div className="flex min-h-64 flex-col items-center justify-center gap-3 rounded-xl border border-dashed text-center">
    <ServerOffIcon className="size-8 text-muted-foreground" aria-hidden /><p className="text-sm text-muted-foreground">{selectedGroup ? t("node.empty_group") : t("node.empty")}</p>
  </div>
  return <div className="grid grid-cols-1 gap-4 md:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4">{visible.map((node) => <NodeCard key={node.id} node={node} />)}</div>
}
