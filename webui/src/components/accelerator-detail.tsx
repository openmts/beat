import { useEffect, useState } from "react"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { Skeleton } from "@/components/ui/skeleton"
import { AcceleratorCurrent } from "@/components/accelerator-current"
import { WorkspaceSelect } from "@/components/metric-workspace/select"
import { WorkspacePanel } from "@/components/metric-workspace/panel"
import { WorkspaceTimeControls } from "@/components/metric-workspace/time-controls"
import { WorkspaceInspector } from "@/components/metric-workspace/inspector"
import { useWorkspaceQueries } from "@/hooks/use-workspace-queries"
import { useAcceleratorText } from "@/pages/accelerator-text"
import { acceleratorDeviceName, publicAccelerators } from "@/lib/accelerator-public"
import { acceleratorDefinition } from "@/lib/accelerator-history"
import { isRowPanel } from "@/lib/workspace-row"
import type { Node } from "@/types"
import type { AcceleratorDevicePublic } from "@/types/accelerator"
import type { MetricDefinition, WorkspaceRange } from "@/types/metric-workspace"
interface Props { node: Node; catalog?: MetricDefinition[]; range: WorkspaceRange; refreshSeconds: number; onRange: (value: WorkspaceRange) => void; onInterval: (value: number) => void }
export function AcceleratorDetail(props: Props) {
 const t = useAcceleratorText(), devices = publicAccelerators(props.node.accelerators) ?? []
 const [selected, setSelected] = useState(() => devices[0]?.device_id ?? ""), first = devices[0]?.device_id ?? ""
 useEffect(() => { if (!selected && first) setSelected(first) }, [selected, first])
 const device = devices.find(value => value.device_id === selected)
 const options = devices.map(value => ({ value: value.device_id, label: acceleratorDeviceName(value) }))
 if (selected && !device) options.unshift({ value: selected, label: t("inactive") })
 return <section className="min-w-0 space-y-4 rounded-lg border p-4"><div className="flex flex-wrap items-center justify-between gap-3"><h2 className="font-semibold">{t("title")}</h2>{options.length > 0 && <WorkspaceSelect label={t("device")} value={selected} options={options} onChange={setSelected} />}</div>
  {!devices.length && <p className="text-sm text-muted-foreground">{t("publicUnavailable")}</p>}
  {selected && !device && <p role="status" className="text-sm text-muted-foreground">{t("inactive")}</p>}
  {device && <Tabs defaultValue="current"><TabsList><TabsTrigger value="current">{t("current")}</TabsTrigger><TabsTrigger value="history">{t("history")}</TabsTrigger></TabsList><TabsContent value="current" className="mt-4"><AcceleratorCurrent device={device} catalog={props.catalog} /></TabsContent><TabsContent value="history" className="mt-4">{props.catalog ? <AcceleratorHistory key={device.device_id} {...props} catalog={props.catalog} device={device} /> : <Skeleton className="h-64" />}</TabsContent></Tabs>}
 </section>
}
function AcceleratorHistory(props: Props & { device: AcceleratorDevicePublic; catalog: MetricDefinition[] }) {
 const t = useAcceleratorText(), [inspecting, setInspecting] = useState<string | null>(null)
 const definition = acceleratorDefinition({ nodeID: props.node.id, device: props.device, catalog: props.catalog, t })
 definition.refresh_seconds = props.refreshSeconds
 const queries = useWorkspaceQueries({ dashboardId: `gpu/${props.node.id}/${props.device.device_id}`, definition, nodes: [props.node], groups: [], selections: {}, range: props.range, public: true })
 const inspected = definition.panels.find(panel => panel.id === inspecting)
 return <div className="space-y-4"><WorkspaceTimeControls range={props.range} refreshSeconds={props.refreshSeconds} onRange={props.onRange} onInterval={props.onInterval} onRefresh={queries.refresh} />
  <p aria-live="polite" className="font-mono text-xs text-muted-foreground">{queries.domain.from} → {queries.domain.to} UTC</p>
  {!definition.panels.length && <p className="text-sm text-muted-foreground">{t("publicUnavailable")}</p>}
  <div className="grid gap-4 lg:grid-cols-12">{definition.panels.filter(panel => !isRowPanel(panel)).map(panel => <WorkspacePanel key={panel.id} readOnly panel={panel} state={queries.panels[panel.id]} inspect={() => setInspecting(panel.id)} onRange={props.onRange} />)}</div>
  {inspected && <WorkspaceInspector publicLinks panel={inspected} state={queries.panels[inspected.id] ?? { loading: false }} close={() => setInspecting(null)} />}
 </div>
}
