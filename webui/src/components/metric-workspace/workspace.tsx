import { PlusIcon } from "lucide-react"
import { Fragment, useState } from "react"
import { Button } from "@/components/ui/button"
import { Badge } from "@/components/ui/badge"
import { useLocale } from "@/context/locale"
import { useWorkspacePage } from "@/hooks/use-workspace-page"
import { useWorkspaceQueries } from "@/hooks/use-workspace-queries"
import { addWorkspacePanel, appendPastedWorkspacePanel, afterWorkspaceDiscard, importWorkspaceFile, restoreWorkspaceVersion, workspacePanelAction, workspaceToolbarAction } from "@/lib/workspace-page-actions"
import { moveWorkspacePanel, replaceWorkspacePanel } from "@/lib/workspace-editor"
import { maxPanelInstances, repeatedNodes, workspacePanelInstances } from "@/lib/workspace-repeat"
import { annotationFetchWindow } from "@/lib/workspace-annotations"
import { groupRowItems, rowCollapsed, rowPanelCount, toggleRowCollapse } from "@/lib/workspace-row"
import { workspaceAlertLink, workspaceAlertSeed } from "@/lib/workspace-alert-seed"
import { dashboardLinkValues, panelLinkValues } from "@/lib/workspace-links"
import { WorkspaceToolbar } from "./toolbar"
import { WorkspaceDashboardLinks } from "./dashboard-links"
import { WorkspaceVariables } from "./variable-controls"
import { WorkspaceTimeControls } from "./time-controls"
import { WorkspacePanel } from "./panel"
import { WorkspacePanelEditor } from "./panel-editor"
import { WorkspaceRowHeader } from "./row-header"
import { WorkspaceDefinitionEditor } from "./definition-editor"
import { WorkspaceInspector } from "./inspector"
import { WorkspaceConfirm, WorkspaceReauthenticate } from "./confirm"
import { WorkspaceShare } from "./share"
import { WorkspaceAnnotations } from "./annotations"
import { PanelAlertDialog } from "./panel-alerts"
import { WorkspaceSnapshots } from "./snapshots"
import { WorkspaceVersions } from "./versions"
import { WorkspacePanelLibrary } from "./panel-library"
import { WorkspaceDuplicate } from "./duplicate"
import { WorkspacePastePanel } from "./paste-panel"
import { usePanelAlerts, type PanelAlertCollection } from "@/hooks/use-panel-alerts"
import type { DashboardAnnotation } from "@/lib/metric-collaboration-api"
import type { WorkspaceSources } from "@/lib/metric-workspace-api"
import type { WorkspacePage } from "@/hooks/use-workspace-page"
import type { DashboardPanel } from "@/types/metric-workspace"

export function MetricWorkspaceSurface({ sources }: { sources: WorkspaceSources }) {
  const { t } = useLocale()
  const page = useWorkspacePage(sources)
  const definition = page.editor.record.definition
  const queries = useWorkspaceQueries({ dashboardId: page.editor.record.id, definition, nodes: sources.nodes, groups: sources.groups, selections: page.values, range: page.range })
  const context = { page, sources, t }
  const collaboration = usePanelAlerts(page.editor.record.id)
  const [annotations, setAnnotations] = useState<DashboardAnnotation[]>([])
  const instances = workspacePanelInstances(definition, page.values, sources.nodes, sources.groups)
  const groups = groupRowItems(definition.panels, instances)
  // 折叠状态保存在看板定义里（与 Grafana 相同），因此折叠行会一起落库。
  const toggleRow = (row: DashboardPanel) => page.editor.update((value) => ({ ...value, panels: value.panels.map((panel) => panel.id === row.id ? toggleRowCollapse(panel) : panel) }))
  const truncated = definition.panels.some((panel) => repeatedNodes(panel, definition, page.values, sources.nodes, sources.groups) > maxPanelInstances)
  // 平移面板查询更早的窗口，注释拉取窗口要覆盖它们，否则这些面板的时间轴标记会缺数据。
  const annotationWindow = annotationFetchWindow(queries.domain, definition.panels)
  return <div className="space-y-5">
    <WorkspaceToolbar editor={page.editor} owner={page.owner} select={(id) => afterWorkspaceDiscard(page, () => page.editor.select(id))} action={(action) => workspaceToolbarAction(action, context)} importFile={(file) => { void importWorkspaceFile(page, file) }} />
    <WorkspaceDashboardLinks definition={definition} values={dashboardLinkValues({ selections: page.values, range: page.range, domain: queries.domain, dashboardId: page.editor.record.id })} />
    {(page.editor.error || page.error) && <div role="alert" className="flex flex-wrap items-center justify-between gap-3 rounded-md border border-destructive/30 bg-destructive/5 p-3 text-sm text-destructive"><span>{t(page.editor.conflict ? "workspace.remote_changed" : page.editor.error ?? page.error!)}</span>
      <Button variant="outline" size="sm" onClick={() => { page.setError(null); workspaceToolbarAction("reload", context) }}>{t("workspace.reload")}</Button></div>}
    <div className="flex flex-wrap items-end justify-between gap-4 border-y py-4"><WorkspaceVariables variables={definition.variables} values={page.values} nodes={sources.nodes} groups={sources.groups} onChange={(name, values) => page.setValues({ ...page.values, [name]: values })} />
      <WorkspaceTimeControls range={page.range} refreshSeconds={definition.refresh_seconds} onRange={(range) => { page.setRange(range); if (range.mode === "relative") page.editor.update((value) => ({ ...value, time_range_seconds: range.seconds })) }}
        onInterval={(refresh_seconds) => page.editor.update((value) => ({ ...value, refresh_seconds }))} onRefresh={queries.refresh} />
    </div>
    <div className="flex flex-wrap items-center justify-between gap-3"><div className="flex flex-wrap items-center gap-3"><Badge variant="outline">{t(page.range.mode === "relative" ? "workspace.live" : "workspace.historical")}</Badge>
      <span className="font-mono text-[11px] text-muted-foreground">{queries.domain.from.replace("T", " ").replace(/\.\d+Z$/, "")} → {queries.domain.to.replace("T", " ").replace(/\.\d+Z$/, "")} UTC</span></div>
      <Button variant="outline" size="sm" disabled={definition.panels.length >= 24 || page.editor.busy} onClick={() => addWorkspacePanel(context)}><PlusIcon />{t("workspace.add_panel")}</Button></div>
    {definition.panels.length === 0 && <div className="grid min-h-72 place-content-center gap-3 rounded-lg border border-dashed px-6 text-center"><h2 className="text-lg font-medium">{t("workspace.empty")}</h2><p className="max-w-md text-sm text-muted-foreground">{t("workspace.empty_description")}</p><Button className="mx-auto" onClick={() => addWorkspacePanel(context)}><PlusIcon />{t("workspace.add_panel")}</Button></div>}
    <WorkspaceAnnotations dashboardId={page.editor.record.id} from={annotationWindow.from} to={annotationWindow.to}
      editable={page.editor.record.id !== "new"} reload={page.annotationReload} onError={page.setError}
      onLoaded={setAnnotations} panels={definition.panels.map((item) => ({ id: item.id, title: item.title }))}
      onChanged={() => page.setAnnotationReload((value) => value + 1)} />
    <div className="grid min-w-0 grid-cols-1 gap-4 lg:grid-cols-12">{groups.map((group) => {
      const row = group.row
      return <Fragment key={row?.id ?? "workspace-leading-panels"}>
        {row && <WorkspaceRowHeader row={row} count={rowPanelCount(definition, row)} collapsed={rowCollapsed(row)} toggle={() => toggleRow(row)} />}
        {group.items.map((instance) => {
          const seed = workspaceAlertSeed(instance.base, { definition, selections: page.values, nodes: sources.nodes, groups: sources.groups })
          const index = definition.panels.findIndex((value) => value.id === instance.base.id)
          const linkValues = panelLinkValues({ panel: instance.panel, definition, selections: page.values, nodes: sources.nodes, groups: sources.groups, range: page.range, domain: queries.domain })
          return <WorkspacePanel key={instance.key} panel={instance.panel} state={queries.panels[instance.key]} index={index} count={definition.panels.length} busy={page.editor.busy} linkValues={linkValues}
            action={(action) => workspacePanelAction(context, instance.base, action)} inspect={() => page.setInspecting(instance.base.id)} onRange={page.setRange} retry={() => queries.retryPanel(instance.key)}
            alertLink={seed ? workspaceAlertLink(seed) : undefined} alerts={collaboration.forPanel(instance.base.id)} annotations={annotations}
            drop={(id) => { if (!page.editor.busy) page.editor.update((value) => moveWorkspacePanel(value, id, index)) }} />
        })}
      </Fragment>
    })}</div>
    <p className="text-xs text-muted-foreground">{t("workspace.drag_hint")}{truncated ? ` ${t("workspace.repeat_truncated", { count: String(maxPanelInstances) })}` : ""}</p>
    <WorkspaceDialogs page={page} sources={sources} queries={queries} collaboration={collaboration} />
  </div>
}

function WorkspaceDialogs(props: { page: WorkspacePage; sources: WorkspaceSources; queries: ReturnType<typeof useWorkspaceQueries>; collaboration: PanelAlertCollection }) {
  const { page, sources } = props
  const panel = page.editor.record.definition.panels.find((value) => value.id === page.inspecting)
  return <>
    {page.panel && <WorkspacePanelEditor panel={page.panel} definition={page.editor.record.definition} metrics={sources.catalog.metrics} nodes={sources.nodes} close={() => page.setPanel(null)} apply={(panel) => page.editor.update((definition) => replaceWorkspacePanel(definition, panel))} />}
    {page.details && <WorkspaceDefinitionEditor definition={page.editor.record.definition} close={() => page.setDetails(false)} apply={(definition) => page.editor.update(() => definition)} />}
    {panel && <WorkspaceInspector panel={panel} state={props.queries.panels[panel.id] ?? { loading: false }} close={() => page.setInspecting(null)} focusNode={(id) => { window.open(`/admin/metrics?node_id=${encodeURIComponent(id)}`, "_blank", "noopener") }} />}
    {page.link && <WorkspaceShare link={page.link} close={() => page.setLink(null)} />}
    {page.confirmation && <WorkspaceConfirm confirmation={page.confirmation} close={() => page.setConfirmation(null)} />}
    {page.reauth && <WorkspaceReauthenticate action={page.reauth.action} totp={page.totp} close={() => page.setReauth(null)} confirmed={page.setProofUntil} />}
    {page.alertPanel && <PanelAlertDialog dashboardId={page.editor.record.id} panel={page.alertPanel}
      alerts={props.collaboration.forPanel(page.alertPanel.id)} close={() => page.setAlertPanel(null)}
      applied={(values) => props.collaboration.apply(page.alertPanel!.id, values)} onError={page.setError} />}
    {page.libraryPanel && <WorkspacePanelLibrary panel={page.libraryPanel} close={() => page.setLibraryPanel(null)} onError={page.setError} />}
    {page.libraryOpen && <WorkspacePanelLibrary close={() => page.setLibraryOpen(false)} insert={(value) => page.setPanel(value)} onError={page.setError} />}
    {page.pastePanel && <WorkspacePastePanel definition={page.editor.record.definition} metrics={props.sources.catalog.metrics}
      close={() => page.setPastePanel(false)} append={(value) => appendPastedWorkspacePanel(page, value)} />}
    {page.duplicateOpen && <WorkspaceDuplicate title={page.editor.record.definition.title}
      close={() => page.setDuplicateOpen(false)} confirm={(title) => page.editor.duplicate(title)} />}
    {page.snapshots && <WorkspaceSnapshots dashboardId={page.editor.record.id} dashboardName={page.editor.record.definition.title}
      close={() => page.setSnapshots(false)} onError={page.setError} />}
    {page.versions && <WorkspaceVersions dashboardId={page.editor.record.id} currentRevision={page.editor.record.revision}
      refreshKey={page.editor.saved.revision} restore={(revision, summary) => restoreWorkspaceVersion(page, revision, summary)}
      close={() => page.setVersions(false)} onError={page.setError} />}
  </>
}
