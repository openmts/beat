import { useState } from "react"
import { usePublicWorkspaceSources } from "@/hooks/use-public-workspace-sources"
import { usePublicNodeSummary } from "@/hooks/use-public-node-summary"
import { useWorkspaceQueries } from "@/hooks/use-workspace-queries"
import { buildPublicWorkspaceHistory } from "@/lib/public-workspace-history"
import { emptyPublicDefinition, initialPublicControls, type PublicWorkspaceControls } from "@/lib/public-workspace-controls"
import { applyRowCollapse } from "@/lib/workspace-row"
import type { WorkspaceRange } from "@/types/metric-workspace"

export function usePublicMetricWorkspace(nodeId?: string) {
  const source = usePublicWorkspaceSources(nodeId)
  const defaults = initialPublicControls(nodeId, source.data)
  const [draft, setDraft] = useState<PublicWorkspaceControls | null>(null)
  const controls = draft?.key === defaults.key ? draft : defaults
  const node = usePublicNodeSummary(nodeId, controls.range.mode === "relative" ? controls.refreshSeconds : 0)
  // 公共节点页是只读的：行的折叠只保存在当前标签页，不写回服务端。
  const [rowOverrides, setRowOverrides] = useState<Record<string, boolean>>({})
  const servedDefinition = source.data?.dashboard.definition ?? emptyPublicDefinition
  const definition = { ...servedDefinition, panels: applyRowCollapse(servedDefinition.panels, rowOverrides) }
  const queryDefinition = node.data ? { ...definition, refresh_seconds: controls.refreshSeconds } : emptyPublicDefinition
  const queries = useWorkspaceQueries({ dashboardId: defaults.key, definition: queryDefinition, nodes: source.data?.nodes ?? [], groups: source.data?.groups ?? [],
    selections: controls.values, range: controls.range, public: true })
  const history = source.data && node.data ? buildPublicWorkspaceHistory({ nodeId: node.data.id, definition,
    catalog: source.data.catalog, panels: queries.panels }) : { history: undefined, metricNames: [], state: "loading" as const }
  const patch = (value: Partial<PublicWorkspaceControls>) => setDraft((current) => ({ ...(current?.key === defaults.key ? current : defaults), ...value }))
  return { node, source, definition, queries, values: controls.values, range: controls.range, refreshSeconds: controls.refreshSeconds,
    history: history.history, historyMetricNames: history.metricNames,
    metricsState: source.error ? "error" as const : history.state,
    setRange: (range: WorkspaceRange) => patch({ range }), setRefreshSeconds: (refreshSeconds: number) => patch({ refreshSeconds }),
    setVariable: (name: string, values: string[]) => patch({ values: { ...controls.values, [name]: values } }),
    toggleRow: (rowId: string, collapsed: boolean) => setRowOverrides((current) => ({ ...current, [rowId]: collapsed })),
    refresh: () => { node.refresh(); queries.refresh() } }
}

export type PublicMetricWorkspace = ReturnType<typeof usePublicMetricWorkspace>
