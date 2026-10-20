import { useEffect, useState } from "react"
import { PublicNodePoller, type PublicNodeEvent } from "@/lib/public-node-poller"
import type { Node } from "@/types"

interface SummaryState { nodeId?: string; data: Node | null; loading: boolean; error: string | null }

export function usePublicNodeSummary(nodeId: string | undefined, refreshSeconds: number) {
  const [state, setState] = useState<SummaryState>({ data: null, loading: Boolean(nodeId), error: null })
  const [revision, setRevision] = useState(0)
  useEffect(() => {
    if (!nodeId) return
    const update = (event: PublicNodeEvent) => setState((previous) => updateSummary(previous.nodeId === nodeId ? previous : { nodeId, data: null, loading: true, error: null }, event))
    return new PublicNodePoller({ nodeId, refreshSeconds, update }).start()
  }, [nodeId, refreshSeconds, revision])
  const visible = state.nodeId === nodeId ? state : { nodeId, data: null, loading: Boolean(nodeId), error: null }
  return { ...visible, refresh: () => setRevision((value) => value + 1) }
}

function updateSummary(state: SummaryState, event: PublicNodeEvent): SummaryState {
  if (event.type === "loading") return { ...state, loading: state.data === null, error: null }
  if (event.type === "ready") return { ...state, data: event.node, loading: false, error: null }
  return { ...state, data: event.unavailable ? null : state.data, loading: false,
    error: event.unavailable ? "workspace.node_unavailable" : "workspace.node_refresh_failed" }
}
