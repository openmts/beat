import { Badge } from "@/components/ui/badge"
import { Accordion, AccordionItem, AccordionPanel, AccordionTrigger } from "@/components/ui/accordion"
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { Separator } from "@/components/ui/separator"
import { Skeleton } from "@/components/ui/skeleton"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table"
import { useLocale } from "@/context/locale"
import { useOperationResource } from "@/hooks/use-operation-resource"
import { getMigrationOverview } from "@/lib/migration-api"
import type { MigrationOverview } from "@/types/migration-overview"

// 只读视图固定 30 秒轮询；它没有开始、回滚、切换数据库或重启按钮。
const refreshMilliseconds = 30000

const stateLabelKeys: Record<string, string> = {
  current: "migration.state.current",
  upgrade_pending: "migration.state.upgrade_pending",
  pending_restore: "migration.state.pending_restore",
  inconsistent: "migration.state.inconsistent",
}

const healthLabelKeys: Record<string, string> = {
  ok: "migration.health.ok",
  degraded: "migration.health.degraded",
}

const failureLabelKeys: Record<string, string> = {
  future_schema: "migration.failure.future_schema",
  ledger_invalid: "migration.failure.ledger_invalid",
  integrity_failed: "migration.failure.integrity_failed",
  apply: "migration.failure.apply",
  busy: "migration.failure.busy",
  permission: "migration.failure.permission",
  unknown: "migration.failure.unknown",
}

type Translate = (key: string, params?: Record<string, string | number>) => string

function label(t: Translate, keys: Record<string, string>, value: string) {
  return t(keys[value] ?? value)
}

function stateVariant(state: string) {
  if (state === "current") return "secondary" as const
  if (state === "pending_restore") return "outline" as const
  return "destructive" as const
}

function facts(overview: MigrationOverview, t: Translate) {
  const latest = overview.latest_completed
  return [
    { label: t("migration.current_schema"), value: String(overview.current_schema) },
    { label: t("migration.supported_range"), value: `${overview.minimum_supported_schema}–${overview.maximum_supported_schema}` },
    { label: t("migration.latest_completed_migration"), value: latest ? `${latest.version} · ${latest.name}` : "—" },
    { label: t("migration.pending_migrations"), value: String(overview.pending_migrations.length) },
    {
      label: t("migration.restore_state"),
      value: overview.restore_pending ? (t("migration.staged_applied_on_next_start")) : overview.restore_staged ? (t("migration.staged_record_present")) : t("migration.no_restore_pending"),
    },
    { label: t("migration.application_database"), value: label(t, healthLabelKeys, overview.application_health) },
    { label: t("migration.metric_store"), value: label(t, healthLabelKeys, overview.metrics_health) },
    { label: t("migration.build_version"), value: overview.version || "—" },
  ]
}

function correctiveDecision(overview: MigrationOverview, t: Translate) {
  if (overview.state === "pending_restore") {
    return t("migration.a_validated_archive_is_staged_in_the_data_direct")
  }
  if (overview.state === "upgrade_pending" || overview.state === "inconsistent") {
    return t("migration.validate_the_backup_stop_the_server_run_the_loca")
  }
  return t("migration.the_server_applied_every_migration_and_nothing_i")
}

function RunbookSteps() {
  const { t } = useLocale()
  return (
    <Accordion>
      <AccordionItem value="inspect">
        <AccordionTrigger>{t("migration.1_inspect_local_migration_status")}</AccordionTrigger>
        <AccordionPanel>
          <p className="text-muted-foreground">{t("migration.run_it_with_the_server_stopped_it_only_reads_a_p")}</p>
          <pre className="mt-2 overflow-auto rounded-md bg-muted p-3 text-xs">{`beat-server admin-recovery migration-status --data-dir <BEAT_DATA_DIR>`}</pre>
        </AccordionPanel>
      </AccordionItem>
      <AccordionItem value="check">
        <AccordionTrigger>{t("migration.2_validate_the_backup_and_upgrade_path")}</AccordionTrigger>
        <AccordionPanel>
          <p className="text-muted-foreground">{t("migration.the_command_replays_migrations_on_a_private_copy")}</p>
          <pre className="mt-2 overflow-auto rounded-md bg-muted p-3 text-xs">{`beat-server admin-recovery migration-check --data-dir <BEAT_DATA_DIR> --backup <BACKUP_ARCHIVE>`}</pre>
        </AccordionPanel>
      </AccordionItem>
      <AccordionItem value="decide">
        <AccordionTrigger>{t("migration.3_retry_the_deployment_or_roll_back")}</AccordionTrigger>
        <AccordionPanel>
          <p className="text-muted-foreground">
            {t("migration.if_the_check_passes_retry_this_deployment_otherw")}
          </p>
        </AccordionPanel>
      </AccordionItem>
    </Accordion>
  )
}

export function MigrationRecoveryPanel() {
  const { t } = useLocale()
  const resource = useOperationResource(getMigrationOverview, refreshMilliseconds)
  const overview = resource.data
  return (
    <div className="space-y-4">
      <Alert variant={overview && overview.state !== "current" ? "destructive" : undefined}>
        <AlertTitle className="flex items-center gap-2">
          {t("migration.migration_and_recovery")}
          {overview ? <Badge variant={stateVariant(overview.state)}>{label(t, stateLabelKeys, overview.state)}</Badge> : null}
        </AlertTitle>
        <AlertDescription>
          {resource.error ? resource.error : overview ? correctiveDecision(overview, t) : t("migration.loading_migration_state")}
        </AlertDescription>
      </Alert>
      <Card>
        <CardHeader><CardTitle>{t("migration.current_state")}</CardTitle></CardHeader>
        <CardContent className="space-y-4">
          {overview ? (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>{t("migration.item")}</TableHead>
                  <TableHead>{t("migration.value")}</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {facts(overview, t).map((fact) => (
                  <TableRow key={fact.label}>
                    <TableCell className="text-muted-foreground">{fact.label}</TableCell>
                    <TableCell>{fact.value}</TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          ) : (
            <div className="space-y-2">{Array.from({ length: 6 }, (_, index) => <Skeleton key={index} className="h-6 w-full" />)}</div>
          )}
          {overview?.last_failure ? (
            <>
              <Separator />
              <p className="text-sm text-destructive">
                {label(t, failureLabelKeys, overview.last_failure.failure_code)}
                {overview.last_failure.rollback_required ? (t("migration.roll_back_to_the_pre_upgrade_artifact")) : null}
              </p>
            </>
          ) : null}
        </CardContent>
      </Card>
      <Card>
        <CardHeader><CardTitle>{t("migration.local_recovery_steps")}</CardTitle></CardHeader>
        <CardContent><RunbookSteps /></CardContent>
      </Card>
    </div>
  )
}
