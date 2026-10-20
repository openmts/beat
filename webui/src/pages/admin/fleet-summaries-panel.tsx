import { useCallback, useEffect, useState } from "react"
import { SendIcon } from "lucide-react"
import { Button } from "@/components/ui/button"
import { Skeleton } from "@/components/ui/skeleton"
import { Alert, AlertDescription } from "@/components/ui/alert"
import { Badge } from "@/components/ui/badge"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table"
import { useLocale } from "@/context/locale"
import { formatDateTime } from "@/lib/intl-format"
import { useAlertChannels, useGroups, useNodes } from "@/hooks/use-api"
import { useConfirmedOperation } from "@/hooks/use-confirmed-operation"
import { APIError } from "@/lib/api-error"
import { listFleetSummaries, deleteFleetSummary, testFleetSummary } from "@/lib/fleet-summary-api"
import type { FleetSummarySchedule } from "@/types/fleet-summary"
import { OperationConfirm } from "./operation-confirm"
import { FleetSummaryEditor } from "./fleet-summary-editor"

type ScopeOption = { id: string; name: string }
type Translate = (key: string, params?: Record<string, string | number>) => string

/** 计划作用域只渲染显示名；未知或已删除的引用不暴露原始 ID。 */
function scopeSummary(ids: string[], options: ScopeOption[], t: Translate): string {
  const names = ids.map(id => options.find(option => option.id === id)?.name).filter((name): name is string => Boolean(name))
  if (names.length > 0) return names.join(", ")
  return ids.length > 0 ? t("fleet.summary.removed") : t("fleet.all")
}

function scheduleLabel(row: FleetSummarySchedule, t: Translate): string {
  const cadence = row.cadence === "weekly" ? t("fleet.summary.weekly") : t("fleet.summary.daily")
  const weekday = row.cadence === "weekly" && row.weekday !== null ? t("fleet.summary.day_value", { day: row.weekday }) : ""
  return [cadence, weekday, row.local_time, row.timezone].filter(Boolean).join(" · ")
}

function deliveryLabel(state: string | null, t: Translate): string {
  switch (state) {
    case "queued": return t("fleet.summary.delivering")
    case "succeeded": return t("fleet.summary.delivered")
    case "partial": return t("fleet.summary.partial")
    case "failed": return t("fleet.summary.failed")
    case "unknown": return t("fleet.summary.unknown")
    case "": return "—"
    default: return state ?? "—"
  }
}

function nextRunLabel(value: string | null, locale: string): string {
  if (!value) return "—"
  return formatDateTime(locale, value)
}

export default function FleetSummariesPanel({ onHistory }: { onHistory: () => void }) {
  const { t, locale } = useLocale()
  const flow = useConfirmedOperation()
  const nodes = useNodes()
  const groups = useGroups()
  const channels = useAlertChannels()
  const [rows, setRows] = useState<FleetSummarySchedule[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState("")
  const [unavailable, setUnavailable] = useState(false)
  const [revision, setRevision] = useState(0)
  const [editing, setEditing] = useState<FleetSummarySchedule | null | undefined>(undefined)
  const [receipt, setReceipt] = useState("")
  const refresh = useCallback(() => setRevision(value => value + 1), [])
  useEffect(() => {
    const controller = new AbortController()
    setLoading(true); setError(""); setUnavailable(false)
    listFleetSummaries(controller.signal).then(data => { if (!controller.signal.aborted) setRows(data) }).catch((failure: unknown) => {
      if (controller.signal.aborted) return
      setUnavailable(failure instanceof APIError && [404, 501, 503].includes(failure.status ?? 0))
      setError(failure instanceof Error ? failure.message : "Load failed")
    }).finally(() => { if (!controller.signal.aborted) setLoading(false) })
    return () => controller.abort()
  }, [revision])
  const action = (row: FleetSummarySchedule, kind: "test" | "delete") => flow.request({
    title: `${kind === "test" ? t("fleet.summary.send_test") : t("fleet.summary.delete_title")}: ${row.name}`,
    description: kind === "test" ? t("fleet.summary.send_description") : t("fleet.summary.delete_description"),
    requiresRecent: false,
    execute: async () => {
      if (kind === "test") { const result = await testFleetSummary(row.id); setReceipt(result.status) }
      else { await deleteFleetSummary(row.id, row.revision); refresh() }
    },
  })
  if (loading) return <Skeleton className="h-40" />
  if (error && unavailable) return <Alert><AlertDescription>{t("fleet.summary.disabled_service")}<Button variant="outline" onClick={refresh}>{t("fleet.retry")}</Button></AlertDescription></Alert>
  if (error) return <Alert variant="destructive"><AlertDescription>{error}<Button variant="outline" onClick={refresh}>{t("fleet.retry")}</Button></AlertDescription></Alert>
  const nodeOptions = (nodes.data ?? []).map(node => ({ id: node.id, name: node.alias || node.name }))
  const groupOptions = (groups.data ?? []).map(group => ({ id: group.id, name: group.name }))
  const channelOptions = (channels.data ?? []).map(channel => ({ id: channel.id, name: channel.name }))
  const headers = [t("fleet.name"), t("fleet.summary.schedule"), t("fleet.summary.next_run"), t("fleet.summary.scope"), t("fleet.summary.channels"), t("fleet.summary.last_result"), t("fleet.summary.actions")]
  return (
    <section className="space-y-4">
      <Button onClick={() => setEditing(null)}>{t("fleet.summary.add")}</Button>
      {receipt && <Alert><AlertDescription>{receipt}<Button variant="link" onClick={onHistory}>{t("fleet.summary.history")}</Button></AlertDescription></Alert>}
      {rows.length === 0
        ? <p className="text-sm text-muted-foreground">{t("fleet.summary.empty")}</p>
        : <Table>
          <TableHeader><TableRow>{headers.map(label => <TableHead key={label}>{label}</TableHead>)}</TableRow></TableHeader>
          <TableBody>
            {rows.map(row => (
              <TableRow key={row.id}>
                <TableCell>{row.name}{!row.enabled && <Badge variant="secondary" className="ml-2">{t("fleet.summary.disabled")}</Badge>}</TableCell>
                <TableCell>{scheduleLabel(row, t)}</TableCell>
                <TableCell>{nextRunLabel(row.next_run_at, locale)}</TableCell>
                <TableCell className="text-sm">
                  <span className="block">{t("fleet.summary.nodes")}: {scopeSummary(row.node_ids, nodeOptions, t)}</span>
                  <span className="block">{t("fleet.groups")}: {scopeSummary(row.group_ids, groupOptions, t)}</span>
                </TableCell>
                <TableCell>{scopeSummary(row.channel_ids, channelOptions, t)}</TableCell>
                <TableCell>{row.last_result ?? "—"} · {deliveryLabel(row.delivery_state, t)}</TableCell>
                <TableCell className="flex gap-2">
                  <Button size="sm" variant="outline" onClick={() => setEditing(row)}>{t("fleet.summary.edit")}</Button>
                  <Button size="sm" variant="outline" onClick={() => action(row, "test")}><SendIcon />{t("fleet.summary.test_send")}</Button>
                  <Button size="sm" variant="destructive" onClick={() => action(row, "delete")}>{t("fleet.summary.delete")}</Button>
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>}
      {editing !== undefined && <FleetSummaryEditor value={editing} close={() => setEditing(undefined)} saved={refresh} />}
      <OperationConfirm flow={flow} />
    </section>
  )
}
