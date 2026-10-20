import { XIcon } from "lucide-react"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Switch } from "@/components/ui/switch"
import { useLocale } from "@/context/locale"
import { gradientModes, maximumFillOpacity, maximumLineWidth, minimumLineWidth, panelLineStyle, stacksSeries } from "@/lib/workspace-line-style"
import { panelColors } from "@/lib/workspace-chart"
import { emptyOverride, maximumPanelOverrides, overrideError, overrideLimitPanelTypes, overrideMatcherTypeList, overrideSeriesTypeList, overrideStandardPanelTypes, overrideThresholdPanelTypes, switchOverrideMatcherType } from "@/lib/workspace-overrides"
import { axisSideSupported, type AxisSide } from "@/lib/workspace-axis-sides"
import { defaultSparklineLineWidth, maxSparklineFillOpacity, maxSparklineLineWidth, minSparklineLineWidth, panelSparkline, sparklineError, sparklineGradientPatch, sparklineStyle } from "@/lib/workspace-sparkline"
import { Checkbox } from "@/components/ui/checkbox"
import { WorkspaceSelect } from "./select"
import { WorkspaceOverrideThresholds } from "./override-thresholds"
import type { DashboardPanel, PanelColor, DashboardOverride } from "@/types/metric-workspace"

/** Grafana 的 Overrides 标签页：按序列标签命中后覆盖颜色、单位与图例可见性；覆盖不改采样值。 */
export function WorkspaceOverrides(props: { panel: DashboardPanel; update: (panel: DashboardPanel) => void }) {
  const { t } = useLocale()
  const panel = props.panel, update = props.update
  const overrides = panel.overrides ?? []
  const error = overrideError(panel)
  // 与服务端一致：线宽只属于折线，填充属于折线与柱状图，其它类型不显示这两项。
  const lineWidthEnabled = panel.type === "timeseries"
  const fillOpacityEnabled = panel.type === "timeseries" || panel.type === "barchart"
  // 按序列轴位置同样只对折线与柱状面板开放，其余类型不显示这一项。
  const axisPlacementEnabled = axisSideSupported(panel.type)
  // 逐序列阈值只显示在真正按阈值取色的面板上，其余类型服务端也会拒绝。
  const thresholdsEnabled = overrideThresholdPanelTypes.includes(panel.type)
  // 填充到另一条序列只在不堆叠的折线面板上有意义：堆叠面积和填充带不能同时渲染。
  const fillBelowEnabled = panel.type === "timeseries" && !stacksSeries(panelLineStyle(panel).stack)
  // Standard options 同样按面板类型门禁：显示名与小数位只要面板给序列渲染名字与数值，
  // Min/Max 只在有单值量程的面板上出现，与后端 validateOverrideStandard 一致。
  const standardEnabled = overrideStandardPanelTypes.includes(panel.type)
  const limitsEnabled = overrideLimitPanelTypes.includes(panel.type)
  const change = (index: number, override: DashboardOverride) =>
    update({ ...panel, overrides: overrides.map((value, current) => current === index ? override : value) })
  return <fieldset className="space-y-3 border-t pt-5">
    <legend className="text-sm font-medium">{t("workspace.overrides")}</legend>
    <p className="text-xs text-muted-foreground">{t("workspace.overrides_hint")}</p>
    {fillBelowEnabled && <p className="text-xs text-muted-foreground">{t("workspace.override_fill_below_hint")}</p>}
    {overrides.map((override, index) => <div key={index} className="flex flex-wrap items-center gap-3 rounded-md border p-2">
      <WorkspaceSelect label={`${t("workspace.override_matcher")} ${index + 1}`} value={override.matcher.type}
        options={overrideMatcherTypeList.map((type) => ({ value: type, label: t(`workspace.override_matcher_${type}`) }))}
        onChange={(type) => change(index, switchOverrideMatcherType(override, type))} />
      {override.matcher.type === "by_type"
        ? <WorkspaceSelect label={`${t("workspace.override_pattern")} ${index + 1}`} value={override.matcher.value}
          options={overrideSeriesTypeList.map((type) => ({ value: type, label: type }))}
          onChange={(value) => change(index, { ...override, matcher: { ...override.matcher, value } })} />
        : <Input className="max-w-56" type={override.matcher.type === "by_value" ? "number" : "text"}
          aria-label={`${t("workspace.override_pattern")} ${index + 1}`} placeholder={t("workspace.override_pattern")}
          value={override.matcher.value} onChange={(event) => change(index, { ...override, matcher: { ...override.matcher, value: event.target.value } })} />}
      <WorkspaceSelect label={`${t("workspace.override_color")} ${index + 1}`} value={override.color ?? "none"}
        options={[{ value: "none", label: t("workspace.override_inherit") }, ...Object.keys(panelColors).map((color) => ({ value: color, label: t(`workspace.${color}`) }))]}
        onChange={(color) => change(index, { ...override, color: color === "none" ? undefined : color as PanelColor })} />
      <Input className="max-w-32" aria-label={`${t("workspace.override_unit")} ${index + 1}`} placeholder={t("workspace.override_unit")}
        value={override.unit ?? ""} onChange={(event) => change(index, { ...override, unit: event.target.value || undefined })} />
      {lineWidthEnabled && <Input className="max-w-24" type="number" min={minimumLineWidth} max={maximumLineWidth}
        aria-label={`${t("workspace.override_line_width")} ${index + 1}`} placeholder={t("workspace.override_line_width")}
        value={override.line_width ?? ""} onChange={(event) => change(index, { ...override, line_width: event.target.value === "" ? undefined : Number(event.target.value) })} />}
      {fillOpacityEnabled && <Input className="max-w-24" type="number" min={0} max={maximumFillOpacity}
        aria-label={`${t("workspace.override_fill_opacity")} ${index + 1}`} placeholder={t("workspace.override_fill_opacity")}
        value={override.fill_opacity ?? ""} onChange={(event) => change(index, { ...override, fill_opacity: event.target.value === "" ? undefined : Number(event.target.value) })} />}
      {axisPlacementEnabled && <WorkspaceSelect label={`${t("workspace.override_axis_placement")} ${index + 1}`} value={override.axis_placement ?? "inherit"}
        options={[{ value: "inherit", label: t("workspace.override_inherit") },
          { value: "left", label: t("workspace.axis_placement_left") }, { value: "right", label: t("workspace.axis_placement_right") }]}
        onChange={(side) => change(index, { ...override, axis_placement: side === "inherit" ? undefined : side as AxisSide })} />}
      {axisPlacementEnabled && <Input className="max-w-40" aria-label={`${t("workspace.axis_label")} ${index + 1}`} placeholder={t("workspace.axis_label")}
        value={override.axis_label ?? ""} onChange={(event) => change(index, { ...override, axis_label: event.target.value || undefined })} />}
      {axisPlacementEnabled && <Input className="max-w-24" type="number" aria-label={`${t("workspace.soft_min")} ${index + 1}`} placeholder={t("workspace.soft_min")}
        value={override.axis_soft_min ?? ""} onChange={(event) => change(index, { ...override, axis_soft_min: event.target.value === "" ? undefined : Number(event.target.value) })} />}
      {axisPlacementEnabled && <Input className="max-w-24" type="number" aria-label={`${t("workspace.soft_max")} ${index + 1}`} placeholder={t("workspace.soft_max")}
        value={override.axis_soft_max ?? ""} onChange={(event) => change(index, { ...override, axis_soft_max: event.target.value === "" ? undefined : Number(event.target.value) })} />}
      {/* 逐序列轴宽：0 与留空都表示自动，范围与服务端 MaxDashboardAxisWidth 一致。 */}
      {axisPlacementEnabled && <Input className="max-w-24" type="number" aria-label={`${t("workspace.override_axis_width")} ${index + 1}`} placeholder={t("workspace.override_axis_width")}
        value={override.axis_width ?? ""} onChange={(event) => change(index, { ...override, axis_width: event.target.value === "" ? undefined : Number(event.target.value) })} />}
      {fillBelowEnabled && <Input className="max-w-40" aria-label={`${t("workspace.override_fill_below_to")} ${index + 1}`} placeholder={t("workspace.override_fill_below_to")}
        value={override.fill_below_to ?? ""} onChange={(event) => change(index, { ...override, fill_below_to: event.target.value || undefined })} />}
      {/* Grafana 的 Overrides → Standard options：显示名与小数位跟着会给序列渲染名字与数值的面板，
          值域只出现在把单值映射到一段量程的 stat / gauge / bargauge 上。 */}
      {standardEnabled && <Input className="max-w-40" aria-label={`${t("workspace.override_display_name")} ${index + 1}`} placeholder={t("workspace.override_display_name")}
        value={override.display_name ?? ""} onChange={(event) => change(index, { ...override, display_name: event.target.value || undefined })} />}
      {standardEnabled && <Input className="max-w-24" type="number" min={0} max={6} aria-label={`${t("workspace.override_decimals")} ${index + 1}`} placeholder={t("workspace.override_decimals")}
        value={override.decimals ?? ""} onChange={(event) => change(index, { ...override, decimals: event.target.value === "" ? undefined : Number(event.target.value) })} />}
      {limitsEnabled && <Input className="max-w-24" type="number" aria-label={`${t("workspace.override_min")} ${index + 1}`} placeholder={t("workspace.override_min")}
        value={override.min ?? ""} onChange={(event) => change(index, { ...override, min: event.target.value === "" ? undefined : Number(event.target.value) })} />}
      {limitsEnabled && <Input className="max-w-24" type="number" aria-label={`${t("workspace.override_max")} ${index + 1}`} placeholder={t("workspace.override_max")}
        value={override.max ?? ""} onChange={(event) => change(index, { ...override, max: event.target.value === "" ? undefined : Number(event.target.value) })} />}
      <label className="flex items-center gap-2 text-sm"><Checkbox checked={override.hide_legend === true}
        onCheckedChange={(checked) => change(index, { ...override, hide_legend: checked === true ? true : undefined })} />{t("workspace.override_hide_legend")}</label>
      {thresholdsEnabled && <WorkspaceOverrideThresholds index={index} thresholds={override.thresholds}
        change={(thresholds) => change(index, { ...override, thresholds })} />}
      <Button size="icon-sm" variant="ghost" aria-label={`${t("workspace.override_remove")} ${index + 1}`}
        onClick={() => update({ ...panel, overrides: overrides.filter((_, current) => current !== index) })}><XIcon /></Button>
    </div>)}
    <Button variant="outline" size="sm" disabled={overrides.length >= maximumPanelOverrides}
      onClick={() => update({ ...panel, overrides: [...overrides, emptyOverride()] })}>{t("workspace.add_override")}</Button>
    {error && <p role="alert" className="text-xs text-destructive">{t(error)}</p>}
  </fieldset>
}

/** Grafana 的 Value options → Calculation：取值面板先按计算方式把每条序列压成一个数。 */
/** Grafana 把 Sparkline 放在 stat 面板的 Value options 里：与值计算并列，只对 stat 生效。 */
export function WorkspaceSparklineOptions(props: { panel: DashboardPanel; update: (panel: DashboardPanel) => void }) {
  const { t } = useLocale()
  const panel = props.panel, update = props.update
  const error = sparklineError(panel)
  // 关闭开关时同时清掉线宽与填充：服务端会拒绝「配置写了但永远不渲染」的组合。
  const toggle = (checked: boolean) => update(checked
    ? { ...panel, sparkline: true }
    : { ...panel, sparkline: undefined, sparkline_line_width: undefined, sparkline_fill_opacity: undefined, sparkline_gradient_mode: undefined })
  return <fieldset className="space-y-3 border-t pt-5">
    <legend className="text-sm font-medium">{t("workspace.sparkline")}</legend>
    <div className="flex items-center gap-3"><Switch id="panel-sparkline" checked={panelSparkline(panel)} onCheckedChange={toggle} />
      <Label htmlFor="panel-sparkline">{t("workspace.sparkline")}</Label></div>
    {panelSparkline(panel) && <div className="grid gap-4 sm:grid-cols-4">
      <div className="grid gap-2"><Label htmlFor="panel-sparkline-width">{t("workspace.line_width")}</Label>
        <Input id="panel-sparkline-width" type="number" min={minSparklineLineWidth} max={maxSparklineLineWidth}
          placeholder={String(defaultSparklineLineWidth)} value={panel.sparkline_line_width ?? ""}
          onChange={(event) => update({ ...panel, sparkline_line_width: event.target.value === "" ? undefined : Number(event.target.value) })} /></div>
      <div className="grid gap-2"><Label htmlFor="panel-sparkline-fill">{t("workspace.fill_opacity")}</Label>
        <Input id="panel-sparkline-fill" type="number" min={0} max={maxSparklineFillOpacity} placeholder="0" value={panel.sparkline_fill_opacity ?? ""}
          onChange={(event) => update({ ...panel, sparkline_fill_opacity: event.target.value === "" ? undefined : Number(event.target.value) })} /></div>
      <div className="grid gap-2"><Label>{t("workspace.gradient_mode")}</Label>
        <WorkspaceSelect label={t("workspace.gradient_mode")} value={sparklineStyle(panel).gradientMode}
          options={gradientModes.map((mode) => ({ value: mode, label: t(`workspace.gradient_mode_${mode}`) }))}
          onChange={(mode) => update({ ...panel, ...sparklineGradientPatch(panel, mode) })} /></div>
    </div>}
    <p className="text-xs text-muted-foreground">{t("workspace.sparkline_hint")}</p>
    {error && <p role="alert" className="text-xs text-destructive">{t(error)}</p>}
  </fieldset>
}
