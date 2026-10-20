import { useState } from "react"
import { useAlertEvents } from "@/hooks/use-api"
import * as api from "@/lib/api"
import { useLocale } from "@/context/locale"
import { formatDateTime } from "@/lib/intl-format"
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
  const [statusFilter, setStatusFilter] = useState("all")
  const { data: events, loading, loadingMore, error, refresh, nextCursor, loadMore } = useAlertEvents(statusFilter)
  const { locale, t } = useLocale()
  const [actionError, setActionError] = useState<string | null>(null)
  const [busy, setBusy] = useState<string | null>(null)
  const statusOptions = [
    { label: t("app.all"), value: "all" },
    { label: t("alert.triggered"), value: "triggered" },
    { label: t("alert.acknowledged"), value: "acknowledged" },
    { label: t("alert.resolved"), value: "resolved" },
  ]

  // 显示名一律取事件快照；实体已删除时只渲染“已删除”，绝不回退成节点或规则 ID。
  const displayName = (snapshot: string | undefined, fallback: string) =>
    snapshot && snapshot.length > 0 ? snapshot : fallback

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
          {events?.length ?? 0}
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
              ) : events && events.length > 0 ? (
                events.map((event) => (
                  <TableRow key={event.id}>
                    <TableCell className="font-medium">
                      {displayName(event.node_name_snapshot, t("alert.node_deleted"))}
                    </TableCell>
                    <TableCell>
                      <p>{event.message}</p>
                      <p className="mt-1 text-xs text-muted-foreground">
                        {displayName(event.rule_name_snapshot, t("alert.rule_deleted"))}
                      </p>
                    </TableCell>
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
                      {formatDateTime(locale, event.triggered_at)}
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

      {nextCursor && (
        <div className="flex justify-center">
          <Button variant="outline" size="sm" disabled={loadingMore} onClick={() => void loadMore()}>
            {t("alert.load_more")}
          </Button>
        </div>
      )}
    </div>
  )
}


export default AlertEventsPanel
