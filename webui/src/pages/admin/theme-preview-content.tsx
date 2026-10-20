import { ArrowLeftIcon } from "lucide-react"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Alert, AlertDescription } from "@/components/ui/alert"
import { MetricsChart } from "@/components/metrics-chart"
import { ThemedNodeCollection } from "@/components/themed-node-collection"
import { NodeOverview } from "@/pages/node-detail-overview"
import { useLocale } from "@/context/locale"
import { usePublicTheme } from "@/lib/theme-presentation"
import type { Node } from "@/types"
import type { ThemeNode } from "@/types/theme-runtime"

export function ThemePreviewContent({ onBack }: { onBack: () => void }) {
  const { t } = useLocale()
  const { snapshot, definition } = usePublicTheme()!
  const selected = snapshot.route.node_id ? snapshot.nodes.find((node) => node.id === snapshot.route.node_id) : undefined
  return <div className="min-h-svh"><header className="flex flex-wrap items-center justify-between gap-3 border-b px-5 py-4">
    <div><h1 className="font-heading text-lg font-semibold">{snapshot.site.site_title}</h1><p className="text-xs text-muted-foreground">{snapshot.site.site_description}</p></div>
    <Badge variant="outline">{t("theme.preview")}</Badge>
  </header><main className="space-y-5 p-4 sm:p-6">
    {selected ? <><Button variant="ghost" size="sm" onClick={onBack}><ArrowLeftIcon />{t("theme.back_to_overview")}</Button>
      <h2 className="text-xl font-semibold">{selected.alias || selected.name}</h2>
      <div data-theme-detail-layout={definition.detail} className={definition.detail === "compact" ? "grid items-start gap-5 lg:grid-cols-[minmax(16rem,22rem)_1fr]" : "space-y-5"}>
        <NodeOverview node={previewNode(selected)} /><PreviewCharts />
      </div></> : <ThemedNodeCollection selectedGroup="" />}
  </main></div>
}

function PreviewCharts() {
  const { t } = useLocale()
  const { snapshot, definition } = usePublicTheme()!
  const history = snapshot.history
  if (!history) return null
  return <div className="min-w-0 space-y-3">
    {snapshot.state.metrics === "error" && <Alert variant="destructive"><AlertDescription>{t("app.error")}</AlertDescription></Alert>}
    {snapshot.state.metrics === "loading" ? <p role="status">{t("app.loading")}</p> :
      <div className={`grid grid-cols-1 gap-4 md:grid-cols-2 ${definition.detail === "spotlight" ? "md:[&>*:first-child]:col-span-2" : ""}`}>
        {["cpu", "memory", "disk"].map((key, index) => <MetricsChart key={key} title={t(`node.${key}`)} data={history.series[key]}
          color={`var(--chart-${index + 1})`} valueFormat="percent" from={history.from} to={history.to} rangeHours={1} />)}
      </div>}
  </div>
}

function previewNode(node: ThemeNode): Node {
  return { id: node.id, name: node.name, alias: node.alias, group_id: node.group_id, host: node.host ?? "", port: 0,
    status: node.status, tags: node.tags, public_remark: node.public_remark, is_public: true, metrics: node.metrics,
    last_seen: node.last_seen, telemetry_at: node.telemetry_at, telemetry_age_seconds: node.telemetry_age_seconds, telemetry_state: node.telemetry_state,
    created_at: "", updated_at: "", sort_order: 0, ssh_public_key: "",
    cpu_model: node.system.cpu_model ?? "", os: node.system.os ?? "", platform: node.system.platform ?? "", os_version: node.system.os_version ?? "",
    kernel: node.system.kernel ?? "", arch: node.system.arch ?? "", virtualization: node.system.virtualization ?? "", agent_version: node.system.agent_version ?? "" }
}
