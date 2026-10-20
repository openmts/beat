import { useState } from "react"
import { Alert, AlertDescription } from "@/components/ui/alert"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Sheet, SheetContent, SheetDescription, SheetHeader, SheetTitle } from "@/components/ui/sheet"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table"
import { Textarea } from "@/components/ui/textarea"
import { useAuth } from "@/context/auth"
import { useLocale } from "@/context/locale"
import { formatDateTime } from "@/lib/intl-format"
import { useConfirmedOperation } from "@/hooks/use-confirmed-operation"
import { useSecurityEvent } from "@/hooks/use-security-events"
import { acknowledgeSecurityEvent, getSecurityEventSource } from "@/lib/security-api"
import { OperationConfirm } from "@/pages/admin/operation-confirm"
import { securityStateVariant } from "@/pages/admin/security-status"
import { useSecurityEventsText, useSecurityLabel } from "@/pages/admin/security-events-text"
import type { SecurityEvent, SecurityEventDelivery, SecurityEventSource } from "@/types/security"

const noteLimit = 500

export function SecurityEventDetail({ id, revealSource, onClose, onAcknowledged }: {
  id: string; revealSource: boolean; onClose: () => void; onAcknowledged: () => void
}) {
  const t = useSecurityEventsText()
  const label = useSecurityLabel()
  const { locale } = useLocale()
  const detail = useSecurityEvent(id)
  const event = detail.data
  return (
    <Sheet open onOpenChange={open => { if (!open) onClose() }}>
      <SheetContent className="w-full overflow-y-auto p-6 sm:max-w-2xl">
        <SheetHeader>
          <SheetTitle>{event ? label("kind", event.kind) : t("details")}</SheetTitle>
          <SheetDescription>
            {event ? formatDateTime(locale, event.occurred_at) : t("loading")}
          </SheetDescription>
        </SheetHeader>
        {detail.error && <Alert variant="destructive" role="alert"><AlertDescription>{detail.error}</AlertDescription></Alert>}
        {event && <div className="mt-5 space-y-6">
          <EventFacts event={event} />
          {revealSource && <SourceReveal id={id} />}
          <DeliveryRows deliveries={event.deliveries ?? []} failures={event.delivery_failures} />
          <Acknowledgement event={event} onAcknowledged={() => { onAcknowledged(); void detail.refresh() }} />
        </div>}
      </SheetContent>
    </Sheet>
  )
}

function EventFacts({ event }: { event: SecurityEvent }) {
  const t = useSecurityEventsText()
  const label = useSecurityLabel()
  const { locale } = useLocale()
  const reasons = event.reasons.map(reason => label("reason", reason)).join(" / ")
  const facts: [string, string][] = [
    [t("severity"), label("severity", event.severity)],
    [t("actor"), event.actor_display_name || "—"],
    [t("auth_method"), label("auth", event.auth_method)],
    [t("source"), `${event.source_summary || "—"} · ${label("source", event.source_class)}`],
    [t("device"), event.device_summary || "—"],
    [t("country"), [event.country_code, event.region].filter(Boolean).join(" · ") || "—"],
    [t("reasons"), reasons || "—"],
    [t("routing"), label("routing", event.routing_state)],
    [t("retain_until"), formatDateTime(locale, event.retain_until)],
  ]
  return <dl className="grid grid-cols-1 gap-x-6 gap-y-3 text-sm sm:grid-cols-2">
    {facts.map(([term, value]) => (
      <div key={term}><dt className="text-muted-foreground">{term}</dt><dd className="mt-0.5 break-words">{value}</dd></div>
    ))}
  </dl>
}

function DeliveryRows({ deliveries, failures }: { deliveries: SecurityEventDelivery[]; failures: number }) {
  const t = useSecurityEventsText()
  const label = useSecurityLabel()
  const { locale } = useLocale()
  return <section className="space-y-3">
    <h3 className="text-sm font-semibold">{t("deliveries")}</h3>
    {failures > 0 && <Alert variant="destructive" role="alert"><AlertDescription>{t("attention")}</AlertDescription></Alert>}
    {!deliveries.length
      ? <p className="text-sm text-muted-foreground">{t("no_deliveries")}</p>
      : <div className="overflow-hidden rounded-md border"><Table><TableHeader><TableRow>
        <TableHead>{t("channel")}</TableHead><TableHead>{t("delivery_state")}</TableHead>
        <TableHead>{t("attempts")}</TableHead><TableHead>{t("error_code")}</TableHead><TableHead>{t("updated")}</TableHead>
      </TableRow></TableHeader><TableBody>{deliveries.map(delivery => (
        <TableRow key={delivery.delivery_id}>
          <TableCell className="max-w-40 truncate">{delivery.channel_name || "—"}</TableCell>
          <TableCell><Badge variant={securityStateVariant(delivery.state)}>{label("delivery", delivery.state)}</Badge></TableCell>
          <TableCell className="tabular-nums">{delivery.attempt_count}</TableCell>
          <TableCell className="font-mono text-xs">{delivery.error_code || "—"}</TableCell>
          <TableCell className="whitespace-nowrap">{formatDateTime(locale, delivery.updated_at)}</TableCell>
        </TableRow>
      ))}</TableBody></Table></div>}
  </section>
}

function Acknowledgement({ event, onAcknowledged }: { event: SecurityEvent; onAcknowledged: () => void }) {
  const t = useSecurityEventsText()
  const { principal } = useAuth()
  const { locale } = useLocale()
  const [note, setNote] = useState("")
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState("")
  const [notice, setNotice] = useState("")
  if (event.acknowledged_at) {
    const own = Boolean(principal && principal.user.id === event.acknowledged_by)
    return <section className="space-y-2 text-sm">
      <h3 className="text-sm font-semibold">{t("acked")}</h3>
      <dl className="grid grid-cols-1 gap-x-6 gap-y-3 sm:grid-cols-2">
        <div><dt className="text-muted-foreground">{t("acked_at")}</dt><dd className="mt-0.5">{formatDateTime(locale, event.acknowledged_at)}</dd></div>
        {own && <div><dt className="text-muted-foreground">{t("acked_by")}</dt><dd className="mt-0.5">{t("you")}</dd></div>}
        {event.acknowledged_note && <div className="sm:col-span-2"><dt className="text-muted-foreground">{t("acked_note")}</dt><dd className="mt-0.5 break-words">{event.acknowledged_note}</dd></div>}
      </dl>
    </section>
  }
  const submit = async () => {
    setBusy(true); setError("")
    try { await acknowledgeSecurityEvent(event.id, note.trim()); setNote(""); setNotice(t("ack_done")); onAcknowledged() }
    catch (cause) { setError(cause instanceof Error ? cause.message : "Unknown error") }
    finally { setBusy(false) }
  }
  return <section className="space-y-3">
    <h3 className="text-sm font-semibold">{t("ack_title")}</h3>
    <label htmlFor="security-ack-note" className="block text-sm text-muted-foreground">{t("ack_note")}</label>
    <Textarea id="security-ack-note" maxLength={noteLimit} rows={2} value={note} disabled={busy} onChange={value => setNote(value.target.value)} />
    {error && <Alert variant="destructive" role="alert"><AlertDescription>{error}</AlertDescription></Alert>}
    {notice && <Alert role="status"><AlertDescription>{notice}</AlertDescription></Alert>}
    <Button size="sm" disabled={busy} onClick={() => void submit()}>{t("ack_submit")}</Button>
  </section>
}

function SourceReveal({ id }: { id: string }) {
  const t = useSecurityEventsText()
  const { locale } = useLocale()
  const flow = useConfirmedOperation()
  const [source, setSource] = useState<SecurityEventSource | null>(null)
  return <section className="space-y-3">
    <h3 className="text-sm font-semibold">{t("reveal")}</h3>
    <p className="text-sm text-muted-foreground">{t("reveal_warning")}</p>
    {source
      ? <dl className="grid grid-cols-1 gap-x-6 gap-y-3 text-sm sm:grid-cols-2">
        <div><dt className="text-muted-foreground">{t("source_address")}</dt><dd className="mt-0.5 font-mono">{source.address}</dd></div>
        <div><dt className="text-muted-foreground">{t("source_captured")}</dt><dd className="mt-0.5">{formatDateTime(locale, source.captured_at)}</dd></div>
      </dl>
      : <Button size="sm" variant="outline" disabled={flow.busy}
        onClick={() => flow.request({ title: t("reveal"), description: t("reveal_description"),
          execute: async () => { setSource(await getSecurityEventSource(id)) } })}>{t("reveal")}</Button>}
    <OperationConfirm flow={flow} />
  </section>
}
