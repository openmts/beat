import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Switch } from "@/components/ui/switch"
import { useLocale } from "@/context/locale"
import { legendSortModes, legendValueKinds, maximumLegendValues, tooltipModes } from "@/lib/workspace-chart"
import { legendPlacement, legendPlacementPatch, legendPlacements, legendPlacementTypes, legendWidthError, maxLegendWidth, minLegendWidth } from "@/lib/workspace-legend-placement"
import { maximumQueryPoints, minimumQueryPoints } from "@/lib/workspace-query"
import { panelTimeOverrideError, panelTimeShiftError } from "@/lib/workspace-time-override"
import { axisError } from "@/lib/workspace-axis"
import { axisScale, axisScaleError, axisScaleModes, type AxisScaleMode } from "@/lib/workspace-axis-scale"
import { axisLabelError, maximumAxisLabelBytes } from "@/lib/workspace-axis-label"
import { axisDisplayError, axisGrid, axisPlacement, axisPlacements, centeredZeroError, type AxisPlacement } from "@/lib/workspace-axis-display"
import { WorkspaceSelect } from "./select"
import { useLegendText } from "./legend-text"
import type { DashboardPanel, LegendSort, LegendValue, TooltipMode } from "@/types/metric-workspace"

/** 面板级查询选项，语义与 Grafana 的 Query options 一致：留空即跟随看板范围与默认点数。 */
/** Grafana 的 Axis → Scale 与 Soft min / Soft max：只对折线与柱状面板显示。 */
export function WorkspaceAxisOptions(props: { panel: DashboardPanel; update: (panel: DashboardPanel) => void }) {
  const { t } = useLocale()
  const error = axisScaleError(props.panel) ?? axisError(props.panel) ?? axisLabelError(props.panel) ?? axisDisplayError(props.panel) ?? centeredZeroError(props.panel)
  return <div className="grid gap-2">
    <div className="grid gap-4 sm:grid-cols-4"><div className="grid gap-2"><Label>{t("workspace.axis_scale")}</Label>
      <WorkspaceSelect label={t("workspace.axis_scale")} value={axisScale(props.panel)}
        options={axisScaleModes.map((mode) => ({ value: mode, label: t(`workspace.axis_scale_${mode}`) }))}
        onChange={(value) => props.update({ ...props.panel, scale: value === "linear" ? undefined : value as AxisScaleMode })} /></div>
      {(["soft_min", "soft_max"] as const).map((key) => <div key={key} className="grid gap-2"><Label htmlFor={`panel-${key}`}>{t(`workspace.${key}`)}</Label>
        <Input id={`panel-${key}`} type="number" step="any" placeholder={t("workspace.auto")} value={props.panel[key] ?? ""}
          onChange={(event) => props.update({ ...props.panel, [key]: event.target.value === "" ? undefined : Number(event.target.value) })} /></div>)}
    </div>
    <div className="flex flex-wrap items-end gap-6">
      <div className="flex items-center gap-3"><Switch id="panel-grid" checked={axisGrid(props.panel)}
        onCheckedChange={(grid) => props.update({ ...props.panel, grid: grid ? undefined : false })} />
        <Label htmlFor="panel-grid">{t("workspace.axis_grid")}</Label></div>
      <div className="flex items-center gap-3"><Switch id="panel-centered_zero" checked={props.panel.centered_zero === true}
        onCheckedChange={(checked) => props.update({ ...props.panel, centered_zero: checked ? true : undefined })} />
        <Label htmlFor="panel-centered_zero">{t("workspace.centered_zero")}</Label></div>
      <div className="grid gap-2 sm:max-w-xs"><Label>{t("workspace.axis_placement")}</Label>
        <WorkspaceSelect label={t("workspace.axis_placement")} value={axisPlacement(props.panel)}
          options={axisPlacements.map((value) => ({ value, label: t(`workspace.axis_placement_${value}`) }))}
          onChange={(value) => props.update({ ...props.panel, axis_placement: value === "left" ? undefined : value as AxisPlacement })} /></div>
    </div>
    <div className="grid gap-2 sm:max-w-sm"><Label htmlFor="panel-axis_label">{t("workspace.axis_label")}</Label>
      <Input id="panel-axis_label" maxLength={maximumAxisLabelBytes} placeholder={t("workspace.axis_label_placeholder")}
        value={props.panel.axis_label ?? ""} onChange={(event) => props.update({ ...props.panel, axis_label: event.target.value === "" ? undefined : event.target.value })} />
      <p className="text-xs text-muted-foreground">{t("workspace.axis_label_hint")}</p></div>
    <p className="text-xs text-muted-foreground">{t("workspace.soft_axis_hint")}</p>
    <p className="text-xs text-muted-foreground">{t("workspace.axis_scale_hint")}</p>
    {error && <p role="alert" className="text-xs text-destructive">{t(error)}</p>}
  </div>
}

export function WorkspaceQueryOptions(props: { panel: DashboardPanel; defaultPoints?: number; update: (panel: DashboardPanel) => void }) {
  const { t } = useLocale()
  const error = panelTimeOverrideError(props.panel) ?? panelTimeShiftError(props.panel)
  const change = (key: "time_from" | "time_to" | "time_shift", value: string) => props.update({ ...props.panel, [key]: value.trim() || undefined })
  return <fieldset className="space-y-3 border-t pt-5">
    <legend className="text-sm font-medium">{t("workspace.query_options")}</legend>
    <p className="text-xs text-muted-foreground">{t("workspace.time_override_hint")}</p>
    <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-4">
      {([["time_from", "now-6h", "workspace.time_override_from"], ["time_to", "now", "workspace.time_override_to"], ["time_shift", "1d", "workspace.time_shift"]] as const).map(([key, example, labelKey]) => <div key={key} className="grid gap-2">
        <Label htmlFor={`panel-${key}`}>{t(labelKey)}</Label>
        <Input id={`panel-${key}`} placeholder={example} value={props.panel[key] ?? ""} onChange={(event) => change(key, event.target.value)} />
        {key === "time_shift" && <p className="text-xs text-muted-foreground">{t("workspace.time_shift_hint")}</p>}</div>)}
      <div className="grid gap-2"><Label htmlFor="panel-max_points">{t("workspace.max_points")}</Label>
        <Input id="panel-max_points" type="number" min={minimumQueryPoints} max={maximumQueryPoints} placeholder={String(props.defaultPoints ?? maximumQueryPoints)}
          value={props.panel.max_points ?? ""} onChange={(event) => props.update({ ...props.panel, max_points: event.target.value === "" ? undefined : Number(event.target.value) })} />
        <p className="text-xs text-muted-foreground">{t("workspace.max_points_hint")}</p></div>
    </div>
    {error && <p className="text-xs text-destructive">{t(error)}</p>}
  </fieldset>
}

// 图例取值与排序只在图例可见时生效；隐藏图例时同步清空，避免保存出无效组合。
export function WorkspaceLegendOptions(props: { panel: DashboardPanel; update: (panel: DashboardPanel) => void }) {
  const { t } = useLocale()
  const text = useLegendText()
  const values = props.panel.legend_values ?? []
  const hidden = !props.panel.legend
  const update = props.update, panel = props.panel
  const widthFailure = legendWidthError(panel)
  function toggleValue(value: LegendValue) {
    update({ ...panel, legend_values: values.includes(value) ? values.filter((candidate) => candidate !== value) : [...values, value] })
  }
  return <fieldset className="space-y-3 border-t pt-5">
    <legend className="text-sm font-medium">{t("workspace.legend")}</legend>
    <div className="flex items-center gap-3"><Switch id="panel-legend" checked={panel.legend}
      onCheckedChange={(legend) => update(legend ? { ...panel, legend } : { ...panel, legend, legend_values: [], legend_sort: "none", legend_placement: undefined, legend_width: undefined })} />
      <Label htmlFor="panel-legend">{t("workspace.legend")}</Label></div>
    {legendPlacementTypes.some((type) => type === panel.type) && <div className="grid gap-2 sm:max-w-sm"><Label>{t("workspace.legend_placement")}</Label>
      <WorkspaceSelect label={t("workspace.legend_placement")} value={legendPlacement(panel)} disabled={hidden}
        options={legendPlacements.map((value) => ({ value, label: t(`workspace.legend_placement_${value}`) }))}
        onChange={(value) => update({ ...panel, ...legendPlacementPatch(value), ...(value === "right" ? {} : { legend_width: undefined }) })} /></div>}
    {!hidden && legendPlacementTypes.some((type) => type === panel.type) && legendPlacement(panel) === "right" && <div className="grid gap-2 sm:max-w-xs">
      <Label htmlFor="panel-legend-width">{t("workspace.legend_width")}</Label>
      <Input id="panel-legend-width" type="number" min={minLegendWidth} max={maxLegendWidth} step="1" placeholder={t("workspace.auto")} value={panel.legend_width ?? ""}
        onChange={(event) => update({ ...panel, legend_width: event.target.value === "" ? undefined : Number(event.target.value) })} />
      <p className="text-xs text-muted-foreground">{t("workspace.legend_width_hint")}</p></div>}
    <div className="grid gap-4 sm:grid-cols-2">
      <div className="grid gap-2"><span className="text-sm text-muted-foreground">{text("values")}</span>
        <div className="flex flex-wrap gap-x-4 gap-y-2">{legendValueKinds.map((value) => <div key={value} className="flex items-center gap-2">
          <Switch id={`legend-value-${value}`} checked={values.includes(value)} disabled={hidden || (!values.includes(value) && values.length >= maximumLegendValues)} onCheckedChange={() => toggleValue(value)} />
          <Label htmlFor={`legend-value-${value}`}>{text(value)}</Label></div>)}</div></div>
      <div className="grid gap-2"><Label htmlFor="panel-legend-sort">{text("sort")}</Label>
        <WorkspaceSelect label={text("sort")} value={panel.legend_sort ?? "none"} disabled={hidden}
          options={legendSortModes.map((mode) => ({ value: mode, label: text(`sort_${mode}`) }))}
          onChange={(sort) => update({ ...panel, legend_sort: sort as LegendSort })} /></div>
    </div>
    <div className="grid gap-2 sm:max-w-xs"><Label htmlFor="panel-tooltip">{text("tooltip")}</Label>
      <WorkspaceSelect label={text("tooltip")} value={panel.tooltip ?? "all"}
        options={tooltipModes.map((mode) => ({ value: mode, label: text(`tooltip_${mode}`) }))}
        onChange={(tooltip) => update({ ...panel, tooltip: tooltip as TooltipMode })} /></div>
    {widthFailure && <p role="alert" className="text-xs text-destructive">{t(widthFailure)}</p>}
  </fieldset>
}
