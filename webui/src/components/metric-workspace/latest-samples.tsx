import { useEffect, useState } from "react"
import { useLocale } from "@/context/locale"
import { dateTimeFormatter } from "@/lib/intl-format"
import { queryWorkspaceLatest } from "@/lib/metric-workspace-api"
import { formatWorkspaceValue } from "@/lib/workspace-format"
import type { MetricLatestQuerySeries, MetricLatestResultSeries, MetricResultSeries } from "@/types/metric-workspace"

interface LatestState {
  status: "loading" | "ok" | "error"
  series: MetricLatestResultSeries[]
}

/** 设备序列按设备分桶，服务端即时查询只覆盖节点级指标，因此只请求不带设备的序列。 */
export function latestSampleRequests(series: MetricResultSeries[]): MetricLatestQuerySeries[] {
  const seen = new Set<string>()
  return series.flatMap((entry) => {
    if (entry.device_id) return []
    const key = `${entry.node_id}\u0000${entry.metric}`
    if (seen.has(key)) return []
    seen.add(key)
    return [{ ref_id: entry.ref_id, node_id: entry.node_id, metric: entry.metric }]
  })
}

/**
 * 面板结果是分桶统计，最新桶可能是区间平均。这里补一次即时查询，让检查器里的
 * 「最新样本」始终是原始样本；查询失败只降级成提示，不影响既有数据表。
 */
export function WorkspaceLatestSamples(props: { series: MetricResultSeries[]; locale: string; publicWorkspace?: boolean }) {
  const { t } = useLocale()
  const requests = latestSampleRequests(props.series)
  const key = JSON.stringify(requests)
  const [state, setState] = useState<LatestState>({ status: "loading", series: [] })
  useEffect(() => {
    if (!requests.length) {
      setState({ status: "ok", series: [] })
      return
    }
    const controller = new AbortController()
    setState({ status: "loading", series: [] })
    queryWorkspaceLatest({ series: requests }, { signal: controller.signal, public: props.publicWorkspace })
      .then((result) => setState({ status: "ok", series: result.series }))
      .catch(() => { if (!controller.signal.aborted) setState({ status: "error", series: [] }) })
    return () => controller.abort()
    // requests 由 key 代表，避免每帧新建数组导致的重复请求。
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [key, props.publicWorkspace])
  if (state.status === "error") return <p className="text-xs text-muted-foreground">{t("workspace.latest_failed")}</p>
  if (!requests.length || !state.series.length) return null
  const format = dateTimeFormatter(props.locale, { year: "numeric", month: "numeric", day: "numeric", hour: "2-digit", minute: "2-digit", second: "2-digit", hour12: false })
  return <section aria-label={t("workspace.latest_samples")} className="space-y-1">
    <p className="text-xs font-medium text-muted-foreground">{t("workspace.latest_samples")} · {t("workspace.latest_hint")}</p>
    <ul className="flex flex-wrap gap-x-4 gap-y-1 text-xs">
      {state.series.map((entry) => <li key={`${entry.ref_id}-${entry.node_id}-${entry.metric}`} className="font-mono tabular-nums">
        <span className="text-muted-foreground">{entry.label}</span>{" "}
        <span>{entry.state === "ok" && entry.value !== undefined ? formatWorkspaceValue(entry.value, entry.unit, { locale: props.locale }) : t("workspace.latest_empty")}</span>
        {entry.sample_at && <span className="text-muted-foreground"> · {format.format(new Date(entry.sample_at))}</span>}
      </li>)}
    </ul>
  </section>
}
