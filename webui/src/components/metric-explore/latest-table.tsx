import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table"
import { useLocale } from "@/context/locale"
import { dateTimeFormatter } from "@/lib/intl-format"
import { formatWorkspaceValue } from "@/lib/workspace-format"
import type { ExpandedSeries, MetricLatestResult, MetricLatestResultSeries } from "@/types/metric-workspace"

/**
 * 即时查询结果：每条序列只有一个当前值，因此渲染成表格而不是时间序列。
 * 标签与看板、区间探索共用同一套展开信息，节点名与设备维度保持一致。
 */
export function ExploreLatestTable(props: { latest: MetricLatestResult; expanded?: ExpandedSeries[] }) {
  const { t, locale } = useLocale()
  const format = dateTimeFormatter(locale, { year: "numeric", month: "numeric", day: "numeric", hour: "2-digit", minute: "2-digit", second: "2-digit", hour12: false })
  const labels = new Map((props.expanded ?? []).map((series) => [`${series.ref_id}\u0000${series.node_id}\u0000${series.metric}`, series.label]))
  const label = (series: MetricLatestResultSeries) => labels.get(`${series.ref_id}\u0000${series.node_id}\u0000${series.metric}`) ?? (series.label || series.metric)
  return <div role="region" aria-label={t("explore.latest_title")} className="overflow-x-auto rounded-md border">
    <Table><TableHeader><TableRow><TableHead>{t("explore.latest_series")}</TableHead><TableHead>{t("explore.latest_value")}</TableHead><TableHead>{t("explore.latest_time")}</TableHead></TableRow></TableHeader>
      <TableBody>{props.latest.series.map((series) => <TableRow key={`${series.ref_id}-${series.node_id}-${series.metric}`}>
        <TableCell className="max-w-80"><span className="rounded bg-muted px-1.5 py-0.5 font-mono text-xs">{series.ref_id}</span> <span className="text-sm">{label(series)}</span></TableCell>
        <TableCell className="font-mono text-sm tabular-nums">{series.state === "ok" && series.value !== undefined ? formatWorkspaceValue(series.value, series.unit, { locale }) : t("workspace.latest_empty")}</TableCell>
        <TableCell className="whitespace-nowrap font-mono text-xs tabular-nums">{series.sample_at ? format.format(new Date(series.sample_at)) : "—"}</TableCell>
      </TableRow>)}</TableBody></Table>
  </div>
}
