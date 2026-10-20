import { Alert, AlertDescription } from "@/components/ui/alert"
import { Button } from "@/components/ui/button"
import { useLocale } from "@/context/locale"
import { formatDateTime } from "@/lib/intl-format"
import { useFleetText } from "@/pages/fleet-text"
import { fleetTotals } from "@/lib/fleet-selectors"
import { formatMetricValue } from "@/lib/metric-format"
import type { Node } from "@/types"
import type { PublicFleetSnapshot } from "@/lib/public-fleet-live"
export function FleetSummary({ nodes, unavailable = false }: { nodes: Node[]; unavailable?: boolean }) {
 const { locale } = useLocale(), t = useFleetText(), totals = fleetTotals(nodes)
 const rate = (value: ReturnType<typeof fleetTotals>["upload"]) => value.value === null || unavailable ? "—" : `${formatMetricValue(value.value, "bytes-per-second", locale)}${value.covered < nodes.length ? ` · ${t("partial")} (${value.covered}/${nodes.length})` : ""}`
 return <dl className="grid grid-cols-2 gap-x-3 gap-y-1 py-2 sm:grid-cols-3 xl:grid-cols-6 xl:gap-x-0">{[["total", totals.total], ["online", totals.online], ["groups", totals.groups], ["countries", totals.countries], ["upload", rate(totals.upload)], ["download", rate(totals.download)]].map(([label, value]) => <div key={label} className="flex min-w-0 items-baseline gap-1.5 border-l pl-2 first:border-l-0 first:pl-0 xl:px-3"><dt className="shrink-0 text-[11px] font-medium text-muted-foreground">{t(String(label))}</dt><dd className="min-w-0 truncate text-sm font-semibold tabular-nums">{unavailable ? "—" : value}</dd></div>)}</dl>
}
export function FleetConnection({ state, retry }: { state: PublicFleetSnapshot; retry: () => void }) {
 const { locale } = useLocale(), t = useFleetText()
 if (state.offline || state.error || state.stale) return <Alert variant={state.error === "capacity" ? "destructive" : "default"}><AlertDescription><div className="flex flex-wrap items-center justify-between gap-2"><span>{t(state.offline ? "offlineMode" : state.error === "capacity" || state.error === "denied" ? state.error : state.nodes.length ? "staleNotice" : "unavailable")}{state.receivedAt !== null && ` · ${t("updated")}: ${formatDateTime(locale, state.serverTime ?? state.receivedAt)}`}</span><Button variant="outline" size="sm" disabled={state.offline} onClick={retry}>{t("retry")}</Button></div></AlertDescription></Alert>
 return <p className="text-xs text-muted-foreground xl:shrink-0">{t(state.loading ? "loading" : state.connected ? "streaming" : "polling")}{state.receivedAt !== null && ` · ${t("updated")}: ${formatDateTime(locale, state.serverTime ?? state.receivedAt)}`}</p>
}
