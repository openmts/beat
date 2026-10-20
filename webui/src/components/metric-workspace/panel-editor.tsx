import { useState } from "react"
import { Button } from "@/components/ui/button"
import { Checkbox } from "@/components/ui/checkbox"
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Textarea } from "@/components/ui/textarea"
import { useLocale } from "@/context/locale"
import { dashboardQueryPoints } from "@/lib/workspace-query"
import { switchWorkspacePanelType, validateWorkspacePanel } from "@/lib/workspace-panel-editor"
import { isRowPanel, isTextPanel } from "@/lib/workspace-row"
import { textPanelError } from "@/lib/workspace-text"
import { panelDescriptionError } from "@/lib/workspace-panel-description"
import { WorkspaceSelect } from "./select"
import { WorkspacePanelLinks, WorkspacePanelOptions } from "./panel-options"
import { WorkspaceSeriesFields } from "./series-fields"
import type { Node } from "@/types"
import type { DashboardDefinition, DashboardPanel, MetricDefinition, PanelSize, PanelType } from "@/types/metric-workspace"

/** 下拉顺序与 Grafana 一致：先常用可视化，最后是不查询数据的文本面板与行面板。 */
const panelTypeChoices: PanelType[] = ["timeseries", "barchart", "stat", "gauge", "bargauge", "piechart", "heatmap", "histogram", "geomap", "statetimeline", "table", "text", "row"]

export function WorkspacePanelEditor(props: { panel: DashboardPanel; definition: DashboardDefinition; metrics: MetricDefinition[]; nodes: Node[]; close: () => void; apply: (panel: DashboardPanel) => void }) {
  const { t } = useLocale()
  const [panel, setPanel] = useState(() => structuredClone(props.panel))
  const [error, setError] = useState<string | null>(null)
  const metric = props.metrics.find((value) => value.key === panel.series[0]?.metric)
  const row = isRowPanel(panel), text = isTextPanel(panel)
  const textError = text ? textPanelError(panel) : null
  const descriptionError = panelDescriptionError(panel)
  function apply() {
    const invalid = validateWorkspacePanel(panel, props.metrics, props.definition.variables)
    if (invalid) { setError(invalid); return }
    props.apply(panel)
    props.close()
  }
  return <Dialog open onOpenChange={(open) => { if (!open) props.close() }}>
    <DialogContent className="max-h-[90svh] overflow-y-auto sm:max-w-4xl"><DialogHeader><DialogTitle>{t("workspace.edit_panel")}</DialogTitle><DialogDescription>{t(text ? "workspace.text_description" : row ? "workspace.row_description" : "workspace.empty_description")}</DialogDescription></DialogHeader>
      <div className="grid gap-4 sm:grid-cols-[2fr_1fr_1fr]"><div className="grid gap-2"><Label htmlFor="panel-title">{t("workspace.panel_title")}</Label><Input id="panel-title" value={panel.title} maxLength={120} onChange={(event) => setPanel({ ...panel, title: event.target.value })} /></div>
        <div className="grid gap-2"><Label>{t("workspace.panel_type")}</Label><WorkspaceSelect label={t("workspace.panel_type")} value={panel.type} options={panelTypeChoices.map((value) => ({ value, label: t(`workspace.${value}`) }))} onChange={(type) => setPanel(switchWorkspacePanelType(panel, type as PanelType, props.metrics, props.definition))} /></div>
        <div className="grid gap-2"><Label>{t("workspace.resize")}</Label><WorkspaceSelect label={t("workspace.resize")} value={panel.size} options={["small", "medium", "large"].map((value) => ({ value, label: t(`workspace.${value}`) }))} onChange={(size) => setPanel({ ...panel, size: size as PanelSize })} /></div>
      </div>
      <div className="grid gap-2"><Label htmlFor="panel-description">{t("workspace.panel_description")}</Label>
        <Textarea id="panel-description" rows={2} value={panel.description ?? ""}
          onChange={(event) => setPanel({ ...panel, description: event.target.value === "" ? undefined : event.target.value })} />
        <p className="text-xs text-muted-foreground">{t("workspace.panel_description_hint")}</p>
        {descriptionError && <p role="alert" className="text-xs text-destructive">{t(descriptionError)}</p>}</div>
      {row && <label className="flex items-center gap-2 text-sm"><Checkbox checked={panel.row_collapsed === true} onCheckedChange={(checked) => setPanel({ ...panel, row_collapsed: checked === true ? true : undefined })} />{t("workspace.row_collapsed")}</label>}
      {text && <div className="grid gap-2"><Label htmlFor="panel-text">{t("workspace.text_content")}</Label>
        <Textarea id="panel-text" rows={8} value={panel.text ?? ""} onChange={(event) => setPanel({ ...panel, text: event.target.value })} />
        <p className="text-xs text-muted-foreground">{t("workspace.text_hint")}</p>
        <p className="text-xs text-muted-foreground">{t("workspace.text_table_hint")}</p>
        {textError && <p role="alert" className="text-xs text-destructive">{t(textError)}</p>}</div>}
      {text && <WorkspacePanelLinks panel={panel} update={setPanel} />}
      {!row && !text && <div className="grid gap-2 sm:max-w-sm"><Label>{t("workspace.repeat")}</Label>
        <WorkspaceSelect label={t("workspace.repeat")} value={panel.repeat ?? ""} options={[{ value: "", label: t("workspace.repeat_none") }, ...props.definition.variables.map((variable) => ({ value: variable.name, label: variable.label || variable.name }))]}
          onChange={(repeat) => setPanel({ ...panel, ...(repeat ? { repeat } : { repeat: undefined }) })} />
        <p className="text-xs text-muted-foreground">{t("workspace.repeat_hint")}</p>
      </div>}
      {!row && !text && <WorkspaceSeriesFields panel={panel} definition={props.definition} metrics={props.metrics} nodes={props.nodes} update={setPanel} />}
      {!row && !text && <WorkspacePanelOptions panel={panel} unit={metric ? metric.unit + (panel.series[0]?.aggregation === "rate" ? "_per_second" : "") : undefined}
        defaultPoints={dashboardQueryPoints(props.definition)} update={setPanel} />}
      {error && <p role="alert" className="text-sm text-destructive">{t(error)}</p>}
      <DialogFooter><Button variant="outline" onClick={props.close}>{t("workspace.cancel")}</Button><Button onClick={apply}>{t("workspace.apply")}</Button></DialogFooter>
    </DialogContent>
  </Dialog>
}
