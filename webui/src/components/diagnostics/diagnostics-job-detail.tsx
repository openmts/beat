import { useEffect, useMemo, useState } from "react"
import { EyeIcon, EyeOffIcon } from "lucide-react"

import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Skeleton } from "@/components/ui/skeleton"
import { Sheet, SheetContent, SheetDescription, SheetHeader, SheetTitle } from "@/components/ui/sheet"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table"
import { DiagnosticsChart } from "@/components/diagnostics/diagnostics-chart"
import { useLocale } from "@/context/locale"
import { dateTimeFormatter } from "@/lib/intl-format"
import { useDiagnosticJobDetail, useDiagnosticSerieses } from "@/hooks/use-diagnostics"
import {
  detailSeriesSpecs,
  diagnosticPreviewInterval,
  diagnosticStateVariant,
  diagnosticTimeline,
  formatBytes,
  formatDiagnosticValue,
  maskDiagnosticAddress,
} from "@/lib/diagnostics-format"
import type { DiagnosticSeries } from "@/lib/diagnostics-api"

const chartColors = ["var(--chart-1)", "var(--chart-2)", "var(--chart-3)", "var(--chart-4)"]

interface DiagnosticsJobDetailProps {
  jobId: string | null
  open: boolean
  onOpenChange: (open: boolean) => void
}

export function DiagnosticsJobDetail(props: DiagnosticsJobDetailProps) {
  const { t, locale } = useLocale()
  const active = props.open ? props.jobId : null
  const detail = useDiagnosticJobDetail(active, diagnosticPreviewInterval)
  const job = detail.data?.job ?? null
  const specs = useMemo(() => (job ? detailSeriesSpecs(job) : []), [job])
  const series = useDiagnosticSerieses(active, specs)
  const [revealed, setRevealed] = useState(false)

  useEffect(() => {
    setRevealed(false)
  }, [props.jobId])

  return (
    <Sheet open={props.open} onOpenChange={props.onOpenChange}>
      <SheetContent side="right" className="w-full overflow-y-auto p-6 sm:max-w-2xl">
        <SheetHeader>
          <SheetTitle>{t("diagnostics.detail_title")}</SheetTitle>
          <SheetDescription>{job ? `${t(`diagnostics.kind.${job.kind}`)} · ${job.source_node_name || job.source_node_id} → ${job.target_name || job.target_id}` : "…"}</SheetDescription>
        </SheetHeader>
        {detail.loading && !job ? (
          <div className="flex flex-col gap-3"><Skeleton className="h-24 w-full" /><Skeleton className="h-48 w-full" /></div>
        ) : !job ? (
          <p className="text-sm text-muted-foreground">{t("diagnostics.empty")}</p>
        ) : (
          <div className="flex flex-col gap-6">
            {detail.error && <p className="text-sm text-destructive">{detail.error}</p>}
            <div className="flex flex-wrap items-center gap-3">
              <Badge variant={diagnosticStateVariant(job.state)}>{t(`diagnostics.state.${job.state}`)}</Badge>
              <span className="text-xs text-muted-foreground">{`${t("diagnostics.attempt")} ${job.attempt}`}</span>
              <span className="text-xs text-muted-foreground">{`${t("diagnostics.actor")}: ${job.actor_username || job.actor_user_id}`}</span>
              {job.error_code && <span className="text-xs text-destructive">{job.error_code}</span>}
            </div>
            <dl className="grid grid-cols-1 gap-3 sm:grid-cols-3">
              <SummaryBand label={t("diagnostics.measured_bytes")} value={formatBytes(job.actual_bytes)} />
              <SummaryBand label={t("diagnostics.reserved_bytes")} value={formatBytes(job.reserved_bytes)} />
              <SummaryBand label={t("diagnostics.table_created")} value={formatTimestamp(job.created_at, locale)} />
            </dl>
            <section className="flex flex-col gap-3">
              <h3 className="text-sm font-medium">{t("diagnostics.timeline")}</h3>
              <ol className="flex flex-col gap-2 sm:flex-row sm:items-start sm:gap-0">
                {diagnosticTimeline(job).map((step) => (
                  <li key={step.key} className="flex flex-1 flex-col gap-1">
                    <div className="flex items-center gap-2">
                      <span aria-hidden data-reached={step.reached} className={`inline-block size-2 rounded-full ${step.reached ? "bg-primary" : "bg-muted-foreground/40"}`} />
                      <span className={`text-xs ${step.current ? "font-semibold" : "text-muted-foreground"}`}>{t(`diagnostics.timeline.${step.key}`)}</span>
                    </div>
                    <span className="ps-4 text-xs tabular-nums text-muted-foreground">{step.at ? formatTimestamp(step.at, locale) : "—"}</span>
                  </li>
                ))}
              </ol>
            </section>
            {job.kind === "mtr" ? (
              <section className="flex flex-col gap-3">
                <div className="flex items-center justify-between gap-3">
                  <h3 className="text-sm font-medium">{t("diagnostics.hops")}</h3>
                  <Button size="sm" variant="outline" onClick={() => setRevealed((current) => !current)}>
                    {revealed ? <EyeOffIcon /> : <EyeIcon />}
                    {revealed ? t("diagnostics.hop_privacy_hide") : t("diagnostics.hop_privacy_show")}
                  </Button>
                </div>
                <p className="text-xs text-muted-foreground">{t("diagnostics.hop_privacy_note")}</p>
                <div className="overflow-x-auto rounded-lg border border-border">
                  <Table>
                    <TableHeader>
                      <TableRow>
                        <TableHead>{t("diagnostics.hop_index")}</TableHead>
                        <TableHead>{t("diagnostics.hop_address")}</TableHead>
                        <TableHead>{t("diagnostics.hop_reached")}</TableHead>
                      </TableRow>
                    </TableHeader>
                    <TableBody>
                      {(detail.data?.hops ?? []).map((hop) => (
                        <TableRow key={hop.index}>
                          <TableCell className="tabular-nums">{hop.index}</TableCell>
                          <TableCell className="font-mono text-xs">{revealed ? hop.address ?? hop.hostname ?? "—" : maskDiagnosticAddress(hop.address ?? hop.hostname)}</TableCell>
                          <TableCell className="text-xs">{hop.reached ? t("diagnostics.hop_reached_yes") : t("diagnostics.hop_unreached")}</TableCell>
                        </TableRow>
                      ))}
                    </TableBody>
                  </Table>
                </div>
              </section>
            ) : (
              <IperfSummary series={series.series} />
            )}
            {series.loading && <Skeleton className="h-48 w-full" />}
            {series.error && <p className="text-sm text-destructive">{series.error}</p>}
            {!series.loading && job.kind === "mtr" && (
              <div className="flex flex-col gap-6">
                <DiagnosticsChart title={t("diagnostics.chart_latency")} metric="diagnostic_mtr_latency_ms"
                  lines={[{ label: t("diagnostics.chart_latency"), color: chartColors[0], series: series.series[0] ?? null }]} />
                <DiagnosticsChart title={t("diagnostics.chart_loss")} metric="diagnostic_mtr_loss_percent"
                  lines={[{ label: t("diagnostics.loss"), color: chartColors[1], series: series.series[1] ?? null }]} />
              </div>
            )}
            {!series.loading && job.kind === "iperf3" && (
              <DiagnosticsChart title={t("diagnostics.chart_throughput")} metric="diagnostic_iperf_bytes_per_second"
                lines={[
                  { label: t("diagnostics.summary_sender"), color: chartColors[0], series: series.series[0] ?? null },
                  { label: t("diagnostics.summary_receiver"), color: chartColors[1], series: series.series[1] ?? null },
                ]} />
            )}
          </div>
        )}
      </SheetContent>
    </Sheet>
  )
}

function SummaryBand(props: { label: string; value: string }) {
  return (
    <div className="flex flex-col gap-1 border-t border-border pt-2">
      <dt className="text-xs text-muted-foreground">{props.label}</dt>
      <dd className="text-sm font-medium tabular-nums">{props.value}</dd>
    </div>
  )
}

function IperfSummary(props: { series: (DiagnosticSeries | null)[] }) {
  const { locale, t } = useLocale()
  const rows = [
    { role: t("diagnostics.summary_sender"), throughput: props.series[0] ?? null, bytes: props.series[2] ?? null },
    { role: t("diagnostics.summary_receiver"), throughput: props.series[1] ?? null, bytes: props.series[3] ?? null },
  ]
  return (
    <section className="flex flex-col gap-3">
      <h3 className="text-sm font-medium">{t("diagnostics.summary")}</h3>
      <div className="overflow-x-auto rounded-lg border border-border">
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>{t("diagnostics.summary_role")}</TableHead>
              <TableHead>{t("diagnostics.summary_throughput")}</TableHead>
              <TableHead>{t("diagnostics.summary_bytes")}</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {rows.map((row) => (
              <TableRow key={row.role}>
                <TableCell>{row.role}</TableCell>
                <TableCell className="tabular-nums">{lastValue(row.throughput, "diagnostic_iperf_bytes_per_second", locale)}</TableCell>
                <TableCell className="tabular-nums">{lastValue(row.bytes, "diagnostic_iperf_bytes", locale)}</TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      </div>
    </section>
  )
}

function lastValue(series: DiagnosticSeries | null, metric: string, locale?: string): string {
  const points = series?.points ?? []
  if (points.length === 0) return "—"
  return formatDiagnosticValue(metric, points[points.length - 1].Value, locale)
}

function formatTimestamp(value: string, locale: string): string {
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return value
  return dateTimeFormatter(locale, { dateStyle: "short", timeStyle: "short" }).format(date)
}
