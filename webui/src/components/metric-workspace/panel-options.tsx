import { WorkspaceAxisOptions, WorkspaceQueryOptions, WorkspaceLegendOptions } from "./panel-axis-options"
import { WorkspaceOverrides, WorkspaceSparklineOptions } from "./panel-override-options"
import { WorkspaceValueMappings, WorkspaceTransformations } from "./panel-transformation-options"
import { XIcon } from "lucide-react"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { useLocale } from "@/context/locale"
import { panelColors } from "@/lib/workspace-chart"
import { defaultNoValue, maximumPanelNoValueBytes, noValuePanelTypes } from "@/lib/workspace-overrides"
import { emptyPanelLink, maximumPanelLinkTitleLength, maximumPanelLinks, panelLinkError, panelLinks, panelLinkPlaceholders } from "@/lib/workspace-links"
import { panelThresholdDisplay, thresholdDisplayError, thresholdDisplayModes } from "@/lib/workspace-threshold-display"
import { calculationError, panelCalculation, panelCalculations, panelCalculationTypes } from "@/lib/workspace-calculation"
import { Checkbox } from "@/components/ui/checkbox"
import { softAxisPanelTypes } from "@/lib/workspace-axis"
import { WorkspaceSelect } from "./select"
import { WorkspaceLinkIconSelect } from "./link-icon"
import { WorkspaceBarChartOptions, WorkspaceBarGaugeOptions, WorkspaceCellDisplayOptions, WorkspaceGaugeOptions, WorkspaceGeoMapOptions, WorkspaceHeatmapOptions, WorkspaceHistogramOptions, WorkspaceLineStyle, WorkspacePieChartOptions, WorkspaceStateTimelineOptions, WorkspaceStatOptions } from "./panel-visual-options"
import type { DashboardPanel, DashboardPanelLink, PanelCalculation, PanelColor, ThresholdDisplayMode } from "@/types/metric-workspace"

export function WorkspacePanelOptions(props: { panel: DashboardPanel; unit?: string; defaultPoints?: number; update: (panel: DashboardPanel) => void }) {
  const { t } = useLocale()
  return <div className="space-y-5 border-t pt-5">
    <div className="grid gap-4 sm:grid-cols-4"><div className="grid gap-2"><Label>{t("workspace.unit")}</Label><WorkspaceSelect label={t("workspace.unit")} value={props.panel.unit || "auto"}
      options={[{ value: "auto", label: t("workspace.unit_auto") }, ...(props.unit ? [{ value: props.unit, label: props.unit }] : [])]} onChange={(unit) => props.update({ ...props.panel, unit })} /></div>
      {(["decimals", "min", "max"] as const).map((key) => <div key={key} className="grid gap-2"><Label htmlFor={`panel-${key}`}>{t(`workspace.${key}`)}</Label><Input id={`panel-${key}`} type="number" placeholder={t("workspace.auto")}
        value={props.panel[key] ?? ""} step={key === "decimals" ? "1" : "any"} onChange={(event) => props.update({ ...props.panel, [key]: event.target.value === "" ? undefined : Number(event.target.value) })} /></div>)}
      {/* Grafana 的 Standard options → No value：只有会为缺失样本渲染一行文本的面板显示这一项。 */}
      {noValuePanelTypes.includes(props.panel.type) && <div className="grid gap-2"><Label htmlFor="panel-no_value">{t("workspace.no_value")}</Label>
        <Input id="panel-no_value" maxLength={maximumPanelNoValueBytes} placeholder={defaultNoValue} value={props.panel.no_value ?? ""}
          onChange={(event) => props.update({ ...props.panel, no_value: event.target.value || undefined })} /></div>}
    </div>
    {softAxisPanelTypes.includes(props.panel.type) && <WorkspaceAxisOptions {...props} />}
    {(props.panel.type === "timeseries" || props.panel.type === "barchart") && <WorkspaceLineStyle {...props} />}
    {props.panel.type === "barchart" && <WorkspaceBarChartOptions {...props} />}
    {props.panel.type === "bargauge" && <WorkspaceBarGaugeOptions {...props} />}
    {props.panel.type === "piechart" && <WorkspacePieChartOptions {...props} />}
    {props.panel.type === "heatmap" && <WorkspaceHeatmapOptions {...props} />}
    {props.panel.type === "histogram" && <WorkspaceHistogramOptions {...props} />}
    {props.panel.type === "geomap" && <WorkspaceGeoMapOptions {...props} />}
    {props.panel.type === "statetimeline" && <WorkspaceStateTimelineOptions {...props} />}
    {props.panel.type === "table" && <WorkspaceCellDisplayOptions {...props} />}
    <WorkspaceLegendOptions {...props} />
    <WorkspaceQueryOptions {...props} />
    {panelCalculationTypes.includes(props.panel.type) && <WorkspaceCalculation {...props} />}
    {props.panel.type === "stat" && <WorkspaceStatOptions {...props} />}
    {props.panel.type === "gauge" && <WorkspaceGaugeOptions {...props} />}
    {props.panel.type === "stat" && <WorkspaceSparklineOptions {...props} />}
    <WorkspaceThresholds {...props} />
    <WorkspaceTransformations {...props} />
    <WorkspaceOverrides {...props} />
    <WorkspacePanelLinks {...props} />
    {!["timeseries", "barchart", "heatmap", "histogram", "geomap"].includes(props.panel.type) && <WorkspaceValueMappings {...props} />}
  </div>
}

function WorkspaceCalculation(props: { panel: DashboardPanel; update: (panel: DashboardPanel) => void }) {
  const { t } = useLocale()
  const error = calculationError(props.panel)
  const panel = props.panel
  return <fieldset className="space-y-3 border-t pt-5">
    <legend className="text-sm font-medium">{t("workspace.calculation")}</legend>
    <div className="grid gap-2 sm:max-w-xs">
      <WorkspaceSelect label={t("workspace.calculation")} value={panelCalculation(panel)}
        options={panelCalculations.map((value) => ({ value, label: t(`workspace.calculation_${value}`) }))}
        onChange={(value) => props.update({ ...panel, calculation: value === "last" ? undefined : value as PanelCalculation })} />
      <p className="text-xs text-muted-foreground">{t("workspace.calculation_hint")}</p>
    </div>
    {error && <p role="alert" className="text-xs text-destructive">{t(error)}</p>}
  </fieldset>
}

function WorkspaceThresholds(props: { panel: DashboardPanel; update: (panel: DashboardPanel) => void }) {
  const { t } = useLocale()
  const error = thresholdDisplayError(props.panel)
  // 只有折线能画线、色带与背景；其他面板保留「按阈值取色」与「关闭」两种状态。
  const modes = props.panel.type === "timeseries"
    ? thresholdDisplayModes.map((value) => ({ value, label: t(`workspace.threshold_mode_${value}`) }))
    : [{ value: "", label: t("workspace.threshold_mode_color") }, { value: "off", label: t("workspace.off") }]
  const selected = props.panel.type === "timeseries" ? panelThresholdDisplay(props.panel) : props.panel.threshold_mode ?? ""
  const change = (mode: string) => props.update({ ...props.panel, threshold_mode: mode === "line" || mode === "" ? undefined : mode as ThresholdDisplayMode })
  function edit(index: number, patch: Partial<DashboardPanel["thresholds"][number]>) {
    props.update({ ...props.panel, thresholds: props.panel.thresholds.map((value, current) => current === index ? { ...value, ...patch } : value) })
  }
  return <fieldset className="space-y-3"><legend className="mb-2 text-sm font-medium">{t("workspace.thresholds")}</legend>
    <div className="grid gap-2 sm:max-w-xs"><Label>{t("workspace.threshold_mode")}</Label>
      <WorkspaceSelect label={t("workspace.threshold_mode")} value={selected} options={modes} onChange={change} />
      <p className="text-xs text-muted-foreground">{t("workspace.threshold_mode_hint")}</p></div>
    {props.panel.thresholds.map((threshold, index) => <div key={index} className="flex items-center gap-3">
      <Input className="max-w-44" aria-label={`${t("workspace.threshold_value")} ${index + 1}`} type="number" step="any" value={Number.isFinite(threshold.value) ? threshold.value : ""} onChange={(event) => edit(index, { value: event.target.value === "" ? NaN : Number(event.target.value) })} />
      <WorkspaceSelect label={`${t("workspace.color")} ${index + 1}`} value={threshold.color} options={Object.keys(panelColors).map((color) => ({ value: color, label: t(`workspace.${color}`) }))} onChange={(color) => edit(index, { color: color as PanelColor })} />
      <Button size="icon-sm" variant="ghost" aria-label={t("workspace.threshold_remove")} onClick={() => props.update({ ...props.panel, thresholds: props.panel.thresholds.filter((_, current) => current !== index) })}><XIcon /></Button>
    </div>)}
    <Button variant="outline" size="sm" disabled={props.panel.thresholds.length >= 4} onClick={() => props.update({ ...props.panel, thresholds: [...props.panel.thresholds, { value: (props.panel.thresholds.at(-1)?.value ?? 70) + 10, color: "amber" }] })}>{t("workspace.add_threshold")}</Button>
    {error && <p role="alert" className="text-xs text-destructive">{t(error)}</p>}
  </fieldset>
}

/** Grafana 的 Data links：表格化的标题/URL/新标签页编辑，URL 里可插入面板上下文占位符。 */
export function WorkspacePanelLinks(props: { panel: DashboardPanel; update: (panel: DashboardPanel) => void }) {
  const { t } = useLocale()
  const panel = props.panel, update = props.update
  const links = panelLinks(panel)
  const change = (index: number, patch: Partial<DashboardPanelLink>) =>
    update({ ...panel, links: links.map((link, current) => current === index ? { ...link, ...patch } : link) })
  const placeholders = panelLinkPlaceholders.map((name) => "$" + "{" + name + "}").join(" ")
  return <fieldset className="space-y-3 border-t pt-5">
    <legend className="text-sm font-medium">{t("workspace.links")}</legend>
    <p className="text-xs text-muted-foreground">{t("workspace.links_hint")}</p>
    <p className="font-mono text-[11px] text-muted-foreground">{placeholders}</p>
    {links.map((link, index) => <div key={index} className="flex flex-wrap items-center gap-3 rounded-md border p-2">
      <Input className="max-w-48" maxLength={maximumPanelLinkTitleLength} aria-label={`${t("workspace.link_title")} ${index + 1}`} placeholder={t("workspace.link_title")}
        value={link.title} onChange={(event) => change(index, { title: event.target.value })} />
      <Input className="min-w-64 flex-1" aria-label={`${t("workspace.link_url")} ${index + 1}`} placeholder="/admin/metrics?node_id=${node}"
        value={link.url} onChange={(event) => change(index, { url: event.target.value })} />
      <WorkspaceLinkIconSelect link={link} label={`${t("workspace.link_icon")} ${index + 1}`} onChange={(icon) => change(index, { icon })} />
      <label className="flex items-center gap-2 text-sm"><Checkbox checked={link.new_tab === true} onCheckedChange={(checked) => change(index, { new_tab: checked === true ? true : undefined })} />{t("workspace.link_new_tab")}</label>
      <Button size="icon-sm" variant="ghost" aria-label={`${t("workspace.link_remove")} ${index + 1}`} onClick={() => update({ ...panel, links: links.filter((_, current) => current !== index) })}><XIcon /></Button>
      {panelLinkError(link) && <p role="alert" className="w-full text-xs text-destructive">{t("workspace.link_invalid")}</p>}
    </div>)}
    <Button variant="outline" size="sm" disabled={links.length >= maximumPanelLinks} onClick={() => update({ ...panel, links: [...links, emptyPanelLink()] })}>{t("workspace.add_link")}</Button>
  </fieldset>
}
