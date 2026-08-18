import { useState } from "react"
import { useAlertEvents, useManagedNodes } from "@/hooks/use-api"
import * as api from "@/lib/api"
import { useLocale } from "@/context/locale"
import { Alert, AlertDescription } from "@/components/ui/alert"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card, CardContent } from "@/components/ui/card"
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select"
import { Skeleton } from "@/components/ui/skeleton"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table"
import { CheckIcon, ShieldCheckIcon } from "lucide-react"
import { messageFromError } from "@/pages/admin/alert-utils"

function AlertEventsPanel() {
  const { data: events, loading, error, refresh } = useAlertEvents()
  const { data: nodes } = useManagedNodes()
  const { t } = useLocale()
  const [statusFilter, setStatusFilter] = useState("all")
  const [actionError, setActionError] = useState<string | null>(null)
  const [busy, setBusy] = useState<string | null>(null)
  const statusOptions = [
    { label: t("app.all"), value: "all" },
    { label: t("alert.triggered"), value: "triggered" },
    { label: t("alert.acknowledged"), value: "acknowledged" },
    { label: t("alert.resolved"), value: "resolved" },
  ]

  const getNodeName = (nodeId: string) => nodes?.find((n) => n.id === nodeId)?.name ?? nodeId

  const filtered = events?.filter((e) => {
    if (statusFilter === "all") return true
    return e.status === statusFilter
  })

  const handleAcknowledge = async (eventId: string) => {
    setActionError(null)
    setBusy(eventId)
    try {
      await api.acknowledgeAlertEvent(eventId)
      await refresh()
    } catch (err) {
      setActionError(messageFromError(err))
    } finally {
      setBusy(null)
    }
  }

  const handleResolve = async (eventId: string) => {
    setActionError(null)
    setBusy(eventId)
    try {
      await api.resolveAlertEvent(eventId)
      await refresh()
    } catch (err) {
      setActionError(messageFromError(err))
    } finally {
      setBusy(null)
    }
  }

  if (error) {
    return <Alert variant="destructive"><AlertDescription>{error}</AlertDescription></Alert>
  }

  return (
    <div className="flex flex-col gap-4">
      <div className="flex items-center justify-between">
        <div className="text-sm text-muted-foreground">
          {events ? filtered?.length ?? 0 : 0} / {events?.length ?? 0}
        </div>
        <Button variant="ghost" size="sm" onClick={() => void refresh()}>{t("app.refresh")}</Button>
      </div>
      <div className="flex justify-end">
        <Select
          items={statusOptions}
          value={statusFilter}
          onValueChange={(v) => { if (v) setStatusFilter(v) }}
        >
          <SelectTrigger className="w-36">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectGroup>
              {statusOptions.map((option) => (
                <SelectItem key={option.value} value={option.value}>{option.label}</SelectItem>
              ))}
            </SelectGroup>
          </SelectContent>
        </Select>
      </div>

      {actionError && (
        <Alert variant="destructive"><AlertDescription>{actionError}</AlertDescription></Alert>
      )}

      <Card>
        <CardContent className="p-0">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>{t("app.nodes")}</TableHead>
                <TableHead>{t("app.description")}</TableHead>
                <TableHead>{t("alert.threshold")}</TableHead>
                <TableHead>{t("alert.status")}</TableHead>
                <TableHead>{t("alert.triggered_at")}</TableHead>
                <TableHead className="w-28">{t("app.actions")}</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {loading ? (
                Array.from({ length: 3 }).map((_, i) => (
                  <TableRow key={i}>
                    <TableCell><Skeleton className="h-4 w-16" /></TableCell>
                    <TableCell><Skeleton className="h-4 w-32" /></TableCell>
                    <TableCell><Skeleton className="h-4 w-12" /></TableCell>
                    <TableCell><Skeleton className="h-4 w-16" /></TableCell>
                    <TableCell><Skeleton className="h-4 w-24" /></TableCell>
                    <TableCell><Skeleton className="h-8 w-20" /></TableCell>
                  </TableRow>
                ))
              ) : filtered && filtered.length > 0 ? (
                filtered.map((event) => (
                  <TableRow key={event.id}>
                    <TableCell className="font-medium">{getNodeName(event.node_id)}</TableCell>
                    <TableCell>{event.message}</TableCell>
                    <TableCell className="font-mono text-xs">{event.value}</TableCell>
                    <TableCell>
                      <Badge variant={
                        event.status === "triggered" ? "destructive"
                          : event.status === "acknowledged" ? "default"
                          : "secondary"
                      }>
                        {event.status === "triggered" ? t("alert.triggered")
                          : event.status === "acknowledged" ? t("alert.acknowledged")
                          : t("alert.resolved")}
                      </Badge>
                    </TableCell>
                    <TableCell className="text-xs text-muted-foreground">
                      {new Date(event.triggered_at).toLocaleString()}
                    </TableCell>
                    <TableCell>
                      {event.status === "triggered" && (
                        <Button size="sm" variant="outline" onClick={() => void handleAcknowledge(event.id)}
                          disabled={busy === event.id}>
                          <ShieldCheckIcon data-icon="inline-start" />
                          {t("alert.acknowledge")}
                        </Button>
                      )}
                      {event.status === "acknowledged" && (
                        <Button size="sm" onClick={() => void handleResolve(event.id)}
                          disabled={busy === event.id}>
                          <CheckIcon data-icon="inline-start" />
                          {t("alert.resolve")}
                        </Button>
                      )}
                    </TableCell>
                  </TableRow>
                ))
              ) : (
                <TableRow>
                  <TableCell colSpan={6} className="text-center text-muted-foreground">
                    {t("app.no_data")}
                  </TableCell>
                </TableRow>
              )}
            </TableBody>
          </Table>
        </CardContent>
      </Card>
    </div>
  )
}


export default AlertEventsPanel
