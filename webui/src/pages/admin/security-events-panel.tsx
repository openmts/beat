import { useState } from "react"
import { Alert, AlertDescription } from "@/components/ui/alert"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table"
import { useAuth } from "@/context/auth"
import { useLocale } from "@/context/locale"
import { formatDateTime } from "@/lib/intl-format"
import { useSecurityEvents, useSecurityOptions, useSecurityPolicy } from "@/hooks/use-security-events"
import { SecurityEventDetail } from "@/pages/admin/security-event-detail"
import { SecurityPolicyEditor } from "@/pages/admin/security-policy-editor"
import { securityDeliveryAttention, securitySeverityVariant, securityStateVariant } from "@/pages/admin/security-status"
import { useSecurityEventsText, useSecurityLabel } from "@/pages/admin/security-events-text"
import type { SecurityEvent, SecurityEventFilter, SecurityOption } from "@/types/security"

const pageSize = 25

const fallbackValues: Record<string, string[]> = {
  kind: ["security.login_succeeded", "security.login_suspicious"],
  severity: ["info", "medium", "high"],
  reason: ["rate_limit_lockout", "repeated_invalid_totp", "new_source_prefix", "new_device_family", "disabled_or_revoked_identity", "concurrent_distant_source"],
  source: ["public", "private", "loopback", "link_local", "unknown"],
  delivery: ["pending", "delivered", "failed", "none"],
}

export function SecurityEventsPanel() {
  const t = useSecurityEventsText()
  const label = useSecurityLabel()
  const { locale } = useLocale()
  const { principal } = useAuth()
  const [filter, setFilter] = useState<SecurityEventFilter>({ limit: pageSize, offset: 0 })
  const [selected, setSelected] = useState<string | null>(null)
  const list = useSecurityEvents(filter)
  const owner = principal?.user.role === "owner"
  const policy = useSecurityPolicy(owner)
  const offset = filter.offset ?? 0
  const update = (patch: Partial<SecurityEventFilter>) => setFilter(current => ({ ...current, ...patch, offset: 0 }))
  return <div className="grid gap-4">
    <p className="max-w-3xl text-sm text-muted-foreground">{t("description")}</p>
    <SecurityEventFilters filter={filter} onChange={update} loading={list.loading} refresh={() => void list.refresh()} />
    {list.error && <Alert variant="destructive" role="alert"><AlertDescription>{list.error}</AlertDescription></Alert>}
    <div className="overflow-hidden rounded-md border"><Table><TableHeader><TableRow>
      <TableHead>{t("time")}</TableHead><TableHead>{t("event")}</TableHead><TableHead>{t("severity")}</TableHead>
      <TableHead>{t("actor")}</TableHead><TableHead>{t("source")}</TableHead><TableHead>{t("device")}</TableHead>
      <TableHead>{t("reasons")}</TableHead><TableHead>{t("delivery")}</TableHead><TableHead>{t("details")}</TableHead>
    </TableRow></TableHeader><TableBody>
      {(list.data?.events ?? []).map(event => <TableRow key={event.id}>
        <TableCell className="whitespace-nowrap">{formatDateTime(locale, event.occurred_at)}</TableCell>
        <TableCell>{label("kind", event.kind)}</TableCell>
        <TableCell><Badge variant={securitySeverityVariant(event.severity)}>{label("severity", event.severity)}</Badge></TableCell>
        <TableCell className="max-w-40 truncate">{event.actor_display_name || "—"}</TableCell>
        <TableCell className="max-w-48 truncate">{event.source_summary || label("source", event.source_class)}</TableCell>
        <TableCell className="max-w-48 truncate">{event.device_summary || "—"}</TableCell>
        <TableCell className="max-w-48 truncate">{event.reasons.map(reason => label("reason", reason)).join(" / ") || "—"}</TableCell>
        <TableCell><DeliveryCell event={event} /></TableCell>
        <TableCell><Button size="sm" variant="outline" aria-label={`${t("details")}: ${label("kind", event.kind)}`} onClick={() => setSelected(event.id)}>{t("details")}</Button></TableCell>
      </TableRow>)}
      {!list.data?.events.length && <TableRow><TableCell colSpan={9} className="py-8 text-center text-muted-foreground">{t(list.loading ? "loading" : "empty")}</TableCell></TableRow>}
    </TableBody></Table></div>
    <div className="flex flex-wrap items-center justify-between gap-3 text-sm">
      <span>{t("total")}: <span className="tabular-nums">{list.data?.total ?? 0}</span></span>
      <div className="flex gap-2">
        <Button variant="outline" disabled={offset === 0 || list.loading}
          onClick={() => setFilter(current => ({ ...current, offset: Math.max(0, offset - pageSize) }))}>{t("previous")}</Button>
        <Button variant="outline" disabled={!list.data || offset + pageSize >= list.data.total || list.loading}
          onClick={() => setFilter(current => ({ ...current, offset: offset + pageSize }))}>{t("next")}</Button>
      </div>
    </div>
    {selected && <SecurityEventDetail id={selected} revealSource={policy.data?.privacy_level === "full_source"}
      onClose={() => setSelected(null)} onAcknowledged={() => void list.refresh()} />}
    {owner && <SecurityPolicyEditor settings={policy.data} error={policy.error ?? ""} onSaved={() => void policy.refresh()} />}
  </div>
}

function DeliveryCell({ event }: { event: SecurityEvent }) {
  const t = useSecurityEventsText()
  const label = useSecurityLabel()
  return <div className="grid justify-items-start gap-1">
    <Badge variant={securityStateVariant(event.delivery_state)}>{label("delivery", event.delivery_state)}</Badge>
    {securityDeliveryAttention(event) && <span className="text-xs text-destructive">{t("attention")}</span>}
  </div>
}

function SecurityEventFilters({ filter, onChange, loading, refresh }: {
  filter: SecurityEventFilter
  onChange: (patch: Partial<SecurityEventFilter>) => void
  loading: boolean
  refresh: () => void
}) {
  const t = useSecurityEventsText()
  const label = useSecurityLabel()
  const options = useSecurityOptions()
  const itemsFor = (group: string, server: SecurityOption[] | undefined) => {
    const values = server?.length ? server : fallbackValues[group].map(value => ({ value, label: value }))
    return [{ value: "", label: t("all") }, ...values.map(entry => ({ value: entry.value, label: label(group, entry.value, entry.label) }))]
  }
  const selects: { id: string; labelKey: "filter_kind" | "filter_severity" | "filter_reason" | "filter_source" | "filter_delivery"; key: keyof SecurityEventFilter; items: SecurityOption[] }[] = [
    { id: "security-filter-kind", labelKey: "filter_kind", key: "kind", items: itemsFor("kind", options.data?.kinds) },
    { id: "security-filter-severity", labelKey: "filter_severity", key: "severity", items: itemsFor("severity", options.data?.severities) },
    { id: "security-filter-reason", labelKey: "filter_reason", key: "reason", items: itemsFor("reason", options.data?.reasons) },
    { id: "security-filter-source", labelKey: "filter_source", key: "source", items: itemsFor("source", options.data?.sources) },
    { id: "security-filter-delivery", labelKey: "filter_delivery", key: "delivery", items: itemsFor("delivery", options.data?.deliveries) },
  ]
  return <div className="flex flex-wrap items-end gap-3">
    {selects.map(select => <div key={select.id} className="grid min-w-40 gap-1">
      <label htmlFor={select.id} className="text-sm">{t(select.labelKey)}</label>
      <Select items={select.items} value={String(filter[select.key] ?? "")}
        onValueChange={value => onChange({ [select.key]: value ?? "" })}>
        <SelectTrigger id={select.id} className="w-full"><SelectValue /></SelectTrigger>
        <SelectContent>{select.items.map(item => <SelectItem key={item.value} value={item.value}>{item.label}</SelectItem>)}</SelectContent>
      </Select>
    </div>)}
    <Button variant="outline" disabled={loading} onClick={refresh}>{t("refresh")}</Button>
    <p className="pb-2 text-xs text-muted-foreground">{t("auto_refresh")}</p>
  </div>
}
