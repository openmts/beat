import { Badge } from "@/components/ui/badge"
import { useLocale } from "@/context/locale"
const stateKeys: Record<string, string> = {
 uploading: "operations.state.uploading", scanning: "operations.state.scanning", ready: "operations.state.ready", collecting: "operations.state.collecting",
 queued: "operations.state.queued", leased: "operations.state.leased", running: "operations.state.running",
 cancel_requested: "operations.state.cancel_requested", canceled: "operations.state.canceled", succeeded: "operations.state.succeeded",
 failed: "operations.state.failed", unknown: "operations.state.unknown", timed_out: "operations.state.timed_out",
 interrupted: "operations.state.interrupted", expired: "operations.state.expired", output_limit: "operations.state.output_limit",
}
export const retryableOperationStates = new Set(["failed", "timed_out", "canceled", "output_limit", "interrupted", "expired", "unknown"])
export function OperationStatus({ state }: { state: string }) {
 const { t } = useLocale()
 const name = t(stateKeys[state] ?? state)
 return <Badge variant={retryableOperationStates.has(state) ? "destructive" : state === "succeeded" ? "default" : "secondary"}>{name}</Badge>
}
