import { NotificationDeliveryFilters } from "@/pages/admin/notification-delivery-filters"
import { useState } from "react"
import { useAlertChannels } from "@/hooks/use-api"
import { useNotificationDeliveries } from "@/hooks/use-notification-deliveries"
import { useNotificationOwnerAction } from "@/hooks/use-notification-owner-action"
import { cancelNotificationDelivery, retryNotificationDelivery } from "@/lib/notification-api"
import { useLocale } from "@/context/locale"
import { formatDateTime } from "@/lib/intl-format"
import { Alert, AlertDescription } from "@/components/ui/alert"
import { Button } from "@/components/ui/button"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table"
import { NotificationConfirmDialog } from "@/pages/admin/notification-confirm-dialog"
import { NotificationDeliveryDetail } from "@/pages/admin/notification-delivery-detail"
import { NotificationStateBadge } from "@/pages/admin/notification-status"
import type { DeliveryFilter, NotificationDelivery } from "@/types/notification"

const pageSize = 25

export default function NotificationDeliveriesPanel({ initialChannel = "" }: { initialChannel?: string }) {
  const { t, locale } = useLocale()
  const [filter, setFilter] = useState<DeliveryFilter>({ channel_id: initialChannel, offset: 0, limit: pageSize })
  const [selected, setSelected] = useState<string | null>(null)
  const [notice, setNotice] = useState("")
  const list = useNotificationDeliveries(filter)
  const channels = useAlertChannels()
  const owner = useNotificationOwnerAction()
  const retry = (delivery: NotificationDelivery) => owner.request({ title: t("notification.retry"), description: t("notification.retry_help"), duplicate: true,
    execute: async () => { const retried = await retryNotificationDelivery(delivery.id); setSelected(retried.id); setNotice(t("notification.retry_result", { state: t(`notification.state.${retried.state}`) })); await list.refresh() } })
  const cancel = (delivery: NotificationDelivery) => owner.request({ title: t("notification.cancel_delivery"), description: t("notification.cancel_help"), confirm: true,
    execute: async () => { await cancelNotificationDelivery(delivery.id); setSelected(null); setNotice(t("notification.cancelled")); await list.refresh() } })
  const offset = filter.offset ?? 0
  return <div className="grid gap-4">
    <p className="max-w-3xl text-sm text-muted-foreground">{t("notification.history_help")}</p>
    <NotificationDeliveryFilters filter={filter} channels={channels.data ?? []} onChange={update => setFilter(current => ({ ...current, ...update, offset: 0 }))} loading={list.loading} refresh={() => void list.refresh()} />
    {(list.error || channels.error || owner.error) && <Alert variant="destructive" role="alert"><AlertDescription>{list.error || channels.error || owner.error}</AlertDescription></Alert>}
    {notice && <Alert role="status"><AlertDescription>{notice}</AlertDescription></Alert>}
    <div className="overflow-hidden rounded-md border"><Table><TableHeader><TableRow>
      <TableHead>{t("notification.created_at")}</TableHead><TableHead>{t("alert.channels")}</TableHead><TableHead>{t("notification.event")}</TableHead>
      <TableHead>{t("notification.status")}</TableHead><TableHead>{t("notification.attempts")}</TableHead><TableHead>{t("notification.next_attempt")}</TableHead><TableHead>{t("app.actions")}</TableHead>
    </TableRow></TableHeader><TableBody>
      {list.data?.items.map(delivery => <TableRow key={delivery.id}>
        <TableCell className="whitespace-nowrap">{formatDateTime(locale, delivery.created_at)}</TableCell><TableCell>{delivery.channel_name}</TableCell>
        <TableCell>{t(`notification.event.${delivery.event_kind}`)}</TableCell><TableCell><NotificationStateBadge state={delivery.state} /></TableCell><TableCell>{delivery.attempt_count}</TableCell>
        <TableCell>{delivery.state === "pending" ? formatDateTime(locale, delivery.next_attempt_at) : "—"}</TableCell>
        <TableCell><Button size="sm" variant="outline" onClick={() => setSelected(delivery.id)}>{t("notification.details")}</Button></TableCell>
      </TableRow>)}
      {!list.data?.items.length && <TableRow><TableCell colSpan={7} className="py-8 text-center text-muted-foreground">{t(list.loading ? "notification.loading" : "notification.no_deliveries")}</TableCell></TableRow>}
    </TableBody></Table></div>
    <div className="flex flex-wrap items-center justify-between gap-3 text-sm"><span>{t("notification.total", { count: list.data?.total ?? 0 })}</span><div className="flex gap-2">
      <Button variant="outline" disabled={offset === 0 || list.loading} onClick={() => setFilter(current => ({ ...current, offset: Math.max(0, offset - pageSize) }))}>{t("notification.previous")}</Button>
      <Button variant="outline" disabled={!list.data || offset + pageSize >= list.data.total || list.loading} onClick={() => setFilter(current => ({ ...current, offset: offset + pageSize }))}>{t("notification.next")}</Button>
    </div></div>
    {selected && <NotificationDeliveryDetail id={selected} close={() => setSelected(null)} owner={owner.owner} busy={owner.busy} retry={retry} cancel={cancel} />}
    <NotificationConfirmDialog flow={owner} />
  </div>
}
