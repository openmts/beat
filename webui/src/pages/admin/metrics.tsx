import { Link } from "react-router"
import { CompassIcon, GaugeIcon } from "lucide-react"
import { Button } from "@/components/ui/button"
import { Skeleton } from "@/components/ui/skeleton"
import { MetricWorkspaceSurface } from "@/components/metric-workspace/workspace"
import { WorkspaceCatalog } from "@/components/metric-workspace/catalog"
import { registerLocaleCatalogs, useLocale } from "@/context/locale"
import "@/context/metric-workspace-catalogs"
import { metricExploreTranslations } from "@/context/metric-explore-translations"
import { useWorkspaceSources } from "@/hooks/use-workspace-sources"

// 探索页的文案目录随工作区分片注册：入口按钮与探索页共用同一份文案。
registerLocaleCatalogs([metricExploreTranslations])

export default function MetricsWorkspace() {
  const { t } = useLocale()
  const state = useWorkspaceSources()
  const [catalog, setCatalog] = useState(false)
  return <div className="min-w-0 space-y-6">
    <div className="flex flex-wrap items-start justify-between gap-4"><div><h1 className="font-heading text-2xl font-semibold tracking-tight">{t("workspace.title")}</h1><p className="mt-1 text-sm text-muted-foreground">{t("workspace.description")}</p></div>
      <div className="flex flex-wrap gap-2"><Button variant="outline" size="sm" render={<Link to="/admin/metrics/explore" />}><CompassIcon />{t("explore.title")}</Button><Button variant="outline" size="sm" disabled={!state.sources} onClick={() => setCatalog(true)}>{t("workspace.catalog")}</Button><Button variant="outline" size="sm" render={<Link to="/admin/custom-metrics" />}><GaugeIcon />{t("custom_metric.title")}</Button></div>
    </div>
    {state.error && <div role="alert" className="flex items-center justify-between gap-3 rounded-md border border-destructive/30 bg-destructive/5 p-3 text-sm"><span>{t(state.error)}</span><Button variant="outline" size="sm" onClick={state.reload}>{t("workspace.retry")}</Button></div>}
    {state.sources ? <MetricWorkspaceSurface sources={state.sources} /> : state.loading ? <div className="grid gap-4 lg:grid-cols-2"><Skeleton className="h-72" /><Skeleton className="h-72" /></div> : null}
    {catalog && state.sources && <WorkspaceCatalog catalog={state.sources.catalog} close={() => setCatalog(false)} changed={state.reload} />}
  </div>
}
import { useState } from "react"
