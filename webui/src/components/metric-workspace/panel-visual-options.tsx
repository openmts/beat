import { XIcon } from "lucide-react"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Switch } from "@/components/ui/switch"
import { useLocale } from "@/context/locale"
import { barAlignments, barChartError, defaultBarWidth, maxBarRadius, maxBarWidth, minBarRadius, minBarWidth, panelBarAlignment, panelBarChart } from "@/lib/workspace-bar-chart"
import { barDisplays, barGaugeError, barOrientations, panelBarGauge } from "@/lib/workspace-bar-gauge"
import { panelColors } from "@/lib/workspace-chart"
import { emptyGeoLocation, geoError, geoLayer, geoLayers, maxGeoLocations, maxGeoMarkerSize, maxGeoZoom, minGeoMarkerSize, minGeoZoom } from "@/lib/workspace-geomap"
import { defaultHeatmapBuckets, heatmapError, heatmapScales, maxHeatmapBuckets, minHeatmapBuckets, panelHeatmap } from "@/lib/workspace-heatmap"
import { defaultHistogramBuckets, defaultHistogramMerge, histogramError, histogramScales, maxHistogramBuckets, minHistogramBuckets, panelHistogram } from "@/lib/workspace-histogram"
import { panelStatStyle, statColorModes, statOrientations, statPatch, statStyleError, statTextModes, statTextSizes } from "@/lib/workspace-stat"
import { gaugeThresholdError, gaugeThresholdToggle, panelGaugeThresholds } from "@/lib/workspace-gauge"
import { connectNullsModes, connectNullsPatch, defaultLineWidth, gradientModes, gradientPatch, interpolationPatch, lineInterpolations, linePatternPatch, linePatterns, lineStyleError, maximumFillOpacity, maximumLineWidth, maximumPointSize, minimumLineWidth, minimumPointSize, panelLineStyle, pointRadius, showPointsModes, showPointsPatch, stackModes, stackPatch } from "@/lib/workspace-line-style"
import { fillBelowStackPatch } from "@/lib/workspace-overrides"
import { panelPieChart, pieChartError, pieChartTypes, pieLabelModes } from "@/lib/workspace-pie-chart"
import { panelStateTimeline, stateTimelineError, timelineValueModes } from "@/lib/workspace-state-timeline"
import { cellDisplayModeError, panelCellDisplayMode, panelTableOrientation, tableOrientationError } from "@/lib/workspace-table"
import { WorkspaceSelect } from "./select"
import { cellDisplayModes, tableOrientations, type BarAlignment, type BarDisplay, type BarOrientation, type DashboardCellDisplayMode, type DashboardTableOrientation, type DashboardGeoLocation, type DashboardPanel, type GeoLayer, type HeatmapScale, type HistogramScale, type PanelColor, type PieChartType, type PieLabelMode, type TimelineShowValues } from "@/types/metric-workspace"

// 面板可视化专属选项：折线与柱状、条状仪表、饼图、热力图各自只显示自己的字段。
/** Grafana 的折线与柱状样式：线宽、面积填充、渐变、数据点与堆叠；柱状图共用填充、渐变与堆叠。 */
export function WorkspaceLineStyle(props: { panel: DashboardPanel; update: (panel: DashboardPanel) => void }) {
  const { t } = useLocale()
  const panel = props.panel, update = props.update
  const bars = panel.type === "barchart"
  const style = panelLineStyle(panel)
  const error = lineStyleError(panel)
  const numbers = (bars
    ? [["fill_opacity", t("workspace.fill_opacity"), 0, maximumFillOpacity, String(maximumFillOpacity)]]
    : [["line_width", t("workspace.line_width"), minimumLineWidth, maximumLineWidth, String(defaultLineWidth)],
      ["fill_opacity", t("workspace.fill_opacity"), 0, maximumFillOpacity, "0"]]) as readonly (readonly ["line_width" | "fill_opacity", string, number, number, string])[]
  return <fieldset className="space-y-3 border-t pt-5">
    <legend className="text-sm font-medium">{t(bars ? "workspace.bar_chart_style" : "workspace.line_style")}</legend>
    <div className="grid gap-4 sm:grid-cols-4">
      {numbers.map(([key, label, min, max, example]) => <div key={key} className="grid gap-2"><Label htmlFor={`panel-${key}`}>{label}</Label>
        <Input id={`panel-${key}`} type="number" min={min} max={max} placeholder={example} value={panel[key] ?? ""}
          onChange={(event) => update({ ...panel, [key]: event.target.value === "" ? undefined : Number(event.target.value) })} /></div>)}
      {!bars && <div className="grid gap-2"><Label>{t("workspace.line_interpolation")}</Label>
        <WorkspaceSelect label={t("workspace.line_interpolation")} value={style.interpolation}
          options={lineInterpolations.map((mode) => ({ value: mode, label: t(`workspace.line_interpolation_${mode}`) }))}
          onChange={(mode) => update({ ...panel, ...interpolationPatch(mode) })} /></div>}
      {!bars && <div className="grid gap-2"><Label>{t("workspace.line_pattern")}</Label>
        <WorkspaceSelect label={t("workspace.line_pattern")} value={style.pattern}
          options={linePatterns.map((pattern) => ({ value: pattern, label: t(`workspace.line_pattern_${pattern}`) }))}
          onChange={(pattern) => update({ ...panel, ...linePatternPatch(pattern) })} /></div>}
      {!bars && <div className="grid gap-2"><Label>{t("workspace.connect_nulls")}</Label>
        <WorkspaceSelect label={t("workspace.connect_nulls")} value={style.connectNulls}
          options={connectNullsModes.map((mode) => ({ value: mode, label: t(`workspace.connect_nulls_${mode}`) }))}
          onChange={(mode) => update({ ...panel, ...connectNullsPatch(mode) })} /></div>}
      {!bars && <div className="grid gap-2"><Label>{t("workspace.show_points")}</Label>
        <WorkspaceSelect label={t("workspace.show_points")} value={style.showPoints} options={showPointsModes.map((mode) => ({ value: mode, label: t(`workspace.show_points_${mode}`) }))}
          onChange={(mode) => update({ ...panel, ...showPointsPatch(panel, mode) })} /></div>}
      {!bars && <div className="grid gap-2"><Label htmlFor="panel-point_size">{t("workspace.point_size")}</Label>
        <Input id="panel-point_size" type="number" min={minimumPointSize} max={maximumPointSize} step="1" placeholder={String(pointRadius(style))}
          disabled={style.showPoints === "never"} value={panel.point_size ?? ""}
          onChange={(event) => update({ ...panel, point_size: event.target.value === "" ? undefined : Number(event.target.value) })} /></div>}
      <div className="grid gap-2"><Label>{t("workspace.gradient_mode")}</Label>
        <WorkspaceSelect label={t("workspace.gradient_mode")} value={style.gradientMode}
          options={gradientModes.map((mode) => ({ value: mode, label: t(`workspace.gradient_mode_${mode}`) }))}
          onChange={(mode) => update({ ...panel, ...gradientPatch(panel, mode) })} /></div>
      <div className="grid gap-2"><Label>{t("workspace.stack")}</Label>
        <WorkspaceSelect label={t("workspace.stack")} value={style.stack} options={stackModes.map((mode) => ({ value: mode, label: t(`workspace.stack_${mode}`) }))}
          onChange={(mode) => update({ ...panel, ...stackPatch(panel, mode), ...fillBelowStackPatch(mode, panel.overrides) })} /></div>
    </div>
    <p className="text-xs text-muted-foreground">{t(bars ? "workspace.bar_chart_style_hint" : "workspace.line_style_hint")}</p>
    {error && <p role="alert" className="text-xs text-destructive">{t(error)}</p>}
  </fieldset>
}

/** Grafana 的 Stat styles：Color mode（阈值色落在数值还是面板底色）、Text mode（显示什么）、
 *  Text size（主文本字号）与 Orientation（名称与数值的排版方向）。 */
export function WorkspaceStatOptions(props: { panel: DashboardPanel; update: (panel: DashboardPanel) => void }) {
  const { t } = useLocale()
  const panel = props.panel, update = props.update
  const style = panelStatStyle(panel)
  const error = statStyleError(panel)
  const selects = ([["stat_color_mode", style.colorMode, statColorModes], ["stat_text_mode", style.textMode, statTextModes],
    ["stat_text_size", style.textSize, statTextSizes], ["stat_orientation", style.orientation, statOrientations]] as const)
  return <fieldset className="space-y-3 border-t pt-5">
    <legend className="text-sm font-medium">{t("workspace.stat_style")}</legend>
    <div className="grid gap-4 sm:grid-cols-4">
      {selects.map(([key, value, modes]) => <div key={key} className="grid gap-2"><Label>{t(`workspace.${key}`)}</Label>
        <WorkspaceSelect label={t(`workspace.${key}`)} value={value}
          options={modes.map((mode) => ({ value: mode, label: t(`workspace.${key}_${mode}`) }))}
          onChange={(mode) => update({ ...panel, ...statPatch(key, mode) })} /></div>)}
    </div>
    <p className="text-xs text-muted-foreground">{t("workspace.stat_style_hint")}</p>
    {error && <p role="alert" className="text-xs text-destructive">{t(error)}</p>}
  </fieldset>
}

/** Grafana 的 Gauge 选项：阈值刻度标记与阈值标签；两项都只对 gauge 面板显示。 */
export function WorkspaceGaugeOptions(props: { panel: DashboardPanel; update: (panel: DashboardPanel) => void }) {
  const { t } = useLocale()
  const panel = props.panel, update = props.update
  const style = panelGaugeThresholds(panel)
  const error = gaugeThresholdError(panel)
  const toggles = ([["gauge_threshold_markers", style.markers], ["gauge_threshold_labels", style.labels]] as const)
  return <fieldset className="space-y-3 border-t pt-5">
    <legend className="text-sm font-medium">{t("workspace.gauge_options")}</legend>
    <div className="grid gap-4 sm:grid-cols-4">
      {toggles.map(([key, checked]) => <div key={key} className="flex items-center gap-3 self-center">
        <Switch id={`panel-${key}`} checked={checked} onCheckedChange={(value: boolean) => update({ ...panel, ...gaugeThresholdToggle(key, value) })} />
        <Label htmlFor={`panel-${key}`}>{t(`workspace.${key}`)}</Label></div>)}
    </div>
    <p className="text-xs text-muted-foreground">{t("workspace.gauge_threshold_hint")}</p>
    {error && <p role="alert" className="text-xs text-destructive">{t(error)}</p>}
  </fieldset>
}

/** Grafana 的 Bar chart 选项：柱宽与柱顶圆角；只对 barchart 面板显示。 */
export function WorkspaceBarChartOptions(props: { panel: DashboardPanel; update: (panel: DashboardPanel) => void }) {
  const { t } = useLocale()
  const panel = props.panel, update = props.update
  const style = panelBarChart(panel)
  const error = barChartError(panel)
  const numbers = ([
    ["bar_width", t("workspace.bar_width"), minBarWidth, maxBarWidth, style.width],
    ["bar_radius", t("workspace.bar_radius"), minBarRadius, maxBarRadius, style.radius],
  ] as const).map(([key, label, min, max, resolved]) => ({ key, label, min, max, resolved }))
  return <fieldset className="space-y-3 border-t pt-5">
    <legend className="text-sm font-medium">{t("workspace.bar_chart_options")}</legend>
    <div className="grid gap-4 sm:grid-cols-4">
      {numbers.map(({ key, label, min, max, resolved }) => <div key={key} className="grid gap-2"><Label htmlFor={`panel-${key}`}>{label}</Label>
        <Input id={`panel-${key}`} type="number" min={min} max={max} step="1" placeholder={String(resolved)} value={panel[key] ?? ""}
          onChange={(event) => update({ ...panel, [key]: event.target.value === "" ? undefined : Number(event.target.value) })} /></div>)}
      <div className="grid gap-2"><Label>{t("workspace.bar_alignment")}</Label>
        <WorkspaceSelect label={t("workspace.bar_alignment")} value={panelBarAlignment(panel)}
          options={barAlignments.map((value) => ({ value, label: t(`workspace.bar_alignment_${value}`) }))}
          onChange={(value) => update({ ...panel, ...(value === "auto" ? { bar_alignment: undefined } : { bar_alignment: value as BarAlignment }) })} /></div>
    </div>
    <p className="text-xs text-muted-foreground">{t("workspace.bar_chart_options_hint", { width: String(defaultBarWidth) })}</p>
    {error && <p role="alert" className="text-xs text-destructive">{t(error)}</p>}
  </fieldset>
}

/** Grafana 的 Bar gauge 选项：排列方向、填充方式与未填充轨道；只对 bargauge 面板显示。 */
export function WorkspaceBarGaugeOptions(props: { panel: DashboardPanel; update: (panel: DashboardPanel) => void }) {
  const { t } = useLocale()
  const panel = props.panel, update = props.update
  const style = panelBarGauge(panel)
  const error = barGaugeError(panel)
  return <fieldset className="space-y-3 border-t pt-5">
    <legend className="text-sm font-medium">{t("workspace.bar_gauge")}</legend>
    <div className="grid gap-4 sm:grid-cols-3">
      <div className="grid gap-2"><Label>{t("workspace.bar_orientation")}</Label>
        <WorkspaceSelect label={t("workspace.bar_orientation")} value={style.orientation} options={barOrientations.map((value) => ({ value, label: t(`workspace.bar_orientation_${value}`) }))}
          onChange={(value) => update({ ...panel, ...(value === "auto" ? { bar_orientation: undefined } : { bar_orientation: value as BarOrientation }) })} /></div>
      <div className="grid gap-2"><Label>{t("workspace.bar_display")}</Label>
        <WorkspaceSelect label={t("workspace.bar_display")} value={style.display} options={barDisplays.map((value) => ({ value, label: t(`workspace.bar_display_${value}`) }))}
          onChange={(value) => update({ ...panel, ...(value === "basic" ? { bar_display: undefined } : { bar_display: value as BarDisplay }) })} /></div>
      <div className="grid gap-2"><Label htmlFor="panel-bar-unfilled">{t("workspace.bar_unfilled")}</Label>
        <Switch id="panel-bar-unfilled" checked={style.unfilled} onCheckedChange={(checked) => update({ ...panel, ...(checked ? { bar_unfilled: undefined } : { bar_unfilled: false }) })} /></div>
    </div>
    <p className="text-xs text-muted-foreground">{t("workspace.bar_gauge_hint")}</p>
    {error && <p role="alert" className="text-xs text-destructive">{t(error)}</p>}
  </fieldset>
}

/** Grafana 的 Pie chart 选项：实心饼图/环形图与扇区标签内容；只对 piechart 面板显示。 */
export function WorkspacePieChartOptions(props: { panel: DashboardPanel; update: (panel: DashboardPanel) => void }) {
  const { t } = useLocale()
  const panel = props.panel, update = props.update
  const style = panelPieChart(panel)
  const error = pieChartError(panel)
  return <fieldset className="space-y-3 border-t pt-5">
    <legend className="text-sm font-medium">{t("workspace.pie_chart")}</legend>
    <div className="grid gap-4 sm:grid-cols-2">
      <div className="grid gap-2"><Label>{t("workspace.pie_chart_type")}</Label>
        <WorkspaceSelect label={t("workspace.pie_chart_type")} value={style.chartType} options={pieChartTypes.map((value) => ({ value, label: t(`workspace.pie_chart_type_${value}`) }))}
          onChange={(value) => update({ ...panel, ...(value === "pie" ? { pie_chart_type: undefined } : { pie_chart_type: value as PieChartType }) })} /></div>
      <div className="grid gap-2"><Label>{t("workspace.pie_label_mode")}</Label>
        <WorkspaceSelect label={t("workspace.pie_label_mode")} value={style.labelMode} options={pieLabelModes.map((value) => ({ value, label: t(`workspace.pie_label_mode_${value}`) }))}
          onChange={(value) => update({ ...panel, ...(value === "percent" ? { pie_label_mode: undefined } : { pie_label_mode: value as PieLabelMode }) })} /></div>
    </div>
    <p className="text-xs text-muted-foreground">{t("workspace.pie_chart_hint")}</p>
    {error && <p role="alert" className="text-xs text-destructive">{t(error)}</p>}
  </fieldset>
}

/** Grafana 的 Heatmap 选项：取值轴分桶、刻度与配色；这三项只对热力图生效。 */
export function WorkspaceHeatmapOptions(props: { panel: DashboardPanel; update: (panel: DashboardPanel) => void }) {
  const { t } = useLocale()
  const panel = props.panel, update = props.update
  const style = panelHeatmap(panel)
  const error = heatmapError(panel)
  return <fieldset className="space-y-3 border-t pt-5">
    <legend className="text-sm font-medium">{t("workspace.heatmap")}</legend>
    <div className="grid gap-4 sm:grid-cols-3">
      <div className="grid gap-2"><Label htmlFor="heatmap-buckets">{t("workspace.heatmap_buckets")}</Label>
        <Input id="heatmap-buckets" type="number" min={minHeatmapBuckets} max={maxHeatmapBuckets} step="1" placeholder={String(defaultHeatmapBuckets)}
          value={panel.heatmap_buckets ?? ""} onChange={(event) => update({ ...panel, heatmap_buckets: event.target.value === "" ? undefined : Number(event.target.value) })} /></div>
      <div className="grid gap-2"><Label>{t("workspace.heatmap_scale")}</Label>
        <WorkspaceSelect label={t("workspace.heatmap_scale")} value={style.scale} options={heatmapScales.map((value) => ({ value, label: t(`workspace.heatmap_scale_${value}`) }))}
          onChange={(value) => update({ ...panel, ...(value === "linear" ? { heatmap_scale: undefined } : { heatmap_scale: value as HeatmapScale }) })} /></div>
      <div className="grid gap-2"><Label>{t("workspace.heatmap_color")}</Label>
        <WorkspaceSelect label={t("workspace.heatmap_color")} value={style.color} options={Object.keys(panelColors).map((value) => ({ value, label: t(`workspace.${value}`) }))}
          onChange={(color) => update({ ...panel, ...(color === "blue" ? { heatmap_color: undefined } : { heatmap_color: color as PanelColor }) })} /></div>
    </div>
    <p className="text-xs text-muted-foreground">{t("workspace.heatmap_hint")}</p>
    {error && <p role="alert" className="text-xs text-destructive">{t(error)}</p>}
  </fieldset>
}

/** Grafana 的 Histogram 选项：取值分桶、计数轴刻度、合并序列与配色；这四项只对直方图生效。 */
export function WorkspaceHistogramOptions(props: { panel: DashboardPanel; update: (panel: DashboardPanel) => void }) {
  const { t } = useLocale()
  const panel = props.panel, update = props.update
  const style = panelHistogram(panel)
  const error = histogramError(panel)
  return <fieldset className="space-y-3 border-t pt-5">
    <legend className="text-sm font-medium">{t("workspace.histogram")}</legend>
    <div className="grid gap-4 sm:grid-cols-4">
      <div className="grid gap-2"><Label htmlFor="histogram-buckets">{t("workspace.histogram_buckets")}</Label>
        <Input id="histogram-buckets" type="number" min={minHistogramBuckets} max={maxHistogramBuckets} step="1" placeholder={String(defaultHistogramBuckets)}
          value={panel.histogram_buckets ?? ""} onChange={(event) => update({ ...panel, histogram_buckets: event.target.value === "" ? undefined : Number(event.target.value) })} /></div>
      <div className="grid gap-2"><Label>{t("workspace.histogram_scale")}</Label>
        <WorkspaceSelect label={t("workspace.histogram_scale")} value={style.scale} options={histogramScales.map((value) => ({ value, label: t(`workspace.histogram_scale_${value}`) }))}
          onChange={(value) => update({ ...panel, ...(value === "linear" ? { histogram_scale: undefined } : { histogram_scale: value as HistogramScale }) })} /></div>
      <div className="grid gap-2"><Label>{t("workspace.histogram_color")}</Label>
        <WorkspaceSelect label={t("workspace.histogram_color")} value={style.color} options={Object.keys(panelColors).map((value) => ({ value, label: t(`workspace.${value}`) }))}
          onChange={(color) => update({ ...panel, ...(color === "blue" ? { histogram_color: undefined } : { histogram_color: color as PanelColor }) })} /></div>
      <div className="flex items-center gap-3 self-end pb-2">
        <Switch id="histogram-merge" checked={style.merge} onCheckedChange={(checked: boolean) => update({ ...panel, histogram_merge: checked === defaultHistogramMerge ? undefined : checked })} />
        <Label htmlFor="histogram-merge">{t("workspace.histogram_merge")}</Label></div>
    </div>
    <p className="text-xs text-muted-foreground">{t("workspace.histogram_hint")}</p>
    {error && <p role="alert" className="text-xs text-destructive">{t(error)}</p>}
  </fieldset>
}

/** Grafana 的 State timeline 选项：合并相邻的相同状态与取值标签；只对 statetimeline 面板显示。 */
export function WorkspaceStateTimelineOptions(props: { panel: DashboardPanel; update: (panel: DashboardPanel) => void }) {
  const { t } = useLocale()
  const panel = props.panel, update = props.update
  const style = panelStateTimeline(panel)
  const error = stateTimelineError(panel)
  return <fieldset className="space-y-3 border-t pt-5">
    <legend className="text-sm font-medium">{t("workspace.statetimeline")}</legend>
    <div className="grid gap-4 sm:grid-cols-3">
      <div className="flex items-center gap-3 self-end pb-2">
        <Switch id="timeline-merge" checked={style.merge} onCheckedChange={(checked: boolean) => update({ ...panel, timeline_merge: checked ? undefined : false })} />
        <Label htmlFor="timeline-merge">{t("workspace.state_timeline_merge")}</Label>
      </div>
      <div className="grid gap-2"><Label>{t("workspace.state_timeline_show_values")}</Label>
        <WorkspaceSelect label={t("workspace.state_timeline_show_values")} value={style.showValues} options={timelineValueModes.map((value) => ({ value, label: t(`workspace.state_timeline_show_values_${value}`) }))}
          onChange={(value) => update({ ...panel, ...(value === "auto" ? { timeline_show_values: undefined } : { timeline_show_values: value as TimelineShowValues }) })} /></div>
    </div>
    <p className="text-xs text-muted-foreground">{t("workspace.state_timeline_hint")}</p>
    {error && <p role="alert" className="text-xs text-destructive">{t(error)}</p>}
  </fieldset>
}

/** Grafana 的 Geomap 选项：地点表、图层、缩放、中心、标记大小与可选底图模板；只对 geomap 面板显示。 */
export function WorkspaceGeoMapOptions(props: { panel: DashboardPanel; update: (panel: DashboardPanel) => void }) {
  const { t } = useLocale()
  const panel = props.panel, update = props.update
  const locations = panel.geo_locations ?? []
  const error = geoError(panel)
  const change = (index: number, patch: Partial<DashboardGeoLocation>) => update({ ...panel,
    geo_locations: locations.map((entry, position) => position === index ? { ...entry, ...patch } : entry) })
  const coordinate = (value: number) => Number.isFinite(value) ? value : ""
  return <fieldset className="space-y-3 border-t pt-5">
    <legend className="text-sm font-medium">{t("workspace.geomap")}</legend>
    <div className="grid gap-2"><Label>{t("workspace.geomap_locations")}</Label>
      {locations.map((location, index) => <div key={index} className="grid gap-2 sm:grid-cols-[2fr_2fr_1fr_1fr_auto]">
        <Input aria-label={`${t("workspace.geomap_match")} ${index + 1}`} placeholder={t("workspace.geomap_match")} value={location.match}
          onChange={(event) => change(index, { match: event.target.value })} />
        <Input aria-label={`${t("workspace.geomap_label")} ${index + 1}`} placeholder={t("workspace.geomap_label")} value={location.label ?? ""}
          onChange={(event) => change(index, { label: event.target.value === "" ? undefined : event.target.value })} />
        <Input aria-label={`${t("workspace.geomap_latitude")} ${index + 1}`} type="number" step="any" placeholder={t("workspace.geomap_latitude")}
          value={coordinate(location.latitude)} onChange={(event) => change(index, { latitude: event.target.value === "" ? Number.NaN : Number(event.target.value) })} />
        <Input aria-label={`${t("workspace.geomap_longitude")} ${index + 1}`} type="number" step="any" placeholder={t("workspace.geomap_longitude")}
          value={coordinate(location.longitude)} onChange={(event) => change(index, { longitude: event.target.value === "" ? Number.NaN : Number(event.target.value) })} />
        <Button variant="ghost" size="icon-sm" title={t("workspace.geomap_remove_location")} aria-label={`${t("workspace.geomap_remove_location")} ${index + 1}`}
          onClick={() => update({ ...panel, geo_locations: locations.filter((_, position) => position !== index) })}><XIcon /></Button>
      </div>)}
      <div className="flex items-center gap-3">
        <Button variant="outline" size="sm" disabled={locations.length >= maxGeoLocations}
          onClick={() => update({ ...panel, geo_locations: [...locations, emptyGeoLocation()] })}>{t("workspace.geomap_add_location")}</Button>
        <span className="text-xs text-muted-foreground">{t("workspace.geomap_locations_hint", { limit: maxGeoLocations })}</span>
      </div>
    </div>
    <div className="grid gap-4 sm:grid-cols-4">
      <div className="grid gap-2"><Label>{t("workspace.geomap_layer")}</Label>
        <WorkspaceSelect label={t("workspace.geomap_layer")} value={geoLayer(panel)} options={geoLayers.map((value) => ({ value, label: t(`workspace.geomap_layer_${value}`) }))}
          onChange={(value) => update({ ...panel, geo_layer: value === "markers" ? undefined : value as GeoLayer })} /></div>
      <div className="grid gap-2"><Label htmlFor="geomap-zoom">{t("workspace.geomap_zoom")}</Label>
        <Input id="geomap-zoom" type="number" min={minGeoZoom} max={maxGeoZoom} step="1" placeholder="0" value={panel.geo_zoom ?? ""}
          onChange={(event) => update({ ...panel, geo_zoom: event.target.value === "" ? undefined : Number(event.target.value) })} /></div>
      <div className="grid gap-2"><Label htmlFor="geomap-marker">{t("workspace.geomap_marker_size")}</Label>
        <Input id="geomap-marker" type="number" min={minGeoMarkerSize} max={maxGeoMarkerSize} step="1" placeholder="8" value={panel.geo_marker_size ?? ""}
          onChange={(event) => update({ ...panel, geo_marker_size: event.target.value === "" ? undefined : Number(event.target.value) })} /></div>
      <div className="grid gap-2"><Label htmlFor="geomap-center-lat">{t("workspace.geomap_center_latitude")}</Label>
        <Input id="geomap-center-lat" type="number" step="any" placeholder="0" value={panel.geo_center_latitude ?? ""}
          onChange={(event) => update({ ...panel, geo_center_latitude: event.target.value === "" ? undefined : Number(event.target.value) })} /></div>
      <div className="grid gap-2"><Label htmlFor="geomap-center-lon">{t("workspace.geomap_center_longitude")}</Label>
        <Input id="geomap-center-lon" type="number" step="any" placeholder="0" value={panel.geo_center_longitude ?? ""}
          onChange={(event) => update({ ...panel, geo_center_longitude: event.target.value === "" ? undefined : Number(event.target.value) })} /></div>
    </div>
    <div className="grid gap-2"><Label htmlFor="geomap-tiles">{t("workspace.geomap_tile_url")}</Label>
      <Input id="geomap-tiles" placeholder="https://tiles.example.com/{z}/{x}/{y}.png" value={panel.geo_tile_url ?? ""}
        onChange={(event) => update({ ...panel, geo_tile_url: event.target.value.trim() === "" ? undefined : event.target.value.trim() })} />
      <p className="text-xs text-muted-foreground">{t("workspace.geomap_tile_hint")}</p></div>
    {error && <p role="alert" className="text-xs text-destructive">{t(error)}</p>}
  </fieldset>
}

/** Grafana 的 Cell display mode：表格的值列按值映射与阈值着色。 */
export function WorkspaceCellDisplayOptions(props: { panel: DashboardPanel; update: (panel: DashboardPanel) => void }) {
  const { t } = useLocale()
  const errors = [tableOrientationError(props.panel), cellDisplayModeError(props.panel)].filter((value): value is string => value !== null)
  return <fieldset className="space-y-3 border-t pt-5">
    <legend className="text-sm font-medium">{t("workspace.table_options")}</legend>
    <div className="grid gap-4 sm:grid-cols-2">
      <div className="grid gap-2"><Label>{t("workspace.table_orientation")}</Label>
        <WorkspaceSelect label={t("workspace.table_orientation")} value={panelTableOrientation(props.panel)}
          options={tableOrientations.map((value) => ({ value, label: t(`workspace.table_orientation_${value}`) }))}
          onChange={(value) => props.update({ ...props.panel, table_orientation: value === "long" ? undefined : value as DashboardTableOrientation })} />
        <p className="text-xs text-muted-foreground">{t("workspace.table_orientation_hint")}</p>
      </div>
      <div className="grid gap-2"><Label>{t("workspace.cell_display_mode")}</Label>
        <WorkspaceSelect label={t("workspace.cell_display_mode")} value={panelCellDisplayMode(props.panel)}
          options={cellDisplayModes.map((value) => ({ value, label: t(`workspace.cell_display_${value}`) }))}
          onChange={(value) => props.update({ ...props.panel, cell_display_mode: value === "text" ? undefined : value as DashboardCellDisplayMode })} />
        <p className="text-xs text-muted-foreground">{t("workspace.cell_display_hint")}</p>
      </div>
    </div>
    {errors.map((error) => <p key={error} role="alert" className="text-xs text-destructive">{t(error)}</p>)}
  </fieldset>
}
