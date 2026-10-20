import { MoreHorizontalIcon, RefreshCwIcon, XIcon } from "lucide-react"

import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuTrigger } from "@/components/ui/dropdown-menu"
import { Skeleton } from "@/components/ui/skeleton"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table"
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip"
import { useLocale } from "@/context/locale"
import { dateTimeFormatter } from "@/lib/intl-format"
import { diagnosticActive, diagnosticRetryable, diagnosticStateVariant } from "@/lib/diagnostics-format"
import type { DiagnosticJob } from "@/lib/diagnostics-api"

interface DiagnosticsJobTableProps {
  jobs: DiagnosticJob[]
  loading: boolean
  interrupted?: boolean
  onSelect: (job: DiagnosticJob) => void
  onCancel: (job: DiagnosticJob) => void
  onRetry: (job: DiagnosticJob) => void
  onDelete: (job: DiagnosticJob) => void
}

export function DiagnosticsJobTable(props: DiagnosticsJobTableProps) {
  const { t, locale } = useLocale()
  if (props.loading) {
    return <div className="flex flex-col gap-2"><Skeleton className="h-10 w-full" /><Skeleton className="h-10 w-full" /><Skeleton className="h-10 w-full" /></div>
  }
  if (props.jobs.length === 0) {
    return <p className="rounded-lg border border-dashed border-border px-4 py-8 text-center text-sm text-muted-foreground">{t("diagnostics.empty")}</p>
  }
  return (
    <div className="overflow-x-auto rounded-lg border border-border">
      <Table>
        <TableHeader>
          <TableRow>
            <TableHead>{t("diagnostics.table_created")}</TableHead>
            <TableHead>{t("diagnostics.table_node")}</TableHead>
            <TableHead>{t("diagnostics.table_target")}</TableHead>
            <TableHead>{t("diagnostics.table_kind")}</TableHead>
            <TableHead>{t("diagnostics.table_state")}</TableHead>
            <TableHead>{t("diagnostics.table_actor")}</TableHead>
            <TableHead className="text-right">{t("diagnostics.table_actions")}</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {props.jobs.map((job) => (
            <TableRow key={job.id}>
              <TableCell className="whitespace-nowrap tabular-nums text-xs text-muted-foreground">{formatTimestamp(job.created_at, locale)}</TableCell>
              <TableCell className="font-medium">{job.source_node_name || job.source_node_id}</TableCell>
              <TableCell>{job.target_name || job.target_id}</TableCell>
              <TableCell className="whitespace-nowrap text-xs">{t(`diagnostics.kind.${job.kind}`)}</TableCell>
              <TableCell>
                <Badge variant={diagnosticStateVariant(job.state)}>{t(`diagnostics.state.${job.state}`)}</Badge>
                {job.cancel_requested && diagnosticActive(job.state) && <span className="ms-1 text-xs text-muted-foreground">{t("diagnostics.cancel_hint")}</span>}
              </TableCell>
              <TableCell className="text-xs text-muted-foreground">{job.actor_username || job.actor_user_id}</TableCell>
              <TableCell className="text-right">
                <div className="flex items-center justify-end gap-1">
                  {diagnosticActive(job.state) && (
                    <Tooltip>
                      <TooltipTrigger render={<Button size="icon" variant="ghost" aria-label={`${t("diagnostics.cancel_hint")}: ${job.id}`} onClick={() => props.onCancel(job)} />}>
                        <XIcon />
                      </TooltipTrigger>
                      <TooltipContent>{t("diagnostics.cancel_hint")}</TooltipContent>
                    </Tooltip>
                  )}
                  <Button size="sm" variant="ghost" onClick={() => props.onSelect(job)}>{t("diagnostics.detail")}</Button>
                  <DropdownMenu>
                    <DropdownMenuTrigger render={<Button size="icon" variant="ghost" aria-label={t("diagnostics.table_actions")} />}>
                      <MoreHorizontalIcon />
                    </DropdownMenuTrigger>
                    <DropdownMenuContent align="end">
                      <DropdownMenuItem disabled={!diagnosticRetryable(job.state)} onSelect={() => props.onRetry(job)}>{t("diagnostics.retry")}</DropdownMenuItem>
                      <DropdownMenuItem variant="destructive" onSelect={() => props.onDelete(job)}>{t("diagnostics.delete")}</DropdownMenuItem>
                    </DropdownMenuContent>
                  </DropdownMenu>
                </div>
              </TableCell>
            </TableRow>
          ))}
        </TableBody>
      </Table>
    </div>
  )
}

function formatTimestamp(value: string, locale: string): string {
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return value
  return dateTimeFormatter(locale, { month: "numeric", day: "numeric", hour: "2-digit", minute: "2-digit" }).format(date)
}

export function DiagnosticsRefreshButton(props: { onClick: () => void }) {
  const { t } = useLocale()
  return (
    <Button variant="outline" size="sm" onClick={props.onClick}>
      <RefreshCwIcon />
      {t("diagnostics.refresh")}
    </Button>
  )
}
