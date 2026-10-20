import { useMemo, useState } from "react"
import { Header } from "@/components/header"
import { PublicFooter } from "@/components/public-footer"
import { PublicThemeBoundary } from "@/components/public-theme-boundary"
import { NetworkQualityBand } from "@/components/network-quality-band"
import { NodeCard } from "@/components/node-card"
import { useSiteSettings } from "@/context/site-settings"
import { useLocale } from "@/context/locale"
import { usePublicFleet } from "@/hooks/use-public-fleet"
import { useFleetControls } from "@/hooks/use-fleet-controls"
import { useFleetOrder } from "@/hooks/use-fleet-order"
import { publicFleetNode } from "@/lib/fleet-selectors"
import { FleetToolbar } from "@/pages/fleet-toolbar"
import { FleetResults } from "@/pages/fleet-results"
import { FleetConnection, FleetSummary } from "@/pages/fleet-summary"
import { useFleetText } from "@/pages/fleet-text"
export default function Dashboard() {
 const source = usePublicFleet(), { settings, loading, error } = useSiteSettings(), { locale } = useLocale(), t = useFleetText()
 const showIP = !loading && !error && settings.public_address_mode !== "hidden"
 const nodes = useMemo(() => source.nodes.map(node => publicFleetNode(node, Boolean(showIP))), [source.nodes, showIP])
 const controls = useFleetControls(nodes, source.groups), [paused, setPaused] = useState(false)
 const selected = useFleetOrder(nodes, source.groups, controls.value, locale, paused)
 return <div className="flex min-h-svh flex-col"><a href="#fleet-results" className="sr-only focus:not-sr-only">{t("skip")}</a><Header /><main className="container-page w-full flex-1 py-4">
  <div className="flex flex-col gap-1 py-1 xl:flex-row xl:items-center xl:gap-4"><FleetConnection state={source} retry={source.refresh} /><div className="min-w-0 flex-1"><FleetSummary nodes={nodes} unavailable={source.offline || source.loading || Boolean(source.error && !nodes.length)} /></div></div>
  <FleetToolbar value={controls.value} change={controls.change} nodes={nodes} groups={source.groups} count={selected.length} />
  <div className="py-5">{source.loading ? <div className="grid gap-4 md:grid-cols-2 xl:grid-cols-4">{Array.from({ length: 8 }, (_, index) => <NodeCard key={index} loading />)}</div> : <PublicThemeBoundary route={{ kind: "dashboard" }} nodes={selected} groups={source.groups} state={{ nodes: source.error ? "error" : "ready" }}><FleetResults nodes={selected} allNodes={nodes} groups={source.groups} layout={controls.value.layout} onInteraction={setPaused} /></PublicThemeBoundary>}</div>
  {!loading && !error && settings.show_network_quality && !source.offline && <NetworkQualityBand />}
 </main><PublicFooter /></div>
}
