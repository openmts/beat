import { useState } from "react"
import { Link } from "react-router"
import { Button, buttonVariants } from "@/components/ui/button"
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { useLocale } from "@/context/locale"
import { downloadWorkspaceFile, workspaceCSV, workspaceJSON } from "@/lib/workspace-export"
import { WorkspaceDataTable } from "./data-view"
import { WorkspaceLatestSamples } from "./latest-samples"
import { WorkspaceStatsTable } from "./stats-view"
import type { DashboardPanel, PanelQueryState } from "@/types/metric-workspace"

export function WorkspaceInspector(props: { panel: DashboardPanel; state: PanelQueryState; close: () => void; focusNode?: (id: string) => void; publicLinks?: boolean }) {
  const { t, locale } = useLocale()
  const [error, setError] = useState(false)
  const [copied, setCopied] = useState(false)
  const result = props.state.result
  const query = props.state.resultQuery ?? props.state.query
  const panelJSON = JSON.stringify(props.panel, null, 2)
  async function copyPanelJSON() {
    try { await navigator.clipboard.writeText(panelJSON); setCopied(true) }
    catch { setError(true) }
  }
  function download(type: "csv" | "json") {
    if (!result || !query) return
    try { downloadWorkspaceFile(props.panel.title, type === "csv" ? workspaceCSV(result, props.state.resultExpanded) : workspaceJSON(query, result), type) }
    catch { setError(true) }
  }
  return <Dialog open onOpenChange={(open) => { if (!open) props.close() }}>
    <DialogContent className="sm:max-w-4xl"><DialogHeader><DialogTitle>{props.panel.title} · {t("workspace.inspect")}</DialogTitle>
      <DialogDescription>{result ? t("workspace.response_summary", { series: result.series.length, points: result.series.reduce((total, series) => total + series.points.length, 0), interval: result.interval_ms / 1000 }) : t("workspace.no_data")}</DialogDescription></DialogHeader>
      {(props.state.loading || props.state.error) && result && <p className="text-xs text-muted-foreground">{t("workspace.last_good_range")}</p>}
      <Tabs defaultValue="data"><TabsList><TabsTrigger value="data">{t("workspace.data")}</TabsTrigger><TabsTrigger value="stats">{t("workspace.stats")}</TabsTrigger><TabsTrigger value="query">{t("workspace.query")}</TabsTrigger><TabsTrigger value="json">{t("workspace.panel_json")}</TabsTrigger></TabsList>
        <TabsContent value="query"><pre className="max-h-80 overflow-auto rounded-md bg-muted p-4 text-xs">{JSON.stringify(query ?? props.state.query, null, 2)}</pre></TabsContent>
        <TabsContent value="json" className="space-y-3">
          <div className="flex items-center justify-between gap-3">
            <p className="text-xs text-muted-foreground">{t("workspace.panel_json_hint")}</p>
            <Button variant="outline" size="sm" onClick={() => { void copyPanelJSON() }}>{copied ? t("workspace.json_copied") : t("workspace.copy_json")}</Button>
          </div>
          <pre data-workspace-panel-json className="max-h-80 overflow-auto rounded-md bg-muted p-4 text-xs">{panelJSON}</pre>
        </TabsContent>
        <TabsContent value="stats">{result ? <WorkspaceStatsTable result={result} expanded={props.state.resultExpanded} title={props.panel.title} panel={props.panel} /> : <p className="py-6 text-sm text-muted-foreground">{t("workspace.no_data")}</p>}</TabsContent>
        <TabsContent value="data" className="space-y-3">{result && <WorkspaceLatestSamples series={result.series} locale={locale} publicWorkspace={props.publicLinks} />}
          {result ? <WorkspaceDataTable result={result} expanded={props.state.resultExpanded} /> : <p className="py-6 text-sm text-muted-foreground">{t("workspace.no_data")}</p>}</TabsContent>
      </Tabs>
      {error && <p role="alert" className="text-sm text-destructive">{t("workspace.export_failed")}</p>}
      <div className="flex flex-wrap justify-between gap-3">
        <div className="flex flex-wrap gap-2">{[...new Set(result?.series.map((series) => series.node_id) ?? [])].map((id) => props.publicLinks ?
          <Link key={id} to={`/node/${encodeURIComponent(id)}`} onClick={props.close} className={buttonVariants({ variant: "outline", size: "sm" })}>{t("workspace.node_drilldown")} · {props.state.resultExpanded?.find((series) => series.node_id === id)?.node_name ?? id}</Link> :
          <Button key={id} variant="outline" size="sm" onClick={() => props.focusNode?.(id)}>{t("workspace.node_drilldown")}</Button>)}</div>
        <div className="flex gap-2"><Button variant="outline" disabled={!result} onClick={() => download("csv")}>{t("workspace.export_csv")}</Button><Button variant="outline" disabled={!result} onClick={() => download("json")}>{t("workspace.export_json")}</Button></div>
      </div>
    </DialogContent>
  </Dialog>
}
