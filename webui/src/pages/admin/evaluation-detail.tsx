import { useCallback, useState } from "react"
import { useEvaluationResource } from "@/hooks/use-evaluation-resource"
import { evaluationTime } from "@/lib/alert-evaluation-format"
import { evaluationDetail, evaluationDeviceDetail } from "@/lib/alert-evaluation-api"
import { Alert, AlertDescription } from "@/components/ui/alert"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Dialog, DialogContent, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { useEvaluationText } from "@/pages/admin/evaluation-text"
import { NotificationDeliveryDetail } from "@/pages/admin/notification-delivery-detail"
import type { EvaluationDetail as Detail } from "@/types/alert-evaluation"

export function EvaluationDetailDialog({ ruleID, nodeID, deviceID, onClose, onIncident }: { ruleID: string; nodeID: string; deviceID?: string; onClose: () => void; onIncident?: () => void }) {
  const t = useEvaluationText(), fetcher = useCallback((signal: AbortSignal) => deviceID ? evaluationDeviceDetail({ ruleID, nodeID, deviceID }, signal) : evaluationDetail(ruleID, nodeID, signal), [ruleID, nodeID, deviceID])
  const resource = useEvaluationResource(fetcher)
  return <Dialog open onOpenChange={open => { if (!open) onClose() }}><DialogContent className="max-h-[90svh] overflow-y-auto sm:max-w-2xl"><DialogHeader><DialogTitle>{t("detail")} · {resource.data?.rule?.name ?? ruleID}</DialogTitle></DialogHeader>
    <Button variant="outline" disabled={resource.loading} onClick={() => void resource.refresh()}>{t("refresh")}</Button>
    {resource.error && <Alert variant="destructive"><AlertDescription>{resource.error}</AlertDescription></Alert>}
    {resource.loading && <p>{t("loading")}</p>}{resource.data && <EvaluationDetailContent detail={resource.data} onIncident={onIncident} />}
  </DialogContent></Dialog>
}
function EvaluationDetailContent({ detail, onIncident }: { detail: Detail; onIncident?: () => void }) {
  const t = useEvaluationText(), state = detail.state
  const [deliveryID, setDeliveryID] = useState<string | null>(null)
  const facts = [[t("node"), state.node_id], [t("progress"), `${t("triggerProgress")}: ${state.trigger_elapsed_ms / 1000}s / ${detail.rule?.duration ?? "—"}s · ${t("recoveryProgress")}: ${state.recovery_elapsed_ms / 1000}s / ${detail.rule?.recovery_duration ?? "—"}s`], [t("lastGood"), evaluationTime(state.last_good_at)], [t("due"), evaluationTime(state.next_due_at)], [t("evidence"), state.evidence_id || "—"], [t("epoch"), state.health.epoch_id || "—"]]
  return <div className="space-y-4">
    <div className="flex flex-wrap gap-2">{state.device_id && <Badge variant="outline">{state.device_name || "GPU"}</Badge>}<Badge variant="outline">{t(state.phase)}</Badge><Badge variant={state.evaluation_status === "error" ? "destructive" : "secondary"}>{t(state.evaluation_status)}</Badge></div>
    {state.reason_code && <p className="text-sm text-muted-foreground">{state.reason_code}</p>}
    {state.active_event_status === "acknowledged" && <Alert><AlertDescription>{t("ack")}</AlertDescription></Alert>}
    {state.health.epoch_id && <p className="text-sm">{t("health")}: {t(state.health.status)} · {state.health.elapsed_ms / 1000}s · {state.health.suspended ? t("healthSuspended") : t(state.health.status === "error" ? detail.rule?.error_policy ?? "unknown" : detail.rule?.no_data_policy ?? "unknown")}</p>}
    {state.active_event_id && onIncident && <Button variant="outline" onClick={onIncident}>{t("incidentHistory")}</Button>}
    <dl className="space-y-3 text-sm">{facts.map(([key, value]) => <div key={key}><dt className="text-muted-foreground">{key}</dt><dd className="break-all">{value}</dd></div>)}</dl>
    {detail.window && <p className="text-sm">{t("evidenceWindow")}: {evaluationTime(detail.window.from)} → {evaluationTime(detail.window.to)} · {detail.window.aggregation}</p>}
    <section className="space-y-2 border-t pt-4"><h3 className="font-medium">{t("notifications")}</h3>{detail.notifications.length === 0 && <p className="text-sm text-muted-foreground">{t("empty")}</p>}{detail.notifications.map(item => <div key={item.source_key} className="space-y-1 rounded-md border p-3 text-sm"><p>{t(item.status === "history_unavailable" || item.status === "not_routed" ? item.status : `delivery_${item.status}`)}</p><p className="break-all font-mono text-xs text-muted-foreground">{item.source_key}</p>{item.receipt?.deliveries.map(delivery => <p key={delivery.id} className="break-all text-xs">{delivery.channel_id} · {t(`delivery_${delivery.state}`)} · {delivery.attempt_count}<Button variant="link" size="sm" onClick={() => setDeliveryID(delivery.id)}>{t("detail")}</Button></p>)}</div>)}</section>
    {deliveryID && <NotificationDeliveryDetail id={deliveryID} close={() => setDeliveryID(null)} owner={false} busy={false} retry={() => setDeliveryID(null)} cancel={() => setDeliveryID(null)} />}
    <section className="space-y-2"><h3 className="font-medium">{t("history")}</h3>{detail.recent_health_events.map(event => <p key={event.id} className="text-sm">{t(event.kind)} · {evaluationTime(event.occurred_at)} · {event.reason_code}</p>)}</section>
  </div>
}
