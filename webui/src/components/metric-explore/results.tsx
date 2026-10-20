import { AlertTriangleIcon, RefreshCwIcon } from "lucide-react"
import { Button } from "@/components/ui/button"
import { Skeleton } from "@/components/ui/skeleton"
import { WorkspaceDataTable, WorkspaceDataView } from "@/components/metric-workspace/data-view"
import { ExploreLatestTable } from "./latest-table"
import { useLocale } from "@/context/locale"
import type { ExploreRun } from "@/hooks/use-explore-run"
import type { DashboardPanel, WorkspaceRange } from "@/types/metric-workspace"

/** 探索结果：图与表复用看板同一套渲染，因此取值、单位、缩放与看板保持一致。 */
export function ExploreResults(props: {
  run: ExploreRun; panel: DashboardPanel; view: "chart" | "table"; onRange: (range: WorkspaceRange) => void; onRetry: () => void
}) {
  const { t } = useLocale()
  if (props.run.loading && !props.run.result) return <div className="space-y-2" aria-busy="true"><Skeleton className="h-64 w-full" /><span className="sr-only">{t("explore.running")}</span></div>
  if (props.run.error) return <div role="alert" className="flex flex-wrap items-center justify-between gap-3 rounded-md border border-destructive/30 bg-destructive/5 p-3 text-sm">
    <span className="flex items-center gap-2"><AlertTriangleIcon className="size-4" />{t(props.run.error)}</span>
    <Button variant="outline" size="sm" onClick={props.onRetry}><RefreshCwIcon />{t("workspace.refresh")}</Button>
  </div>
  if (props.run.latest) return <div className={props.run.loading ? "opacity-60 transition-opacity" : undefined}><ExploreLatestTable latest={props.run.latest} expanded={props.run.expanded} /></div>
  if (!props.run.result) return <p className="rounded-md border border-dashed p-6 text-center text-sm text-muted-foreground">{t("explore.idle")}</p>
  return <div className={props.run.loading ? "opacity-60 transition-opacity" : undefined}>
    {props.view === "table"
      ? <WorkspaceDataTable result={props.run.result} expanded={props.run.expanded} />
      : <WorkspaceDataView panel={props.panel} result={props.run.result} expanded={props.run.expanded} onRange={props.onRange} height={320} />}
  </div>
}
