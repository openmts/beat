import {
  ActivityIcon,
  ArrowDownIcon,
  ArrowUpIcon,
  ClockIcon,
  GlobeIcon,
  HistoryIcon,
  NetworkIcon,
  PencilIcon,
  RadioTowerIcon,
  Trash2Icon,
  UsersIcon,
} from "lucide-react"

import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card, CardAction, CardContent, CardDescription, CardFooter, CardHeader, CardTitle } from "@/components/ui/card"
import { useLocale } from "@/context/locale"
import {
  formatLatency,
  formatNetworkInterval,
  latestSuccessPercent,
  networkTypeKey,
} from "@/lib/network-quality"
import type { NetworkProbeType, NetworkTaskView } from "@/types"

const typeIcons = {
  icmp: RadioTowerIcon,
  tcp: NetworkIcon,
  http: GlobeIcon,
} satisfies Record<NetworkProbeType, typeof ActivityIcon>

interface NetworkTaskCardProps {
  view: NetworkTaskView
  first: boolean
  last: boolean
  onEdit: () => void
  onDelete: () => void
  onHistory: () => void
  onMoveUp: () => void
  onMoveDown: () => void
}

export function NetworkTaskCard(props: NetworkTaskCardProps) {
  const { t } = useLocale()
  const { view } = props
  const TypeIcon = typeIcons[view.task.type]
  const latest = view.nodes.find((state) => state.latest !== null)?.latest
  const success = latestSuccessPercent(view)

  return (
    <Card size="sm">
      <CardHeader>
        <div className="flex min-w-0 items-center gap-2">
          <TypeIcon className="size-4 shrink-0 text-muted-foreground" />
          <CardTitle className="truncate">{view.task.name}</CardTitle>
        </div>
        <CardDescription className="truncate" title={view.task.target}>{view.task.target}</CardDescription>
        <CardAction>
          <Badge variant={view.task.enabled ? "secondary" : "outline"}>
            {view.task.enabled ? t("alert.enabled") : t("alert.disabled")}
          </Badge>
        </CardAction>
      </CardHeader>
      <CardContent className="grid grid-cols-2 gap-x-4 gap-y-3">
        <TaskFact icon={ActivityIcon} label={t("app.type")} value={t(networkTypeKey(view.task.type))} />
        <TaskFact
          icon={ClockIcon}
          label={t("network.schedule")}
          value={`${formatNetworkInterval(view.task.interval_seconds)} / ${view.task.timeout_milliseconds}ms`}
        />
        <TaskFact
          icon={UsersIcon}
          label={t("network.assignment")}
          value={view.task.all_nodes ? t("network.all_nodes") : t("network.node_count").replace("{count}", String(view.nodes.length))}
        />
        <TaskFact
          icon={GlobeIcon}
          label={t("network.visibility")}
          value={view.task.is_public ? t("network.public") : t("network.private")}
        />
        <TaskFact
          icon={ActivityIcon}
          label={t("network.latest_latency")}
          value={latest ? formatLatency(latest.latency_ms) : t("network.waiting")}
        />
        <TaskFact
          icon={ActivityIcon}
          label={t("network.latest_success")}
          value={success === null ? t("network.waiting") : `${Math.round(success)}%`}
        />
      </CardContent>
      <CardFooter className="justify-between gap-2">
        <div className="flex gap-1">
          <IconButton label={t("network.move_up")} disabled={props.first} onClick={props.onMoveUp} icon={ArrowUpIcon} />
          <IconButton label={t("network.move_down")} disabled={props.last} onClick={props.onMoveDown} icon={ArrowDownIcon} />
        </div>
        <div className="flex gap-1">
          <IconButton label={t("network.history")} onClick={props.onHistory} icon={HistoryIcon} />
          <IconButton label={t("app.edit")} onClick={props.onEdit} icon={PencilIcon} />
          <IconButton label={t("app.delete")} onClick={props.onDelete} icon={Trash2Icon} destructive />
        </div>
      </CardFooter>
    </Card>
  )
}

function TaskFact({ icon: Icon, label, value }: { icon: typeof ActivityIcon; label: string; value: string }) {
  return (
    <div className="min-w-0">
      <span className="flex items-center gap-1 text-xs text-muted-foreground"><Icon className="size-3" />{label}</span>
      <span className="mt-0.5 block truncate text-sm" title={value}>{value}</span>
    </div>
  )
}

function IconButton({
  label,
  icon: Icon,
  destructive = false,
  ...props
}: Omit<React.ComponentProps<typeof Button>, "children"> & {
  label: string
  icon: typeof ActivityIcon
  destructive?: boolean
}) {
  return (
    <Button
      variant={destructive ? "destructive" : "ghost"}
      size="icon-sm"
      aria-label={label}
      title={label}
      {...props}
    >
      <Icon />
    </Button>
  )
}
