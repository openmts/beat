import { useMemo, useState } from "react"
import { LoaderCircleIcon, PencilIcon, PlusIcon, SendIcon, Trash2Icon } from "lucide-react"
import { useAlertChannels, useManagedNodes, useTrafficReportSchedules } from "@/hooks/use-api"
import * as api from "@/lib/api"
import { useLocale } from "@/context/locale"
import { Alert, AlertDescription } from "@/components/ui/alert"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Dialog, DialogContent, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { Skeleton } from "@/components/ui/skeleton"
import { Switch } from "@/components/ui/switch"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table"
import type {
  AlertChannel,
  ManagedNode,
  TrafficReportDeliveryStatus,
  TrafficReportSchedule,
} from "@/types"
import { messageFromError } from "@/pages/admin/alert-utils"
import {
  emptyTrafficReportForm,
  trafficReportFormError,
  trafficReportFormFrom,
  trafficReportPayload,
  type TrafficReportForm,
} from "@/pages/admin/traffic-report-config"
import { TrafficReportDialog } from "@/pages/admin/traffic-report-dialog"

export default function TrafficReportsPanel() {
  const schedulesState = useTrafficReportSchedules()
  const nodesState = useManagedNodes()
  const channelsState = useAlertChannels()
  const { locale, t } = useLocale()
  const [dialogOpen, setDialogOpen] = useState(false)
  const [editing, setEditing] = useState<TrafficReportSchedule | null>(null)
  const [deleteID, setDeleteID] = useState<string | null>(null)
  const [testingID, setTestingID] = useState<string | null>(null)
  const [loading, setLoading] = useState(false)
  const [actionError, setActionError] = useState<string | null>(null)
  const [form, setForm] = useState<TrafficReportForm>(emptyTrafficReportForm)
  const [testResults, setTestResults] = useState<Record<string, TrafficReportDeliveryStatus>>({})

  const names = useMemo(() => scopeNames(nodesState.data ?? [], channelsState.data ?? []), [nodesState.data, channelsState.data])
  const openCreate = () => {
    setForm(emptyTrafficReportForm())
    setEditing(null)
    setActionError(null)
    setDialogOpen(true)
  }
  const openEdit = (schedule: TrafficReportSchedule) => {
    setForm(trafficReportFormFrom(schedule))
    setEditing(schedule)
    setActionError(null)
    setDialogOpen(true)
  }
  const save = async () => {
    const validationError = trafficReportFormError(form)
    if (validationError) {
      setActionError(t(validationError))
      return
    }
    setLoading(true)
    setActionError(null)
    try {
      const payload = trafficReportPayload(form)
      if (editing) await api.updateTrafficReportSchedule(editing.id, payload)
      else await api.createTrafficReportSchedule(payload)
      await schedulesState.refresh()
      setDialogOpen(false)
      setEditing(null)
    } catch (caught) {
      setActionError(messageFromError(caught))
    } finally {
      setLoading(false)
    }
  }
  const toggle = async (schedule: TrafficReportSchedule) => {
    setActionError(null)
    try {
      const payload = trafficReportPayload({ ...trafficReportFormFrom(schedule), enabled: !schedule.enabled })
      await api.updateTrafficReportSchedule(schedule.id, payload)
      await schedulesState.refresh()
    } catch (caught) {
      setActionError(messageFromError(caught))
    }
  }
  const test = async (schedule: TrafficReportSchedule) => {
    setTestingID(schedule.id)
    setActionError(null)
    try {
      const result = await api.testTrafficReportSchedule(schedule.id)
      setTestResults((current) => ({ ...current, [schedule.id]: result.delivery }))
    } catch (caught) {
      setActionError(messageFromError(caught))
    } finally {
      setTestingID(null)
    }
  }
  const remove = async () => {
    if (!deleteID) return
    setLoading(true)
    setActionError(null)
    try {
      await api.deleteTrafficReportSchedule(deleteID)
      await schedulesState.refresh()
      setDeleteID(null)
    } catch (caught) {
      setActionError(messageFromError(caught))
    } finally {
      setLoading(false)
    }
  }

  const error = schedulesState.error || nodesState.error || channelsState.error
  if (error) return <Alert variant="destructive"><AlertDescription>{error}</AlertDescription></Alert>
  return (
    <div className="flex flex-col gap-4">
      <div className="flex items-center justify-between gap-3">
        <p className="text-sm text-muted-foreground">{t("traffic_report.description")}</p>
        <Button onClick={openCreate}><PlusIcon data-icon="inline-start" />{t("traffic_report.create")}</Button>
      </div>
      {actionError && !dialogOpen ? <Alert variant="destructive"><AlertDescription>{actionError}</AlertDescription></Alert> : null}
      <div className="overflow-hidden rounded-lg border">
        <Table>
          <TableHeader><TableRow>
            <TableHead>{t("node.name")}</TableHead>
            <TableHead>{t("traffic_report.cadence")}</TableHead>
            <TableHead>{t("traffic_report.scope")}</TableHead>
            <TableHead>{t("traffic_report.next_run")}</TableHead>
            <TableHead>{t("traffic_report.last_run")}</TableHead>
            <TableHead>{t("alert.delivery")}</TableHead>
            <TableHead>{t("alert.enabled")}</TableHead>
            <TableHead className="w-52">{t("app.actions")}</TableHead>
          </TableRow></TableHeader>
          <TableBody>
            {schedulesState.loading ? <LoadingRows /> : schedulesState.data?.length ? schedulesState.data.map((schedule) => (
              <TableRow key={schedule.id}>
                <TableCell className="font-medium">{schedule.name}</TableCell>
                <TableCell>{scheduleLabel(schedule, t)}</TableCell>
                <TableCell className="max-w-64"><ScopeSummary schedule={schedule} names={names} t={t} /></TableCell>
                <TableCell>{formatDate(schedule.next_run_at, locale)}</TableCell>
                <TableCell>{schedule.last_run_at ? formatDate(schedule.last_run_at, locale) : t("traffic_report.never_run")}</TableCell>
                <TableCell><DeliveryBadge status={testResults[schedule.id] ?? schedule.last_delivery} t={t} /></TableCell>
                <TableCell><Switch checked={schedule.enabled} onCheckedChange={() => toggle(schedule)} /></TableCell>
                <TableCell><div className="flex items-center gap-1">
                  <Button variant="outline" size="sm" onClick={() => test(schedule)} disabled={testingID === schedule.id}>
                    {testingID === schedule.id ? <LoaderCircleIcon data-icon="inline-start" className="animate-spin" /> : <SendIcon data-icon="inline-start" />}
                    {t("traffic_report.test")}
                  </Button>
                  <Button variant="ghost" size="icon-sm" aria-label={t("app.edit")} title={t("app.edit")} onClick={() => openEdit(schedule)}><PencilIcon /></Button>
                  <Button variant="ghost" size="icon-sm" aria-label={t("app.delete")} title={t("app.delete")} onClick={() => setDeleteID(schedule.id)}><Trash2Icon className="text-destructive" /></Button>
                </div></TableCell>
              </TableRow>
            )) : <TableRow><TableCell colSpan={8} className="text-center text-muted-foreground">{t("app.no_data")}</TableCell></TableRow>}
          </TableBody>
        </Table>
      </div>
      <TrafficReportDialog
        open={dialogOpen} editing={editing !== null} loading={loading} error={dialogOpen ? actionError : null}
        form={form} nodes={nodesState.data ?? []} channels={channelsState.data ?? []}
        onOpenChange={setDialogOpen} onChange={setForm} onSave={save}
      />
      <Dialog open={deleteID !== null} onOpenChange={(open) => { if (!open) setDeleteID(null) }}>
        <DialogContent className="max-w-sm">
          <DialogHeader><DialogTitle>{t("app.delete")}</DialogTitle></DialogHeader>
          <p className="text-sm text-muted-foreground">{t("confirm.delete")}</p>
          <DialogFooter>
            <Button variant="outline" onClick={() => setDeleteID(null)}>{t("app.cancel")}</Button>
            <Button variant="destructive" onClick={remove} disabled={loading}>{t("app.delete")}</Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  )
}

function LoadingRows() {
  return Array.from({ length: 2 }, (_, index) => (
    <TableRow key={index}>{Array.from({ length: 8 }, (__, cell) => <TableCell key={cell}><Skeleton className="h-5 w-full" /></TableCell>)}</TableRow>
  ))
}

function scheduleLabel(schedule: TrafficReportSchedule, t: (key: string) => string): string {
  const time = `${String(schedule.send_hour).padStart(2, "0")}:${String(schedule.send_minute).padStart(2, "0")}`
  const cadence = t(`traffic_report.cadence.${schedule.cadence}`)
  if (schedule.cadence === "weekly") return `${cadence} · ${t(`traffic_report.weekday.${schedule.weekday}`)} · ${time} · ${schedule.timezone}`
  if (schedule.cadence === "monthly") return `${cadence} · ${schedule.month_day} · ${time} · ${schedule.timezone}`
  return `${cadence} · ${time} · ${schedule.timezone}`
}

function ScopeSummary(props: {
  schedule: TrafficReportSchedule
  names: { nodes: Map<string, string>; channels: Map<string, string> }
  t: (key: string) => string
}) {
  const nodes = props.schedule.all_nodes ? props.t("traffic_report.all") : readableNames(props.schedule.node_ids, props.names.nodes, props.t)
  const channels = props.schedule.all_channels ? props.t("traffic_report.all") : readableNames(props.schedule.channel_ids, props.names.channels, props.t)
  return <div className="flex min-w-0 flex-col gap-1 text-sm"><span className="truncate" title={nodes}>{props.t("traffic_report.nodes")}: {nodes}</span><span className="truncate" title={channels}>{props.t("traffic_report.channels")}: {channels}</span></div>
}

function DeliveryBadge(props: { status?: TrafficReportDeliveryStatus; t: (key: string) => string }) {
  if (!props.status) return <Badge variant="secondary">{props.t("traffic_report.delivery.never")}</Badge>
  const variant = props.status.state === "failed" ? "destructive" : props.status.state === "partial" ? "outline" : "default"
  return <Badge variant={variant}>{props.t(`traffic_report.delivery.${props.status.state}`)} · {props.status.delivered}/{props.status.total}</Badge>
}

function scopeNames(nodes: ManagedNode[], channels: AlertChannel[]) {
  return {
    nodes: new Map(nodes.map((node) => [node.id, node.alias ? `${node.alias} (${node.name})` : node.name])),
    channels: new Map(channels.map((channel) => [channel.id, channel.name])),
  }
}

function readableNames(ids: string[], names: Map<string, string>, t: (key: string) => string): string {
  return ids.map((id) => names.get(id) ?? t("traffic_report.unavailable")).join(", ")
}

function formatDate(value: string, locale: string): string {
  return value ? new Date(value).toLocaleString(locale) : "--"
}
