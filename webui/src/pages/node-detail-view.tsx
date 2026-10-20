import { ArrowLeftIcon } from "lucide-react"
import { Link } from "react-router"
import { Alert, AlertDescription } from "@/components/ui/alert"
import { Badge } from "@/components/ui/badge"
import { buttonVariants } from "@/components/ui/button"
import { Skeleton } from "@/components/ui/skeleton"
import { Header } from "@/components/header"
import { PublicFooter } from "@/components/public-footer"
import { usePublicTheme } from "@/lib/theme-presentation"
import { useLocale } from "@/context/locale"
import { useSiteSettings } from "@/context/site-settings"
import type { Node } from "@/types"
import type { PublicMetricWorkspace } from "@/hooks/use-public-metric-workspace"
import { NodeOverview } from "@/pages/node-detail-overview"
import { AcceleratorDetail } from "@/components/accelerator-detail"
import { NodeCharts } from "@/pages/node-detail-charts"

export function NodeDetailView({ workspace }: { workspace: PublicMetricWorkspace }) {
  const { data: node, loading: nodeLoading, error: nodeError } = workspace.node
  const { settings } = useSiteSettings()
  const layout = usePublicTheme()?.definition.detail ?? "balanced"
  return <div className="flex min-h-svh flex-col"><Header /><NodeDetailHeader node={node} nodeLoading={nodeLoading} />
    <main className="container-page safe-pb flex-1 py-4 sm:py-6">
      <ErrorAlerts nodeError={nodeError} />
      <div data-theme-detail-layout={layout} className={layout === "compact" ? "grid items-start gap-6 lg:grid-cols-[minmax(16rem,22rem)_1fr]" : "space-y-6"}>
        <section>{nodeLoading ? <div className="space-y-4"><Skeleton className="h-40 w-full" /><Skeleton className="h-56 w-full" /></div> : node ? <NodeOverview node={node} /> : null}</section>
        <section className="min-w-0 space-y-6"><NodeCharts workspace={workspace} />{node && settings.show_accelerators && node.accelerator_visible === true && <AcceleratorDetail key={node.id} node={node} catalog={workspace.source.data?.catalog.metrics} range={workspace.range} refreshSeconds={workspace.refreshSeconds} onRange={workspace.setRange} onInterval={workspace.setRefreshSeconds} />}</section>
      </div>
    </main><PublicFooter />
  </div>
}
function NodeDetailHeader({ node, nodeLoading }: { node: Node | null; nodeLoading: boolean }) {
  const { t } = useLocale()
  return (<header className="sticky top-12 z-20 flex h-12 shrink-0 items-center gap-2 border-b bg-background/85 px-4 py-2 backdrop-blur-md supports-[backdrop-filter]:bg-background/70">
        <Link
          to="/"
          aria-label={t("app.back")}
          title={t("app.back")}
          className={buttonVariants({ variant: "ghost", size: "icon" })}
        >
          <ArrowLeftIcon />
        </Link>
        <h1 className="min-w-0 flex-1 truncate text-base font-semibold sm:text-lg">
          {nodeLoading ? t("node.loading") : node ? node.alias || node.name : t("app.nodes")}
        </h1>
        {node ? (
          <Badge variant={node.status === "online" ? "default" : "secondary"}>
            <span className="size-1.5 rounded-full bg-current" />
            {node.status === "online" ? t("node.online") : t("node.offline")}
          </Badge>
        ) : null}
      </header>)
}
function ErrorAlerts({ nodeError }: { nodeError: string | null }) {
  const { t } = useLocale()
  // 公共节点页只带公共外壳目录，工作区错误码在这里会原样显示成键名。
  if (!nodeError) return null
  const message = nodeError === "workspace.node_unavailable" ? t("node.unavailable") : t("node.summary_stale")
  return <Alert className="mb-4" variant="destructive"><AlertDescription>{message}</AlertDescription></Alert>
}
