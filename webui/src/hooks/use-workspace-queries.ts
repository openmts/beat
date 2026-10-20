import { useEffect, useReducer, useRef, useState } from "react"
import { WorkspacePoller, type WorkspacePollEvent, type WorkspacePollingOptions } from "@/lib/workspace-poller"
import { dashboardDomain } from "@/lib/workspace-query"
import type { PanelQueryState } from "@/types/metric-workspace"

interface WorkspaceQueryState { dashboardId?: string; domain: { from: string; to: string }; panels: Record<string, PanelQueryState> }

function reduceQueries(state: WorkspaceQueryState, event: WorkspacePollEvent): WorkspaceQueryState {
  if (event.type === "range") return { dashboardId: event.dashboardId, domain: event.domain,
    panels: state.dashboardId === event.dashboardId ? Object.fromEntries(Object.entries(state.panels).filter(([id]) => event.panelIds.includes(id))) : {} }
  if (event.type === "panel") return { ...state, panels: { ...state.panels, [event.id]: { ...state.panels[event.id], ...event.patch } } }
  return { ...state, panels: Object.fromEntries(Object.entries(state.panels).map(([id, panel]) => [id, { ...panel, loading: false }])) }
}

export function useWorkspaceQueries(options: WorkspacePollingOptions) {
  const [revision, setRevision] = useState(0)
  const [state, update] = useReducer(reduceQueries, options.range, (range): WorkspaceQueryState => ({ domain: dashboardDomain(range, options.definition), panels: {} }))
  const current = useRef(options)
  current.current = options
  const poller = useRef<WorkspacePoller | null>(null)
  const key = JSON.stringify(options)
  useEffect(() => {
    const instance = new WorkspacePoller(current.current, update)
    poller.current = instance
    const stop = instance.start()
    return () => { stop(); if (poller.current === instance) poller.current = null }
  }, [key, revision])
  return { ...state, refresh: () => setRevision((value) => value + 1), retryPanel: (panelId: string) => poller.current?.retry(panelId) }
}
