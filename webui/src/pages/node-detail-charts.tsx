import { Fragment, useState } from "react"
import { Alert, AlertDescription } from "@/components/ui/alert"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Skeleton } from "@/components/ui/skeleton"
import { WorkspaceTimeControls } from "@/components/metric-workspace/time-controls"
import { WorkspaceVariables } from "@/components/metric-workspace/variable-controls"
import { WorkspacePanel } from "@/components/metric-workspace/panel"
import { WorkspaceInspector } from "@/components/metric-workspace/inspector"
import { WorkspaceRowHeader } from "@/components/metric-workspace/row-header"
import { WorkspaceDashboardLinks } from "@/components/metric-workspace/dashboard-links"
import { maxPanelInstances, repeatedNodes, workspacePanelInstances } from "@/lib/workspace-repeat"
import { groupRowItems, rowCollapsed, rowPanelCount } from "@/lib/workspace-row"
import { dashboardLinkValues, linksHiddenInReadOnly, panelLinkValues } from "@/lib/workspace-links"
import { usePublicTheme } from "@/lib/theme-presentation"
import { useLocale } from "@/context/locale"
import type { PublicMetricWorkspace } from "@/hooks/use-public-metric-workspace"

export function NodeCharts({ workspace }: { workspace: PublicMetricWorkspace }) {
  const { t } = useLocale()
  const spotlight = usePublicTheme()?.definition.detail === "spotlight"
  const [inspecting, setInspecting] = useState<string | null>(null)
  const source = workspace.source
  const inspected = workspace.definition.panels.find((panel) => panel.id === inspecting)
  const nodes = source.data?.nodes ?? []
  // 公共节点页与后台工作区一致：重复面板按变量逐节点展开。
  const instances = workspacePanelInstances(workspace.definition, workspace.values, nodes)
  const groups = groupRowItems(workspace.definition.panels, instances)
  const repeatNodes = (panel: (typeof workspace.definition.panels)[number]) => repeatedNodes(panel, workspace.definition, workspace.values, nodes)
  if (source.loading) return <div className="space-y-3"><p role="status" className="text-sm text-muted-foreground">{t("app.loading")}</p><Skeleton className="h-64 w-full" /></div>
  if (source.error) return <Alert variant="destructive"><AlertDescription>{t("node.metrics_failed")}<Button variant="outline" size="sm" onClick={source.reload}>{t("app.retry")}</Button></AlertDescription></Alert>
  if (!source.data || !workspace.node.data) return null
  return <div data-public-workspace className="space-y-4">
    <div className="space-y-1"><h2 className="font-semibold">{workspace.definition.title}</h2>{workspace.definition.description && <p className="text-sm text-muted-foreground">{workspace.definition.description}</p>}</div>
    <WorkspaceDashboardLinks readOnly definition={workspace.definition} values={dashboardLinkValues({ selections: workspace.values, range: workspace.range, domain: workspace.queries.domain, dashboardId: source.data.dashboard.id })} />
    <div className="flex flex-wrap items-end justify-between gap-3">
      <WorkspaceVariables variables={workspace.definition.variables} values={workspace.values} nodes={source.data.nodes} groups={source.data.groups} onChange={workspace.setVariable} />
      <WorkspaceTimeControls range={workspace.range} refreshSeconds={workspace.refreshSeconds} onRange={workspace.setRange} onInterval={workspace.setRefreshSeconds} onRefresh={workspace.refresh} />
    </div>
    <div className="flex flex-wrap items-center gap-3"><Badge variant="outline">{t(workspace.range.mode === "absolute" ? "workspace.historical" : "workspace.live")}</Badge>
      <span className="font-mono text-[11px] text-muted-foreground">{workspace.queries.domain.from.replace("T", " ").replace(/\.\d+Z$/, "")} → {workspace.queries.domain.to.replace("T", " ").replace(/\.\d+Z$/, "")} UTC</span>
    </div>
    {!workspace.definition.panels.length && <p className="rounded-lg border border-dashed p-8 text-center text-muted-foreground">{t("workspace.no_data")}</p>}
    <div className={`grid min-w-0 grid-cols-1 gap-4 lg:grid-cols-12 ${spotlight ? "lg:[&>*:first-child]:col-span-12" : ""}`}>
      {groups.map((group) => {
        const row = group.row
        return <Fragment key={row?.id ?? "workspace-leading-panels"}>
          {row && <WorkspaceRowHeader row={row} count={rowPanelCount(workspace.definition, row)} collapsed={rowCollapsed(row)} toggle={() => workspace.toggleRow(row.id, !rowCollapsed(row))} />}
          {group.items.map((instance) => <WorkspacePanel key={instance.key} readOnly hideLinks={linksHiddenInReadOnly(workspace.definition)} panel={instance.panel} state={workspace.queries.panels[instance.key]}
            linkValues={panelLinkValues({ panel: instance.panel, definition: workspace.definition, selections: workspace.values, nodes, groups: source.data?.groups, range: workspace.range, domain: workspace.queries.domain })}
            inspect={() => setInspecting(instance.base.id)} onRange={workspace.setRange} retry={() => workspace.queries.retryPanel(instance.key)} />)}
        </Fragment>
      })}
    </div>
    {workspace.definition.panels.some((panel) => repeatNodes(panel) > maxPanelInstances) && <p className="text-xs text-muted-foreground">{t("workspace.repeat_truncated", { count: maxPanelInstances })}</p>}
    {inspected && <WorkspaceInspector publicLinks panel={inspected} state={workspace.queries.panels[inspected.id] ?? { loading: false }} close={() => setInspecting(null)} />}
  </div>
}
