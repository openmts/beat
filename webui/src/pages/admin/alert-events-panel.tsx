import { useState } from "react"
import { useAlertEvents, useManagedNodes } from "@/hooks/use-api"
import { useLocale } from "@/context/locale"
import { Alert, AlertDescription } from "@/components/ui/alert"
import { Badge } from "@/components/ui/badge"
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

function AlertEventsPanel() {
  const { data: events, loading, error } = useAlertEvents()
  const { data: nodes } = useManagedNodes()
  const { t } = useLocale()
  const [statusFilter, setStatusFilter] = useState("all")
  const statusOptions = [
    { label: t("app.all"), value: "all" },
    { label: t("alert.triggered"), value: "triggered" },
    { label: t("alert.resolved"), value: "resolved" },
  ]

  const getNodeName = (nodeId: string) => nodes?.find((n) => n.id === nodeId)?.name ?? nodeId

  const filtered = events?.filter((e) => {
    if (statusFilter === "all") return true
    return e.status === statusFilter
  })

  if (error) {
    return <Alert variant="destructive"><AlertDescription>{error}</AlertDescription></Alert>
  }

  return (
    <div className="flex flex-col gap-4">
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
                  </TableRow>
                ))
              ) : filtered && filtered.length > 0 ? (
                filtered.map((event) => (
                  <TableRow key={event.id}>
                    <TableCell className="font-medium">{getNodeName(event.node_id)}</TableCell>
                    <TableCell>{event.message}</TableCell>
                    <TableCell className="font-mono text-xs">{event.value}</TableCell>
                    <TableCell>
                      <Badge variant={event.status === "triggered" ? "destructive" : "default"}>
                        {event.status === "triggered" ? t("alert.triggered") : t("alert.resolved")}
                      </Badge>
                    </TableCell>
                    <TableCell className="text-xs text-muted-foreground">
                      {new Date(event.triggered_at).toLocaleString()}
                    </TableCell>
                  </TableRow>
                ))
              ) : (
                <TableRow>
                  <TableCell colSpan={5} className="text-center text-muted-foreground">
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
