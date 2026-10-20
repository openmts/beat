import { EyeIcon, EyeOffIcon, PlusIcon, XIcon } from "lucide-react"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { WorkspaceSelect } from "@/components/metric-workspace/select"
import { useLocale } from "@/context/locale"
import { exploreAddQuery, exploreAggregations, exploreColors, exploreMetric, exploreMetrics, exploreNode, exploreQueryErrorKey, exploreRemoveQuery, maximumExploreQueries, type ExploreQuery, type ExploreState } from "@/lib/metric-explore"
import type { Node } from "@/types"
import type { MetricAggregation, MetricCatalog } from "@/types/metric-workspace"

/** Grafana Explore 的查询编辑器：每个查询行独立选择指标、聚合、节点、设备与颜色。 */
export function ExploreQueryEditor(props: {
  state: ExploreState; catalog: MetricCatalog; nodes: Node[]; onChange: (state: ExploreState) => void
}) {
  const { t } = useLocale()
  const replace = (index: number, patch: Partial<ExploreQuery>) => props.onChange({
    ...props.state, queries: props.state.queries.map((query, current) => current === index ? { ...query, ...patch } : query),
  })
  return <fieldset className="space-y-3">
    <legend className="text-sm font-medium">{t("explore.queries")}</legend>
    {props.state.queries.map((query, index) => <ExploreQueryRow key={query.ref_id} query={query} index={index} catalog={props.catalog} nodes={props.nodes}
      replace={(patch) => replace(index, patch)} remove={() => props.onChange(exploreRemoveQuery(props.state, index))} />)}
    <Button variant="outline" size="sm" disabled={props.state.queries.length >= maximumExploreQueries}
      onClick={() => props.onChange(exploreAddQuery(props.state, props.catalog, props.nodes))}><PlusIcon />{t("explore.add_query")}</Button>
  </fieldset>
}

function ExploreQueryRow(props: {
  query: ExploreQuery; index: number; catalog: MetricCatalog; nodes: Node[]
  replace: (patch: Partial<ExploreQuery>) => void; remove: () => void
}) {
  const { t } = useLocale()
  const query = props.query
  const definition = exploreMetric(props.catalog, query.metric)
  const node = exploreNode(props.nodes, query.node_id)
  const error = exploreQueryErrorKey(query, props.catalog, props.nodes)
  const aggregations = definition?.aggregations ?? [...exploreAggregations]
  return <div className="space-y-2 rounded-md border p-3">
    <div className="flex items-center justify-between gap-2">
      <span className="rounded bg-muted px-2 py-0.5 font-mono text-xs" aria-label={t("explore.ref_id")}>{query.ref_id}</span>
      <div className="flex items-center gap-1">
        <Button variant="ghost" size="icon-sm" aria-label={query.hidden ? t("explore.show_query") : t("explore.hide_query")}
          onClick={() => props.replace({ hidden: !query.hidden })}>{query.hidden ? <EyeOffIcon /> : <EyeIcon />}</Button>
        <Button variant="ghost" size="icon-sm" aria-label={`${t("explore.remove_query")} ${query.ref_id}`} onClick={props.remove}><XIcon /></Button>
      </div>
    </div>
    <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-4">
      <WorkspaceSelect label={t("workspace.metric")} value={query.metric} options={exploreMetrics(props.catalog).map((metric) => ({ value: metric.key, label: metric.label }))}
        onChange={(metric) => props.replace({ metric, aggregation: exploreMetric(props.catalog, metric)?.default_aggregation ?? query.aggregation })} />
      <WorkspaceSelect label={t("explore.aggregation")} value={query.aggregation} options={aggregations.map((value) => ({ value, label: t(`explore.aggregation_${value}`) }))}
        onChange={(aggregation) => props.replace({ aggregation: aggregation as MetricAggregation })} />
      <WorkspaceSelect label={t("workspace.node")} value={query.node_id} options={props.nodes.map((value) => ({ value: value.id, label: value.alias || value.name }))}
        onChange={(node_id) => props.replace({ node_id, device_id: "" })} />
      <WorkspaceSelect label={t("workspace.color")} value={query.color} options={exploreColors.map((value) => ({ value, label: t(`workspace.${value}`) }))}
        onChange={(color) => props.replace({ color: color as ExploreQuery["color"] })} />
    </div>
    <div className="space-y-2">
      <Label htmlFor={`explore-device-${query.ref_id}`}>{t("explore.device")}</Label>
      {node?.accelerators?.length
        ? <WorkspaceSelect label={t("explore.device")} value={query.device_id} options={[{ value: "", label: t("explore.all_devices") },
            ...node.accelerators.map((device) => ({ value: device.device_id, label: `${device.device_id} · ${device.model || device.vendor}` }))]}
            onChange={(device_id) => props.replace({ device_id })} />
        : <Input id={`explore-device-${query.ref_id}`} value={query.device_id} placeholder={t("explore.device_hint")} onChange={(event) => props.replace({ device_id: event.target.value })} />}
    </div>
    {error && <p role="alert" className="text-sm text-destructive">{t(error)}</p>}
  </div>
}
