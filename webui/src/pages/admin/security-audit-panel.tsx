import { useEffect, useState } from "react"
import { Alert, AlertDescription } from "@/components/ui/alert"
import { Badge } from "@/components/ui/badge"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table"
import { useLocale } from "@/context/locale"
import { listAuditEvents } from "@/lib/security-api"
import type { AdminAuditEvent } from "@/types/security"

export function SecurityAuditPanel() {
  const { t } = useLocale()
  const [events, setEvents] = useState<AdminAuditEvent[]>([])
  const [error, setError] = useState("")

  useEffect(() => {
    void listAuditEvents().then((page) => setEvents(page.events)).catch((cause: unknown) => {
      setError(cause instanceof Error ? cause.message : t("app.error"))
    })
  }, [t])

  return (
    <Card>
      <CardHeader><CardTitle>{t("security.audit")}</CardTitle></CardHeader>
      <CardContent>
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
              <TableCell className="whitespace-nowrap">{new Date(event.created_at).toLocaleString()}</TableCell>
              <TableCell>{event.actor_username || "-"}</TableCell>
              <TableCell className="font-mono text-xs">{event.action}</TableCell>
              <TableCell className="max-w-56 truncate">{event.resource_id}</TableCell>
              <TableCell><Badge variant={event.outcome === "success" ? "default" : "destructive"}>{event.outcome}</Badge></TableCell>
            </TableRow>
          ))}</TableBody>
        </Table>
      </CardContent>
    </Card>
  )
}
