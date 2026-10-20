import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table"
import { useLocale } from "@/context/locale"
import { erasureActive, type MetricErasureJob } from "@/lib/metric-erasure-api"
import { dateTimeFormatter } from "@/lib/intl-format"

export function MetricErasureJobs(props: {
  jobs: MetricErasureJob[]
  pending: boolean
  onCancel: (job: MetricErasureJob) => void
  onRetry: (job: MetricErasureJob) => void
}) {
  const { t } = useLocale()
  if (props.jobs.length === 0) {
    return <p className="text-sm text-muted-foreground">{t("erase.empty")}</p>
  }
  return (
    <Table>
      <TableHeader>
        <TableRow>
          <TableHead>{t("erase.table_scope")}</TableHead>
          <TableHead>{t("erase.table_target")}</TableHead>
          <TableHead>{t("erase.table_state")}</TableHead>
          <TableHead>{t("erase.table_progress")}</TableHead>
          <TableHead>{t("erase.table_requested")}</TableHead>
          <TableHead className="text-right">{t("erase.table_actions")}</TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        {props.jobs.map((job) => (
          <JobRow key={job.id} job={job} pending={props.pending} onCancel={props.onCancel} onRetry={props.onRetry} />
        ))}
      </TableBody>
    </Table>
  )
}

function JobRow(props: {
  job: MetricErasureJob
  pending: boolean
  onCancel: (job: MetricErasureJob) => void
  onRetry: (job: MetricErasureJob) => void
}) {
  const { t, locale } = useLocale()
  const job = props.job
  return (
    <TableRow>
      <TableCell>{t(`erase.scope.${job.scope}`)}</TableCell>
      <TableCell>{job.target_name || "--"}</TableCell>
      <TableCell>{stateBadge(t, job)}</TableCell>
      <TableCell className="text-muted-foreground">{progressText(t, job)}</TableCell>
      <TableCell className="text-muted-foreground">
        {dateTimeFormatter(locale, { dateStyle: "medium", timeStyle: "short" }).format(new Date(job.requested_at))}
      </TableCell>
      <TableCell className="text-right">
        {job.state === "queued" ? (
          <Button size="xs" variant="outline" disabled={props.pending} onClick={() => props.onCancel(job)}>
            {t("erase.cancel")}
          </Button>
        ) : null}
        {job.state === "failed" ? (
          <Button size="xs" variant="outline" disabled={props.pending} onClick={() => props.onRetry(job)}>
            {t("erase.retry")}
          </Button>
        ) : null}
      </TableCell>
    </TableRow>
  )
}

function stateBadge(t: (key: string) => string, job: MetricErasureJob) {
  if (job.state === "failed") {
    return <Badge variant="destructive">{t(`erase.state.${job.state}`)}</Badge>
  }
  if (job.state === "succeeded") {
    return <Badge variant="secondary">{t(`erase.state.${job.state}`)}</Badge>
  }
  return <Badge variant="outline">{t(`erase.state.${job.state}`)}</Badge>
}

function progressText(t: (key: string, params?: Record<string, string>) => string, job: MetricErasureJob): string {
  if (job.state === "failed") return job.error_code ? `${t("erase.error_code")}: ${job.error_code}` : t("erase.error")
  if (!erasureActive(job.state) && job.state !== "queued") return "--"
  const cursor = job.progress_cursor ? t("erase.progress_cursor", { cursor: job.progress_cursor }) : t("erase.progress_measurement")
  return job.attempts > 1 ? `${cursor} · ${t("erase.attempts", { count: String(job.attempts) })}` : cursor
}
