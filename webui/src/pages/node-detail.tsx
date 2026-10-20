import { useEffect, useRef } from "react"
import { useParams } from "react-router"
import { PublicThemeBoundary } from "@/components/public-theme-boundary"
import { useSiteSettings } from "@/context/site-settings"
import { usePublicMetricWorkspace } from "@/hooks/use-public-metric-workspace"
import { NodeDetailView } from "@/pages/node-detail-view"

function NodeDetail() {
  const { id } = useParams<{ id: string }>()
  const workspace = usePublicMetricWorkspace(id)
  const node = workspace.node
  const { reportInteraction } = useSiteSettings()
  const ready = Boolean(node.data)
  const opened = useRef(false)
  const range = workspace.range
  const previousRange = useRef(range)

  useEffect(() => {
    if (!ready || opened.current) return
    opened.current = true
    reportInteraction("node_open", "node")
  }, [ready, reportInteraction])

  useEffect(() => {
    if (previousRange.current === range) return
    previousRange.current = range
    reportInteraction("range_change", "node")
  }, [range, reportInteraction])

  return <PublicThemeBoundary route={{ kind: "node", node_id: id }} nodes={node.data ? [node.data] : []} groups={workspace.source.data?.groups ?? []}
    history={workspace.history} historyMetricNames={workspace.historyMetricNames}
    state={{ nodes: node.error ? "error" : node.loading ? "loading" : "ready", metrics: workspace.metricsState }}>
    <NodeDetailView workspace={workspace} />
  </PublicThemeBoundary>
}

export default NodeDetail
