import { useCallback, useEffect, useState } from "react"
import { Alert, AlertDescription } from "@/components/ui/alert"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table"
import { useLocale } from "@/context/locale"
import { formatDateTime } from "@/lib/intl-format"
import { listAuditEvents } from "@/lib/security-api"
import type { AdminAuditEvent } from "@/types/security"

const auditPageSize = 50

export function SecurityAuditPanel() {
  const { locale, t } = useLocale()
  const [events, setEvents] = useState<AdminAuditEvent[]>([])
  const [cursor, setCursor] = useState<string | null>(null)
  const [loadingMore, setLoadingMore] = useState(false)
  const [error, setError] = useState("")

  useEffect(() => {
    let active = true
    void listAuditEvents({ limit: auditPageSize }).then((page) => {
      if (!active) return
      setEvents(page.events)
      setCursor(page.next_cursor ?? null)
    }).catch((cause: unknown) => {
      if (active) setError(cause instanceof Error ? cause.message : t("app.error"))
    })
    return () => { active = false }
  }, [t])

  const loadMore = useCallback(async () => {
    if (!cursor || loadingMore) return
    setLoadingMore(true)
    setError("")
    try {
      const page = await listAuditEvents({ limit: auditPageSize, cursor })
      setEvents((current) => [...current, ...page.events])
      setCursor(page.next_cursor ?? null)
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : t("app.error"))
    } finally {
      setLoadingMore(false)
    }
  }, [cursor, loadingMore, t])

  return (
    <Card>
      <CardHeader><CardTitle>{t("security.audit")}</CardTitle></CardHeader>
      <CardContent className="flex flex-col gap-4">
        {error && <Alert variant="destructive"><AlertDescription>{error}</AlertDescription></Alert>}
        <Table>
          <TableHeader><TableRow>
            <TableHead>{t("security.time")}</TableHead>
            <TableHead>{t("security.actor")}</TableHead>
            <TableHead>{t("security.action")}</TableHead>
            <TableHead>{t("security.resource")}</TableHead>
            <TableHead>{t("security.outcome")}</TableHead>
          </TableRow></TableHeader>
          <TableBody>{events.map((event) => (
            <TableRow key={event.id}>
              <TableCell className="whitespace-nowrap">{formatDateTime(locale, event.created_at)}</TableCell>
              <TableCell>
                <span className="flex items-center gap-2">
                  {event.actor_type === "service" && (
                    <Badge variant="secondary">{t("security.service_actor")}</Badge>
                  )}
                  <span className="truncate">{event.actor_username || "-"}</span>
                </span>
              </TableCell>
              <TableCell className="font-mono text-xs">{event.action}</TableCell>
              <TableCell className="max-w-56 truncate">{event.resource_id}</TableCell>
              <TableCell><Badge variant={event.outcome === "success" ? "default" : "destructive"}>{event.outcome}</Badge></TableCell>
            </TableRow>
          ))}</TableBody>
        </Table>
        {cursor && (
          <div className="flex justify-center">
            <Button variant="outline" size="sm" disabled={loadingMore} onClick={() => void loadMore()}>
              {t("security.load_more")}
            </Button>
          </div>
        )}
      </CardContent>
    </Card>
  )
}
