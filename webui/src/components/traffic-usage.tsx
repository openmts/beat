import { ArrowDownIcon, ArrowUpIcon, GaugeIcon } from "lucide-react"

import { Badge } from "@/components/ui/badge"
import { Progress } from "@/components/ui/progress"
import { useLocale } from "@/context/locale"
import { formatBytes } from "@/lib/metric-format"
import type { TrafficStatus, TrafficSummary } from "@/types"
import { dateTimeFormatter } from "@/lib/intl-format"

interface TrafficUsageProps {
  traffic?: TrafficSummary
  className?: string
  showBreakdown?: boolean
}

const statusVariants: Record<TrafficStatus, "default" | "secondary" | "destructive" | "outline"> = {
  unlimited: "outline",
  normal: "secondary",
  warning: "outline",
  critical: "destructive",
  exceeded: "destructive",
}

export function TrafficUsage({ traffic, className, showBreakdown = true }: TrafficUsageProps) {
  const { locale, t } = useLocale()
  if (!traffic) return null

  const isUnlimited = traffic.limit === 0
  const percentage = Math.max(0, Math.min(traffic.percentage ?? 0, 100))
  const status = traffic.status || "unlimited"
  const value = isUnlimited
    ? `${formatBytes(traffic.used)} · ${t("traffic.unlimited")}`
    : `${formatBytes(traffic.used)} / ${formatBytes(traffic.limit)}`
  const quotaDetail = isUnlimited || traffic.percentage === null || traffic.remaining === null
    ? null
    : `${traffic.percentage.toFixed(1)}% · ${formatBytes(traffic.remaining)} ${t("traffic.remaining")}`
  const adjusted = traffic.accounting_mode === "calibrated"
  const adjustedTitle = adjusted
    ? `${t("traffic.adjusted_hint")}${traffic.calibrated_at ? ` · ${formatDate(traffic.calibrated_at, locale)}` : ""}`
    : undefined

  return (
    <section className={className} aria-label={t("traffic.current_cycle")}>
      <div className="flex items-center justify-between gap-2">
        <p className="text-xs font-medium">{t("traffic.current_cycle")}</p>
        <div className="flex items-center gap-1">
          {adjusted ? (
            <Badge variant="outline" className="gap-1" title={adjustedTitle}>
              <GaugeIcon className="size-3" />{t("traffic.adjusted")}
            </Badge>
          ) : null}
          <Badge variant={statusVariants[status]}>
            {t(`traffic.${status}`)}
          </Badge>
        </div>
      </div>
      <p className="mt-1 font-medium tabular-nums">{value}</p>
      {quotaDetail ? <p className="text-xs text-muted-foreground tabular-nums">{quotaDetail}</p> : null}
      {isUnlimited ? null : (
        <Progress className="mt-2" value={percentage} aria-label={`${percentage.toFixed(1)}%`} />
      )}
      <div className="mt-2 flex flex-wrap gap-x-3 gap-y-1 text-xs text-muted-foreground">
        {showBreakdown ? (
          <>
            <span className="flex items-center gap-1 tabular-nums">
              <ArrowUpIcon className="size-3" />{formatBytes(traffic.sent)}
            </span>
            <span className="flex items-center gap-1 tabular-nums">
              <ArrowDownIcon className="size-3" />{formatBytes(traffic.received)}
            </span>
          </>
        ) : null}
        <span>{t("traffic.resets")} {formatDate(traffic.next_reset, locale)}</span>
      </div>
      {traffic.tracked_since ? null : (
        <p className="mt-1 text-xs text-muted-foreground">{t("traffic.no_data")}</p>
      )}
    </section>
  )
}

function formatDate(value: string, locale: string): string {
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return "--"
  return dateTimeFormatter(locale, { year: "numeric", month: "short", day: "numeric" }).format(date)
}
