import { useMemo, useState } from "react"
import { Header } from "@/components/header"
import { PublicFooter } from "@/components/public-footer"
import { PublicThemeBoundary } from "@/components/public-theme-boundary"
import { NodeCard } from "@/components/node-card"
import { useSiteSettings } from "@/context/site-settings"
import { useLocale } from "@/context/locale"
import { usePublicFleet } from "@/hooks/use-public-fleet"
import { useFleetControls } from "@/hooks/use-fleet-controls"
import { useFleetOrder } from "@/hooks/use-fleet-order"
import { useNodeNetworkSummaries } from "@/hooks/use-network"
import { publicFleetNode } from "@/lib/fleet-selectors"
import { FleetToolbar } from "@/pages/fleet-toolbar"
import { FleetResults } from "@/pages/fleet-results"
import { FleetGroupTabs, FleetOverview } from "@/pages/fleet-overview"
import { FleetConnection } from "@/pages/fleet-summary"
import { useFleetText } from "@/pages/fleet-text"
export default function Dashboard() {
 const source = usePublicFleet(), { settings, loading, error } = useSiteSettings(), { locale } = useLocale(), t = useFleetText()
 const showIP = !loading && !error && settings.public_address_mode !== "hidden"
 const nodes = useMemo(() => source.nodes.map(node => publicFleetNode(node, Boolean(showIP))), [source.nodes, showIP])
 const controls = useFleetControls(nodes, source.groups), [paused, setPaused] = useState(false)
 const selected = useFleetOrder(nodes, source.groups, controls.value, locale, paused)
 const networkEnabled = !loading && !error && settings.show_network_quality && !source.offline
 const network = useNodeNetworkSummaries(networkEnabled)
 return <PublicThemeBoundary route={{ kind: "dashboard" }} nodes={selected} groups={source.groups}
   state={{ nodes: source.error ? "error" : source.loading ? "loading" : "ready" }}
   network={networkEnabled ? { data: network.data, state: network.error ? "error" : network.loading ? "loading" : "ready" } : undefined}>
   <div className="flex min-h-svh flex-col"><a href="#fleet-results" className="sr-only focus:not-sr-only">{t("skip")}</a><Header wide /><main className="mx-auto w-full max-w-[1400px] flex-1 px-4 py-4 sm:px-6">
     <div className="space-y-5">
       <div className="flex flex-col gap-2 lg:flex-row lg:items-center lg:justify-between"><FleetGroupTabs nodes={nodes} groups={source.groups} value={controls.value.groups} change={(groups) => controls.change({ groups })} /><FleetConnection state={source} retry={source.refresh} className="self-start lg:self-auto" /></div>
       <FleetOverview nodes={nodes} unavailable={source.offline || source.loading || Boolean(source.error && !nodes.length)} />
       <FleetToolbar value={controls.value} change={controls.change} nodes={nodes} groups={source.groups} count={selected.length} />
       <div>{source.loading ? <div className="grid gap-4 md:grid-cols-2 xl:grid-cols-4">{Array.from({ length: 8 }, (_, index) => <NodeCard key={index} loading />)}</div> : <FleetResults nodes={selected} allNodes={nodes} groups={source.groups} layout={controls.value.layout} onInteraction={setPaused} networkSummaries={network.summaries} />}</div>
     </div>
   </main><PublicFooter wide /></div>
 </PublicThemeBoundary>
}
