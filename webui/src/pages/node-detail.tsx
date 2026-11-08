import { useEffect, useRef } from "react"
import { useParams } from "react-router"
import { PublicThemeBoundary } from "@/components/public-theme-boundary"
import { useSiteSettings } from "@/context/site-settings"
import { usePublicMetricWorkspace } from "@/hooks/use-public-metric-workspace"
import { usePublicNodeNetwork } from "@/hooks/use-network"
import { NodeDetailView } from "@/pages/node-detail-view"

function NodeDetail() {
  const { id } = useParams<{ id: string }>()
  const workspace = usePublicMetricWorkspace(id)
  const node = workspace.node
  const sourceNodes = workspace.source.data?.nodes
  const summary = node.data
  // 详情页的节点摘要独立轮询；合并进公开列表，避免主题快照一直停留在首屏指标。
  const nodes = sourceNodes
    ? summary ? sourceNodes.map((item) => item.id === summary.id ? summary : item) : sourceNodes
    : summary ? [summary] : []
  const { settings, reportInteraction } = useSiteSettings()
  const ready = Boolean(node.data)
  const opened = useRef(false)
  const range = workspace.range
  const network = usePublicNodeNetwork(id, range, settings.show_network_quality === true)
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

  return <PublicThemeBoundary route={{ kind: "node", node_id: id }} nodes={nodes} groups={workspace.source.data?.groups ?? []}
    history={workspace.history} historyMetricNames={workspace.historyMetricNames}
    networkHistory={network.history.data ?? undefined}
    network={{ data: network.quality.data, state: network.quality.error ? "error" : network.quality.loading ? "loading" : "ready" }}
    range={range} onRangeChange={workspace.setRange}
    state={{ nodes: node.error ? "error" : node.loading ? "loading" : "ready", metrics: workspace.metricsState }}>
    <NodeDetailView workspace={workspace} />
  </PublicThemeBoundary>
}

export default NodeDetail
