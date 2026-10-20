import { useEffect, useState } from "react"
import { Link, useNavigate, useSearchParams } from "react-router"
import { ChartLineIcon, CopyIcon, HistoryIcon, LayoutDashboardIcon, LinkIcon, PlayIcon, TableIcon } from "lucide-react"
import { Button } from "@/components/ui/button"
import { Skeleton } from "@/components/ui/skeleton"
import { ExploreQueryEditor } from "@/components/metric-explore/query-editor"
import { ExploreAddToDashboardDialog } from "@/components/metric-explore/add-to-dashboard"
import { ExploreHistoryDialog } from "@/components/metric-explore/history"
import { ExploreResults } from "@/components/metric-explore/results"
import { WorkspaceSelect } from "@/components/metric-workspace/select"
import { WorkspaceTimeControls } from "@/components/metric-workspace/time-controls"
import { registerLocaleCatalogs, useLocale } from "@/context/locale"
import "@/context/metric-workspace-catalogs"
import { metricExploreTranslations } from "@/context/metric-explore-translations"
import { useExploreRun } from "@/hooks/use-explore-run"
import { useWorkspaceSources } from "@/hooks/use-workspace-sources"
import { formatDateTime } from "@/lib/intl-format"
import { clearExploreHistory, exploreHistoryStorage, loadExploreHistory, recordExploreHistory, removeExploreHistory, saveExploreHistory, toggleExploreHistoryStar, type ExploreHistoryEntry } from "@/lib/explore-history"
import { emptyMetricCatalog, exploreDefaultState, exploreInstantSupported, exploreLatestRequest, explorePanel, explorePointOptions, exploreQueryErrorKey, exploreRequest, exploreRunnable, exploreSearch, parseExploreState, type ExploreQueryType, type ExploreState } from "@/lib/metric-explore"
import type { MetricLatestQuery, MetricQuery } from "@/types/metric-workspace"

registerLocaleCatalogs([metricExploreTranslations])

/** 指标探索：Grafana Explore 等价物——不落库的临时查询，查询与范围都编码进 URL 便于分享。 */
export default function MetricsExplore() {
  const { locale, t } = useLocale()
  const sources = useWorkspaceSources()
  const navigate = useNavigate()
  const [params, setParams] = useSearchParams()
  const [state, setState] = useState<ExploreState | null>(null)
  const [refreshSeconds, setRefreshSeconds] = useState(0)
  const [view, setView] = useState<"chart" | "table">("chart")
  const [status, setStatus] = useState("")
  const [history, setHistory] = useState<ExploreHistoryEntry[]>([])
  const [historyOpen, setHistoryOpen] = useState(false)
  const [addOpen, setAddOpen] = useState(false)
  const catalog = sources.sources?.catalog ?? emptyMetricCatalog
  const nodes = sources.sources?.nodes ?? []
  const active = state ?? exploreDefaultState(emptyMetricCatalog, [])
  const execution = useExploreRun({ state: active, nodes, catalog, ready: Boolean(state), refreshSeconds })

  // URL 只在数据就绪后解析一次：目录与节点列表都要参与校验，避免把非法行渲染出来。
  useEffect(() => {
    if (!state && sources.sources) setState(parseExploreState(params.toString(), sources.sources.catalog, sources.sources.nodes))
  }, [state, sources.sources, params])
  useEffect(() => {
    if (state) setParams(new URLSearchParams(exploreSearch(state)), { replace: true })
  }, [state, setParams])
  useEffect(() => { setHistory(loadExploreHistory(exploreHistoryStorage())) }, [])

  // 隐藏的查询行不参与执行，也不该阻塞其余行；行内校验提示仍然照常显示。
  const errors = active.queries.filter((query) => !query.hidden).map((query) => exploreQueryErrorKey(query, catalog, nodes)).filter((value): value is string => value !== null)
  // 即时查询无法按设备寻址，带设备的行先提示再禁用执行，而不是静默丢掉维度。
  const instant = active.query_type === "instant"
  const instantBlocked = instant && !exploreInstantSupported(active)
  const blocked = errors.length > 0 || exploreRunnable(active).length === 0 || instantBlocked
  async function copy(value: string) {
    try { await navigator.clipboard.writeText(value); setStatus(t("explore.copied")) }
    catch { setStatus(t("explore.copy_failed")) }
  }
  const requestJSON = JSON.stringify(execution.run.latestQuery ?? execution.run.query ?? copyableRequest(active), null, 2)
  // 历史只在用户主动执行或恢复时记录：自动刷新与回到前台补齐不产生新条目。
  function persistHistory(next: ExploreHistoryEntry[]) {
    setHistory(next)
    saveExploreHistory(exploreHistoryStorage(), next)
  }
  function runQuery() {
    persistHistory(recordExploreHistory(history, active, new Date()))
    execution.refresh()
  }
  function restoreHistory(entry: ExploreHistoryEntry) {
    const restored = parseExploreState(entry.search, catalog, nodes)
    persistHistory(recordExploreHistory(history, restored, new Date()))
    setState(restored)
    execution.refresh()
  }
  return <div className="min-w-0 space-y-6">
    <div className="flex flex-wrap items-start justify-between gap-4">
      <div><h1 className="font-heading text-2xl font-semibold tracking-tight">{t("explore.title")}</h1>
        <p className="mt-1 max-w-3xl text-sm text-muted-foreground">{t("explore.description")}</p></div>
      <Button variant="outline" size="sm" render={<Link to="/admin/metrics" />}><ChartLineIcon />{t("explore.workspace_link")}</Button>
    </div>
    {sources.error && <div role="alert" className="flex items-center justify-between gap-3 rounded-md border border-destructive/30 bg-destructive/5 p-3 text-sm">
      <span>{t(sources.error)}</span><Button variant="outline" size="sm" onClick={sources.reload}>{t("workspace.retry")}</Button></div>}
    {!state ? sources.loading ? <Skeleton className="h-64 w-full" /> : null : <div className="space-y-4">
      <div className="flex flex-wrap items-center gap-2">
        <WorkspaceTimeControls range={active.range} refreshSeconds={refreshSeconds} onRange={(range) => setState({ ...active, range })}
          onInterval={setRefreshSeconds} onRefresh={execution.refresh} />
        <WorkspaceSelect label={t("explore.query_type")} value={active.query_type}
          options={[{ value: "range", label: t("explore.query_type_range") }, { value: "instant", label: t("explore.query_type_instant") }]}
          onChange={(value) => setState({ ...active, query_type: value as ExploreQueryType })} />
        {!instant && <WorkspaceSelect label={t("explore.max_points")} value={String(active.max_points)}
          options={explorePointOptions.map((value) => ({ value: String(value), label: String(value) }))}
          onChange={(value) => setState({ ...active, max_points: Number(value) })} />}
        <Button size="sm" disabled={blocked || execution.run.loading} onClick={runQuery}><PlayIcon />{t("explore.run")}</Button>
        <Button variant="outline" size="sm" onClick={() => void copy(requestJSON)}><CopyIcon />{t("explore.copy_json")}</Button>
        <Button variant="outline" size="sm" onClick={() => void copy(shareLink(active))}><LinkIcon />{t("explore.copy_link")}</Button>
        <Button variant="outline" size="sm" onClick={() => setHistoryOpen(true)}><HistoryIcon />{t("explore.history")}</Button>
        <Button variant="outline" size="sm" disabled={blocked} onClick={() => setAddOpen(true)}><LayoutDashboardIcon />{t("explore.add_to_dashboard")}</Button>
        <div role="group" aria-label={t("explore.json_title")} className="flex items-center gap-1">
          <Button variant={view === "chart" ? "default" : "outline"} size="sm" onClick={() => setView("chart")}><ChartLineIcon />{t("explore.view_chart")}</Button>
          <Button variant={view === "table" ? "default" : "outline"} size="sm" onClick={() => setView("table")}><TableIcon />{t("explore.view_table")}</Button>
        </div>
      </div>
      {status && <p role="status" className="text-sm text-muted-foreground">{status}</p>}
      {instant && <p className="text-xs text-muted-foreground">{t("explore.instant_hint")}</p>}
      {instantBlocked && <p role="alert" className="text-sm text-destructive">{t("explore.instant_device_unsupported")}</p>}
      {active.queries.length === 0 && <p className="rounded-md border border-dashed p-4 text-center text-sm text-muted-foreground">{t("explore.no_queries")}</p>}
      <ExploreQueryEditor state={active} catalog={catalog} nodes={nodes} onChange={setState} />
      {!blocked && execution.run.updatedAt && <p className="text-xs text-muted-foreground">{t("explore.updated", { time: formatDateTime(locale, new Date(execution.run.updatedAt)) })} · {t("explore.share_hint")}</p>}
      <ExploreResults run={execution.run} panel={explorePanel(t)} view={view} onRange={(range) => setState({ ...active, range })} onRetry={execution.refresh} />
      {addOpen && <ExploreAddToDashboardDialog state={active} close={() => setAddOpen(false)}
        done={(dashboard) => { setAddOpen(false); setStatus(t("explore.add_panel_done", { title: dashboard.definition.title })); void navigate(`/admin/metrics?dashboard=${encodeURIComponent(dashboard.id)}`) }}
        fail={(key) => { setAddOpen(false); setStatus(t(key)) }} />}
      {historyOpen && <ExploreHistoryDialog entries={history} close={() => setHistoryOpen(false)}
        onRestore={restoreHistory}
        onToggleStar={(entry, starred) => persistHistory(toggleExploreHistoryStar(history, entry.search, starred))}
        onDelete={(searches) => persistHistory(removeExploreHistory(history, searches))}
        onClear={() => persistHistory(clearExploreHistory(history))} />}
    </div>}
  </div>
}

/** 复制 JSON 时即使查询还不可执行也要给出可读的载荷，因此失败时回落到占位请求。 */
function copyableRequest(state: ExploreState): MetricQuery | MetricLatestQuery | Record<string, never> {
  try { return state.query_type === "instant" ? exploreLatestRequest(state) : exploreRequest(state) } catch { return {} }
}

function shareLink(state: ExploreState): string {
  const origin = typeof window === "undefined" ? "" : window.location.origin
  return `${origin}/admin/metrics/explore?${exploreSearch(state)}`
}
