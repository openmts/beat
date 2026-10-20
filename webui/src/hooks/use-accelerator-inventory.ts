import { useEffect, useState } from "react"
import { getAcceleratorInventory } from "@/lib/accelerator-api"
import type { NodeAcceleratorsAdmin } from "@/types/accelerator"
export function useAcceleratorInventory(nodeID: string, identity = "") {
 const key = `${identity}/${nodeID}`
 const [state, setState] = useState<{ key: string; data: NodeAcceleratorsAdmin | null; loading: boolean; error: string }>({ key, data: null, loading: Boolean(nodeID), error: "" })
 const [revision, setRevision] = useState(0)
 useEffect(() => {
  const controller = new AbortController(); setState({ key, data: null, loading: Boolean(nodeID), error: "" })
  if (nodeID) void getAcceleratorInventory(nodeID, controller.signal).then(data => { if (!controller.signal.aborted) setState({ key, data, loading: false, error: "" }) }).catch((error: unknown) => { if (!controller.signal.aborted) setState({ key, data: null, loading: false, error: error instanceof Error ? error.message : "GPU inventory unavailable" }) })
  return () => controller.abort()
 }, [nodeID, key, revision])
 const visible = state.key === key ? state : { key, data: null, loading: Boolean(nodeID), error: "" }
 return { ...visible, reload: () => setRevision(value => value + 1) }
}
