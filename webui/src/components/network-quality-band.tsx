import { useState } from "react"
import { ActivityIcon, GlobeIcon, NetworkIcon, RadioTowerIcon } from "lucide-react"

import { NetworkHistoryDialog } from "@/components/network-history-dialog"
import { Alert, AlertDescription } from "@/components/ui/alert"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Separator } from "@/components/ui/separator"
import { Skeleton } from "@/components/ui/skeleton"
import { useLocale } from "@/context/locale"
import { usePublicNetworkQuality } from "@/hooks/use-network"
import {
  formatLatency,
  latencyRailPercent,
  networkNodeLabel,
  networkTypeKey,
} from "@/lib/network-quality"
import { cn } from "@/lib/utils"
import type { NetworkNodeState, NetworkProbeType, NetworkTaskView } from "@/types"

const typeIcons = {
  icmp: RadioTowerIcon,
  tcp: NetworkIcon,
  http: GlobeIcon,
} satisfies Record<NetworkProbeType, typeof ActivityIcon>

export function NetworkQualityBand() {
  const { t } = useLocale()
  const { data, loading, error } = usePublicNetworkQuality()
  const [historyView, setHistoryView] = useState<NetworkTaskView | null>(null)
  const [historyNodeId, setHistoryNodeId] = useState<string>()

  const openHistory = (view: NetworkTaskView, nodeId: string) => {
    setHistoryView(view)
    setHistoryNodeId(nodeId)
  }

  return (
    <section aria-labelledby="network-quality-title" className="mt-8 flex flex-col gap-4 border-y py-5">
      <div className="flex items-center gap-2">
        <ActivityIcon className="size-5" />
        <h2 id="network-quality-title" className="text-base font-semibold">{t("network.quality")}</h2>
      </div>
      {error ? (
        <Alert variant="destructive"><AlertDescription>{error}</AlertDescription></Alert>
      ) : loading ? (
        <NetworkQualitySkeleton />
      ) : (data ?? []).length === 0 ? (
        <div className="flex min-h-24 items-center justify-center text-sm text-muted-foreground">
          {t("network.no_public_tasks")}
        </div>
      ) : (
        <div className="flex flex-col">
          {(data ?? []).map((view, index) => (
            <div key={view.task.id}>
              {index > 0 && <Separator />}
              <NetworkTaskRow view={view} onOpenHistory={openHistory} />
            </div>
          ))}
        </div>
      )}
      <NetworkHistoryDialog
        open={historyView !== null}
        view={historyView}
        initialNodeId={historyNodeId}
        onOpenChange={(open) => {
          if (!open) setHistoryView(null)
        }}
      />
    </section>
  )
}

function NetworkTaskRow({
  view,
  onOpenHistory,
}: {
  view: NetworkTaskView
  onOpenHistory: (view: NetworkTaskView, nodeId: string) => void
}) {
  const { t } = useLocale()
  const TypeIcon = typeIcons[view.task.type]
  return (
    <div className="grid gap-4 py-4 first:pt-0 last:pb-0 lg:grid-cols-[minmax(12rem,18rem)_1fr]">
      <div className="min-w-0">
        <div className="flex items-center gap-2">
          <TypeIcon className="size-4 shrink-0 text-muted-foreground" />
          <h3 className="truncate font-medium">{view.task.name}</h3>
          <Badge variant="outline">{t(networkTypeKey(view.task.type))}</Badge>
        </div>
        <p className="mt-1 truncate text-xs text-muted-foreground" title={view.task.target}>
          {view.task.target}
        </p>
      </div>
      {view.nodes.length === 0 ? (
        <div className="flex min-h-16 items-center text-sm text-muted-foreground">
          {t("network.no_nodes")}
        </div>
      ) : (
        <div className="grid grid-cols-1 gap-2 sm:grid-cols-2 xl:grid-cols-3">
          {view.nodes.map((state) => (
            <NetworkNodeCell
              key={state.node.id}
              state={state}
              onClick={() => onOpenHistory(view, state.node.id)}
            />
          ))}
        </div>
      )}
    </div>
  )
}

function NetworkNodeCell({ state, onClick }: { state: NetworkNodeState; onClick: () => void }) {
  const { t } = useLocale()
  const latest = state.latest
  return (
    <Button
      variant="outline"
      className="h-auto min-w-0 flex-col items-stretch gap-2 px-3 py-2 text-left"
      onClick={onClick}
      aria-label={`${networkNodeLabel(state.node)} ${t("network.history")}`}
    >
      <span className="flex min-w-0 items-center justify-between gap-2">
        <span className="truncate">{networkNodeLabel(state.node)}</span>
        <span className="shrink-0 font-mono text-xs">
          {latest ? formatLatency(latest.latency_ms) : t("network.waiting")}
        </span>
      </span>
      <span className="h-1.5 overflow-hidden rounded-full bg-muted">
        <span
          className={cn("block h-full rounded-full", latest?.success ? "bg-primary" : "bg-destructive")}
          style={{ width: `${latencyRailPercent(state)}%` }}
        />
      </span>
    </Button>
  )
}

function NetworkQualitySkeleton() {
  return (
    <div className="flex flex-col gap-4">
      {Array.from({ length: 2 }).map((_, index) => (
        <div key={index} className="grid gap-4 lg:grid-cols-[minmax(12rem,18rem)_1fr]">
          <div className="flex flex-col gap-2">
            <Skeleton className="h-5 w-32" />
            <Skeleton className="h-4 w-48" />
          </div>
          <div className="grid grid-cols-1 gap-2 sm:grid-cols-2 xl:grid-cols-3">
            {Array.from({ length: 3 }).map((__, cell) => <Skeleton key={cell} className="h-16" />)}
          </div>
        </div>
      ))}
    </div>
  )
}
