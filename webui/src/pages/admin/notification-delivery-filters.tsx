import { RefreshCwIcon } from "lucide-react"
import { Button } from "@/components/ui/button"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { useLocale } from "@/context/locale"
import type { AlertChannel } from "@/types"
import type { DeliveryFilter, DeliveryState } from "@/types/notification"

export function NotificationDeliveryFilters({ filter, channels, onChange, loading, refresh }: {
  filter: DeliveryFilter; channels: AlertChannel[]; onChange: (update: Partial<DeliveryFilter>) => void; loading: boolean; refresh: () => void
}) {
  const { t } = useLocale()
  const states = ["", "pending", "leased", "succeeded", "dead", "cancelled"].map(value => ({ value, label: t(value ? `notification.state.${value}` : "notification.all_states") }))
  const channelOptions = [{ value: "", label: t("notification.all_channels") }, ...channels.map(channel => ({ value: channel.id, label: channel.name }))]
  if (filter.channel_id && !channels.some(channel => channel.id === filter.channel_id)) channelOptions.push({ value: filter.channel_id, label: t("notification.deleted_channel") })
  return (
    <div className="flex flex-wrap items-end gap-3">
      <div className="grid min-w-44 gap-1"><label htmlFor="delivery-channel" className="text-sm">{t("alert.channels")}</label>
        <Select items={channelOptions} value={filter.channel_id ?? ""} onValueChange={value => onChange({ channel_id: value ?? "" })}>
          <SelectTrigger id="delivery-channel" className="w-full"><SelectValue /></SelectTrigger><SelectContent>{channelOptions.map(item => <SelectItem key={item.value} value={item.value}>{item.label}</SelectItem>)}</SelectContent>
        </Select></div>
      <div className="grid min-w-40 gap-1"><label htmlFor="delivery-state" className="text-sm">{t("notification.status")}</label>
        <Select items={states} value={filter.state ?? ""} onValueChange={value => onChange({ state: value ? value as DeliveryState : undefined })}>
          <SelectTrigger id="delivery-state" className="w-full"><SelectValue /></SelectTrigger><SelectContent>{states.map(item => <SelectItem key={item.value} value={item.value}>{item.label}</SelectItem>)}</SelectContent>
        </Select></div>
      <Button variant="outline" disabled={loading} onClick={refresh}><RefreshCwIcon />{t("notification.refresh")}</Button>
      <p className="pb-2 text-xs text-muted-foreground">{t("notification.auto_refresh")}</p>
    </div>
  )
}
