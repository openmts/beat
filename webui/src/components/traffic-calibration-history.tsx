import { useCallback, useEffect, useState } from "react"
import { RotateCcwIcon } from "lucide-react"

import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Sheet, SheetContent, SheetDescription, SheetHeader, SheetTitle } from "@/components/ui/sheet"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table"
import { useLocale } from "@/context/locale"
import { dateTimeFormatter } from "@/lib/intl-format"
import {
  activeCalibration,
  formatCalibrationBytes,
  listTrafficCalibrations,
  type TrafficCalibrationEvent,
} from "@/lib/traffic-calibration"

interface TrafficCalibrationHistoryProps {
  nodeId: string | null
  open: boolean
  onOpenChange: (open: boolean) => void
  onRevoke: (eventId: string) => void
  pending: boolean
}

export function TrafficCalibrationHistory(props: TrafficCalibrationHistoryProps) {
  const { t, locale } = useLocale()
  const [events, setEvents] = useState<TrafficCalibrationEvent[]>([])
  const [cursor, setCursor] = useState<string | undefined>(undefined)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState("")

  const load = useCallback(async (nodeId: string, nextCursor?: string) => {
    setLoading(true)
    setError("")
    try {
      const page = await listTrafficCalibrations(nodeId, { limit: 25, cursor: nextCursor })
      setEvents((current) => (nextCursor ? [...current, ...page.events] : page.events))
      setCursor(page.next_cursor)
    } catch (err) {
      setError(err instanceof Error ? err.message : "Calibration history unavailable")
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    if (!props.open || !props.nodeId) return
    setEvents([])
    setCursor(undefined)
    void load(props.nodeId)
  }, [props.open, props.nodeId, load])

  const active = activeCalibration(events)
  return (
    <Sheet open={props.open} onOpenChange={props.onOpenChange}>
      <SheetContent side="right" className="w-full overflow-y-auto p-6 sm:max-w-2xl">
        <SheetHeader>
          <SheetTitle>{t("traffic.calibration_history")}</SheetTitle>
          <SheetDescription>{t("traffic.calibration_history_description")}</SheetDescription>
        </SheetHeader>
        {error ? <p role="alert" className="text-sm text-destructive">{error}</p> : null}
        {events.length === 0 && !loading ? (
          <p className="text-sm text-muted-foreground">{t("traffic.calibration_history_empty")}</p>
        ) : (
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>{t("traffic.calibration_effective")}</TableHead>
                <TableHead>{t("traffic.calibration_mode")}</TableHead>
                <TableHead>{t("traffic.calibration_upload")}</TableHead>
                <TableHead>{t("traffic.calibration_download")}</TableHead>
                <TableHead>{t("traffic.calibration_actor")}</TableHead>
                <TableHead>{t("traffic.calibration_created_at")}</TableHead>
                <TableHead />
              </TableRow>
            </TableHeader>
            <TableBody>
              {events.map((event) => (
                <TableRow key={event.id}>
                  <TableCell className="whitespace-nowrap">
                    {formatInstant(event.effective_at ?? event.created_at, locale)}
                  </TableCell>
                  <TableCell>
                    <Badge variant={event.kind === "set" ? "secondary" : "outline"}>
                      {t(`traffic.calibration_kind_${event.kind}`)}
                    </Badge>
                    {event.kind === "set" && event.id === active?.id ? (
                      <Badge variant="default" className="ml-1">{t("traffic.calibration_active")}</Badge>
                    ) : null}
                  </TableCell>
                  <TableCell className="tabular-nums">
                    {event.target_sent === undefined ? "--" : formatCalibrationBytes(event.target_sent)}
                  </TableCell>
                  <TableCell className="tabular-nums">
                    {event.target_received === undefined ? "--" : formatCalibrationBytes(event.target_received)}
                  </TableCell>
                  <TableCell className="max-w-40 truncate" title={event.actor.name}>
                    {event.actor.name || "--"}
                  </TableCell>
                  <TableCell className="whitespace-nowrap">{formatInstant(event.created_at, locale)}</TableCell>
                  <TableCell>
                    {event.kind === "set" && event.id === active?.id ? (
                      <Button
                        variant="outline"
                        size="sm"
                        disabled={props.pending}
                        onClick={() => props.onRevoke(event.id)}
                      >
                        <RotateCcwIcon />{t("traffic.calibration_revoke")}
                      </Button>
                    ) : null}
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        )}
        {cursor ? (
          <Button
            variant="outline"
            disabled={loading || !props.nodeId}
            onClick={() => props.nodeId ? void load(props.nodeId, cursor) : undefined}
          >
            {t("traffic.calibration_history_more")}
          </Button>
        ) : null}
      </SheetContent>
    </Sheet>
  )
}

function formatInstant(value: string, locale: string): string {
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return "--"
  return dateTimeFormatter(locale, { year: "numeric", month: "short", day: "numeric", hour: "2-digit", minute: "2-digit" }).format(date)
}
