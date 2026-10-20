import { PlusIcon, XIcon } from "lucide-react"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { useLocale } from "@/context/locale"
import { additionalWorkspaceSeries } from "@/lib/workspace-panel-editor"
import { panelColors } from "@/lib/workspace-chart"
import { isAcceleratorMetric } from "@/lib/accelerator-metrics"
import { AcceleratorDeviceField } from "./accelerator-device-field"
import { WorkspaceSelect } from "./select"
import type { Node } from "@/types"
import type { DashboardDefinition, DashboardPanel, DashboardSeries, MetricAggregation, MetricDefinition, PanelColor } from "@/types/metric-workspace"

interface SeriesFieldsProps {
  panel: DashboardPanel; definition: DashboardDefinition; metrics: MetricDefinition[]; nodes: Node[]
  update: (panel: DashboardPanel) => void
}

export function WorkspaceSeriesFields(props: SeriesFieldsProps) {
  const { t } = useLocale()
  function change(index: number, series: DashboardSeries) { props.update({ ...props.panel, series: props.panel.series.map((value, current) => current === index ? series : value) }) }
  return <div className="space-y-3">
    {props.panel.series.map((series, index) => <fieldset key={series.ref_id} className="space-y-3 border-b pb-4">
      <legend className="mb-2 flex w-full items-center justify-between text-xs font-medium"><span>{t("workspace.query")} {series.ref_id}</span>
        <Button variant="ghost" size="icon-sm" aria-label={`${t("workspace.remove_series")} ${series.ref_id}`} disabled={props.panel.series.length === 1} onClick={() => props.update({ ...props.panel, series: props.panel.series.filter((_, current) => current !== index) })}><XIcon /></Button></legend>
      <WorkspaceSeriesRow {...props} series={series} onChange={(value) => change(index, value)} />
      <div className="grid gap-3 sm:grid-cols-[1fr_9rem]"><Input aria-label={`${t("workspace.series_label")} ${series.ref_id}`} placeholder={t("workspace.series_label")} value={series.label} maxLength={120} onChange={(event) => change(index, { ...series, label: event.target.value })} />
        <WorkspaceSelect label={`${t("workspace.color")} ${series.ref_id}`} value={series.color} options={Object.keys(panelColors).map((color) => ({ value: color, label: t(`workspace.${color}`) }))} onChange={(color) => change(index, { ...series, color: color as PanelColor })} className="w-full" /></div>
    </fieldset>)}
    <Button variant="outline" size="sm" disabled={props.panel.series.length >= 8} onClick={() => props.update({ ...props.panel, series: [...props.panel.series, additionalWorkspaceSeries(props.panel)] })}><PlusIcon />{t("workspace.add_series")}</Button>
  </div>
}

function WorkspaceSeriesRow(props: SeriesFieldsProps & { series: DashboardSeries; onChange: (value: DashboardSeries) => void }) {
  const { t } = useLocale()
  const selected = props.metrics.find((metric) => metric.key === props.series.metric)
  const nodes = props.definition.variables.map((variable) => ({ value: `$${variable.name}`, label: t("workspace.variable", { name: variable.label }) })).concat(props.nodes.map((node) => ({ value: node.id, label: node.alias || node.name })))
  function metricChanged(key: string) {
    const metric = props.metrics.find((value) => value.key === key)
    if (metric) props.onChange({ ...props.series, metric: key, aggregation: metric.default_aggregation, device_id: isAcceleratorMetric(key) ? props.series.device_id : undefined })
  }
  return <div className="space-y-3"><div className="grid gap-3 sm:grid-cols-3">
    <WorkspaceSelect label={`${t("workspace.node")} ${props.series.ref_id}`} value={props.series.node_id} options={nodes} onChange={(node_id) => props.onChange({ ...props.series, node_id, device_id: undefined })} className="w-full" />
    <WorkspaceSelect label={`${t("workspace.metric")} ${props.series.ref_id}`} value={props.series.metric} options={props.metrics.filter((metric) => metric.enabled).map((metric) => ({ value: metric.key, label: metric.label }))} onChange={metricChanged} className="w-full" />
    <WorkspaceSelect label={`${t("workspace.aggregation")} ${props.series.ref_id}`} value={props.series.aggregation} options={(selected?.aggregations ?? []).map((value) => ({ value, label: t(`workspace.${value === "min" || value === "max" ? `${value}_agg` : value}`) }))}
      onChange={(aggregation) => props.onChange({ ...props.series, aggregation: aggregation as MetricAggregation })} className="w-full" />
  </div>{isAcceleratorMetric(props.series.metric) && <AcceleratorDeviceField series={props.series} change={device_id => props.onChange({ ...props.series, device_id })} />}</div>
}
