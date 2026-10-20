import { useEffect, useState } from "react"
import { LayoutDashboardIcon } from "lucide-react"
import { Button } from "@/components/ui/button"
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { Skeleton } from "@/components/ui/skeleton"
import { useLocale } from "@/context/locale"
import { appendExplorePanels, buildExplorePanels } from "@/lib/explore-add-panel"
import type { ExploreState } from "@/lib/metric-explore"
import { listMetricDashboards, saveMetricDashboard } from "@/lib/metric-workspace-api"
import type { MetricDashboard } from "@/types/metric-workspace"

/**
 * Grafana 的 Explore → Add to dashboard：把当前启用的查询行追加成时序面板。
 * 面板继承目标看板的时间范围与变量，Explore 的临时范围不会被写进看板。
 */
export function ExploreAddToDashboardDialog(props: { state: ExploreState; close: () => void; done: (dashboard: MetricDashboard) => void; fail: (key: string) => void }) {
  const { t } = useLocale()
  const [dashboards, setDashboards] = useState<MetricDashboard[] | null>(null)
  const [saving, setSaving] = useState("")
  const [loadFailed, setLoadFailed] = useState(false)
  useEffect(() => {
    let active = true
    listMetricDashboards().then((value) => { if (active) setDashboards(value) }).catch(() => { if (active) setLoadFailed(true) })
    return () => { active = false }
  }, [])
  const drafts = buildExplorePanels(props.state, t("explore.add_panel_title"))
  async function add(dashboard: MetricDashboard) {
    const definition = appendExplorePanels(dashboard.definition, drafts)
    if (!definition) { props.fail("explore.add_panel_limit"); return }
    setSaving(dashboard.id)
    try { props.done(await saveMetricDashboard({ ...dashboard, definition })) }
    catch { setSaving(""); props.fail("explore.add_panel_failed") }
  }
  return <Dialog open onOpenChange={(open) => { if (!open) props.close() }}>
    <DialogContent className="max-h-[90svh] overflow-y-auto sm:max-w-xl"><DialogHeader>
      <DialogTitle>{t("explore.add_to_dashboard")}</DialogTitle>
      <DialogDescription>{t("explore.add_panel_hint", { panels: String(drafts.length), series: String(props.state.queries.filter((query) => !query.hidden).length) })}</DialogDescription>
    </DialogHeader>
    {loadFailed && <p role="alert" className="text-sm text-destructive">{t("explore.add_dashboard_load_failed")}</p>}
    {dashboards === null && !loadFailed && <Skeleton className="h-24" />}
    {dashboards !== null && dashboards.length === 0 && <p className="text-sm text-muted-foreground">{t("explore.add_panel_empty")}</p>}
    <div className="grid gap-2">{dashboards?.map((dashboard) => <Button key={dashboard.id} variant="outline" className="justify-start" disabled={saving !== ""}
      onClick={() => void add(dashboard)}><LayoutDashboardIcon />{dashboard.definition.title}</Button>)}</div>
    </DialogContent>
  </Dialog>
}
