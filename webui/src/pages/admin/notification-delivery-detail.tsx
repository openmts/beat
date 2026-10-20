import { useLocale } from "@/context/locale"
import { formatDateTime } from "@/lib/intl-format"
import { useNotificationDelivery } from "@/hooks/use-notification-deliveries"
import { Alert, AlertDescription } from "@/components/ui/alert"
import { Button } from "@/components/ui/button"
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { NotificationStateBadge } from "@/pages/admin/notification-status"
import type { NotificationDelivery } from "@/types/notification"

export function NotificationDeliveryDetail(props: { id: string; close: () => void; owner: boolean; busy: boolean; retry: (delivery: NotificationDelivery) => void; cancel: (delivery: NotificationDelivery) => void }) {
  const { t, locale } = useLocale()
  const detail = useNotificationDelivery(props.id)
  const delivery = detail.data
  return <Dialog open onOpenChange={open => { if (!open) props.close() }}><DialogContent className="max-h-[90dvh] overflow-y-auto sm:max-w-2xl">
    <DialogHeader><DialogTitle>{t("notification.delivery_detail")}</DialogTitle><DialogDescription>{t("notification.history_help")}</DialogDescription></DialogHeader>
    {detail.error && <Alert variant="destructive"><AlertDescription>{detail.error}</AlertDescription></Alert>}
    {delivery ? <div className="grid gap-4">
      <div className="flex flex-wrap items-center justify-between gap-3"><div><p className="font-medium">{delivery.channel_name}</p><p className="text-sm text-muted-foreground">{t(`notification.event.${delivery.event_kind}`)}</p></div><NotificationStateBadge state={delivery.state} /></div>
      <dl className="grid gap-3 rounded-md border p-4 text-sm sm:grid-cols-2">
        <div><dt className="text-muted-foreground">{t("notification.created_at")}</dt><dd>{formatDateTime(locale, delivery.created_at)}</dd></div>
        <div><dt className="text-muted-foreground">{t("notification.channel_revision")}</dt><dd>{delivery.channel_revision}</dd></div>
        <div className="sm:col-span-2"><dt className="text-muted-foreground">{t("notification.delivery_id")}</dt><dd className="break-all font-mono text-xs">{delivery.id}</dd></div>
        {delivery.retry_of && <div className="sm:col-span-2"><dt className="text-muted-foreground">{t("notification.retry_of")}</dt><dd className="break-all font-mono text-xs">{delivery.retry_of}</dd></div>}
      </dl>
      <ol className="grid gap-3" aria-label={t("notification.attempts")}>
        {(delivery.attempts ?? []).map(attempt => <li key={attempt.number} className="grid gap-1 border-l-2 pl-4 text-sm">
          <div className="flex justify-between gap-2"><span className="font-medium">{t("notification.attempt_number", { number: attempt.number })}</span><span>{attempt.duration_ms} ms</span></div>
          <span className="text-xs text-muted-foreground">{formatDateTime(locale, attempt.started_at)}</span>
          <span>{t(`notification.attempt.${attempt.outcome}`)}</span>
          {attempt.error_code && <span className="text-xs text-destructive">{attempt.error_stage}: {attempt.error_code} {attempt.status_class}</span>}
        </li>)}
      </ol>
      {!delivery.attempts?.length && <p className="text-sm text-muted-foreground">{t("notification.no_attempts")}</p>}
      {props.owner && <div className="flex flex-wrap gap-2">
        {delivery.state === "dead" && <Button disabled={props.busy} onClick={() => props.retry(delivery)}>{t("notification.retry")}</Button>}
        {(delivery.state === "pending" || delivery.state === "leased") && <Button variant="outline" disabled={props.busy} onClick={() => props.cancel(delivery)}>{t("notification.cancel_delivery")}</Button>}
      </div>}
    </div> : <p role="status" className="text-sm text-muted-foreground">{t(detail.loading ? "notification.loading" : "notification.unavailable")}</p>}
  </DialogContent></Dialog>
}
