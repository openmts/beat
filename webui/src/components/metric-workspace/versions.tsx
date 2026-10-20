import { useEffect, useState } from "react"
import { Button } from "@/components/ui/button"
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { useLocale } from "@/context/locale"
import { listDashboardVersions } from "@/lib/metric-workspace-api"
import type { MetricDashboardVersion } from "@/types/metric-workspace"

// 服务端按字节限制备注（200 B），中文备注按字符计数会超限，这里按 UTF-8 字节截断。
const maxSummaryBytes = 200
function clampSummary(value: string): string {
  const encoder = new TextEncoder()
  let result = value
  while (result.length > 0 && encoder.encode(result).length > maxSummaryBytes) result = result.slice(0, -1)
  return result
}

/**
 * 看板版本历史：列出最近 MaxDashboardVersions 个版本，可把任意历史版本恢复成新版本。
 * 恢复动作本身也产生一条记录，因此「恢复」之后仍能退回去。
 */
export function WorkspaceVersions(props: {
  dashboardId: string
  currentRevision: number
  refreshKey: number
  restore: (revision: number, summary: string) => void
  close: () => void
  onError: (value: string | null) => void
}) {
  const { t } = useLocale()
  const [values, setValues] = useState<MetricDashboardVersion[]>([])
  const [summary, setSummary] = useState("")
  useEffect(() => {
    listDashboardVersions(props.dashboardId).then(setValues).catch(() => props.onError("workspace.load_failed"))
    // oxlint-disable-next-line react-hooks/exhaustive-deps -- 版本列表按看板与修订号拉取一次，文案与回调无需进入依赖
  }, [props.dashboardId, props.refreshKey])
  return <Dialog open onOpenChange={(open) => { if (!open) props.close() }}><DialogContent className="sm:max-w-2xl">
    <DialogHeader><DialogTitle>{t("workspace.version_history")}</DialogTitle>
      <DialogDescription>{t("workspace.version_history_description")}</DialogDescription></DialogHeader>
    {values.length === 0 ? <p className="text-sm text-muted-foreground">{t("workspace.version_empty")}</p> :
      <ol className="max-h-80 space-y-2 overflow-y-auto">{values.map((value) => <li key={value.revision}
        className="flex items-center justify-between gap-3 rounded-md border px-3 py-2 text-sm">
        <span className="min-w-0">
          <span className="font-medium">{t("workspace.version_number", { revision: String(value.revision) })}</span>
          {value.revision === props.currentRevision && <span className="ml-2 text-xs text-muted-foreground">{t("workspace.version_current")}</span>}
          <time className="block text-xs text-muted-foreground" dateTime={value.created_at}>{value.created_at.slice(0, 16).replace("T", " ")}{value.created_by ? ` · ${value.created_by}` : ""}{value.summary ? ` · ${value.summary}` : ""}</time>
        </span>
        <Button variant="outline" size="sm" disabled={value.revision === props.currentRevision}
          onClick={() => props.restore(value.revision, summary)}>{t("workspace.version_restore")}</Button>
      </li>)}</ol>}
    <div className="grid gap-2"><Label htmlFor="workspace-version-summary">{t("workspace.version_summary")}</Label>
      <Input id="workspace-version-summary" value={summary} placeholder={t("workspace.version_summary_placeholder")}
        onChange={(event) => setSummary(clampSummary(event.target.value))} /></div>
    <DialogFooter><Button variant="outline" onClick={props.close}>{t("workspace.cancel")}</Button></DialogFooter>
  </DialogContent></Dialog>
}
