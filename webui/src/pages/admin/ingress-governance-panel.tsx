import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Progress } from "@/components/ui/progress"
import { Skeleton } from "@/components/ui/skeleton"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table"
import { useLocale } from "@/context/locale"
import { useOperationResource } from "@/hooks/use-operation-resource"
import { getRuntimeStatus } from "@/lib/runtime-api"
import { isRuntimeStatus } from "./operation-runtime"
import type { IngressClass, IngressSnapshot } from "@/types/runtime"

const refreshMilliseconds = 10000
// 拒绝原因只用于分类，不携带任何客户端身份。
const classLabelKeys: Record<string, string> = {
  public_static: "ingress.class.public_static",
  public_snapshot: "ingress.class.public_snapshot",
  public_history: "ingress.class.public_history",
  auth_kdf: "ingress.class.auth_kdf",
  agent_read: "ingress.class.agent_read",
  agent_write: "ingress.class.agent_write",
  admin_read: "ingress.class.admin_read",
  admin_write: "ingress.class.admin_write",
  admin_expensive: "ingress.class.admin_expensive",
}
const reasonLabelKeys: Record<string, string> = {
  class_rate: "ingress.reason.class_rate",
  identity_rate: "ingress.reason.identity_rate",
  class_busy: "ingress.reason.class_busy",
  identity_busy: "ingress.reason.identity_busy",
  table_full: "ingress.reason.table_full",
}

type Translate = (key: string, params?: Record<string, string | number>) => string

function label(t: Translate, keys: Record<string, string>, value: string) {
  return t(keys[value] ?? value)
}

function percent(value: number, bound: number) {
  if (!Number.isFinite(value) || !Number.isFinite(bound) || bound <= 0) return 0
  return Math.max(0, Math.min(100, Math.round((value / bound) * 100)))
}

export function IngressGovernancePanel() {
  const { t } = useLocale()
  const resource = useOperationResource(getRuntimeStatus, refreshMilliseconds)
  const status = isRuntimeStatus(resource.data) ? resource.data : undefined
  const ingress = status?.ingress
  return (
    <section className="space-y-4">
      <header className="flex flex-wrap items-start justify-between gap-3">
        <div>
          <h2 className="text-lg font-semibold">{t("ingress.ingress_admission")}</h2>
          <p className="mt-1 text-sm text-muted-foreground">
            {t("ingress.read_only_ingress_budgets_fixed_route_classes_ef")}
          </p>
        </div>
        <Button variant="outline" size="sm" onClick={resource.reload}>{t("ingress.refresh")}</Button>
      </header>
      <Alert>
        <AlertTitle>{t("ingress.in_memory_budgets_restart_to_change")}</AlertTitle>
        <AlertDescription>
          {t("ingress.budget_state_lives_in_server_memory_and_the_brow")}
        </AlertDescription>
      </Alert>
      {resource.error && <Alert variant="destructive"><AlertDescription>{t("ingress.ingress_status_is_unavailable_try_again_later")}</AlertDescription></Alert>}
      {!status && resource.loading && <div role="status" aria-label={t("ingress.loading_ingress_status")} className="space-y-2"><Skeleton className="h-6 w-40" /><Skeleton className="h-32 w-full" /></div>}
      {status && !ingress && <Alert variant="destructive"><AlertDescription>{t("ingress.the_server_did_not_enable_ingress_admission")}</AlertDescription></Alert>}
      {ingress && <IngressTable ingress={ingress} />}
    </section>
  )
}

function IngressTable({ ingress }: { ingress: IngressSnapshot }) {
  const { t } = useLocale()
  return (
    <div className="space-y-3">
      <p className="text-sm text-muted-foreground">
        {t("ingress.identity_table_summary", {
          entries: ingress.identity_entries,
          limit: ingress.identity_entries_limit,
          seconds: Math.round(ingress.identity_ttl_seconds),
        })}
      </p>
      <div className="overflow-auto">
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>{t("ingress.class")}</TableHead>
              <TableHead>{t("ingress.rate_global_identity")}</TableHead>
              <TableHead>{t("ingress.burst_global_identity")}</TableHead>
              <TableHead>{t("ingress.in_flight_now_limit")}</TableHead>
              <TableHead>{t("ingress.admitted")}</TableHead>
              <TableHead>{t("ingress.rejected")}</TableHead>
              <TableHead>{t("ingress.rejection_reasons")}</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {ingress.classes.map((entry) => <ClassRow key={entry.class} entry={entry} />)}
          </TableBody>
        </Table>
      </div>
    </div>
  )
}

function ClassRow({ entry }: { entry: IngressClass }) {
  const { t } = useLocale()
  const busy = entry.reasons.filter((reason) => reason.count > 0)
  return (
    <TableRow>
      <TableCell className="font-medium">{label(t, classLabelKeys, entry.class)}</TableCell>
      <TableCell className="font-mono text-xs tabular-nums">
        {entry.limits.global_rate_per_second} / {entry.limits.identity_rate_per_second}
      </TableCell>
      <TableCell className="font-mono text-xs tabular-nums">
        {entry.limits.global_burst} / {entry.limits.identity_burst}
      </TableCell>
      <TableCell className="min-w-40">
        <div className="space-y-1">
          <span className="font-mono text-xs tabular-nums">{entry.in_flight} / {entry.limits.global_in_flight}</span>
          <Progress value={percent(entry.in_flight, entry.limits.global_in_flight)} aria-label={t("ingress.in_flight_usage")} />
        </div>
      </TableCell>
      <TableCell className="font-mono text-xs tabular-nums">{entry.admitted}</TableCell>
      <TableCell className="font-mono text-xs tabular-nums">{entry.rejected}</TableCell>
      <TableCell>
        {busy.length === 0
          ? <span className="text-xs text-muted-foreground">{t("ingress.none")}</span>
          : <div className="flex flex-wrap gap-1">{busy.map((reason) => (
            <Badge key={reason.reason} variant="secondary">
              {label(t, reasonLabelKeys, reason.reason)} {reason.count}
            </Badge>
          ))}</div>}
      </TableCell>
    </TableRow>
  )
}
