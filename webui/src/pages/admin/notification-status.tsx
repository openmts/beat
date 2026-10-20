import { Badge } from "@/components/ui/badge"
import { useLocale } from "@/context/locale"
import { formatDateTime } from "@/lib/intl-format"
import type { AlertChannel } from "@/types"
import type { DeliveryState } from "@/types/notification"

export function NotificationStateBadge({ state }: { state: DeliveryState | "not_routed" | string }) {
  const { t } = useLocale()
  const failed = state === "dead" || state === "open"
  return <Badge variant={failed ? "destructive" : state === "succeeded" ? "secondary" : "outline"}>{t(`notification.state.${state}`)}</Badge>
}

export function NotificationChannelHealth({ channel }: { channel: AlertChannel }) {
  const { t, locale } = useLocale()
  const health = channel.health
  const state = !channel.enabled ? "paused" : health.circuit === "open" ? "open" : health.consecutive_failures ? "retrying" : "ready"
  return <div className="grid justify-items-start gap-1">
    <NotificationStateBadge state={state} />
    <span className="text-xs text-muted-foreground">{t("notification.queue_depth", { count: health.queue_depth })}</span>
    {health.open_until && state === "open" && <span className="text-xs text-muted-foreground">{t("notification.resume_at", { time: formatDateTime(locale, health.open_until) })}</span>}
    {health.error_code && <span className="text-xs text-destructive">{health.error_stage}: {health.error_code}</span>}
    {health.last_success && <span className="text-xs text-muted-foreground">{t("notification.last_success", { time: formatDateTime(locale, health.last_success) })}</span>}
  </div>
}
