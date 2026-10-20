import { useCallback, useState } from "react"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { useLocale } from "@/context/locale"
import { useOperationResource } from "@/hooks/use-operation-resource"
import { useConfirmedOperation } from "@/hooks/use-confirmed-operation"
import { cancelOperationTask, getOperationOutput, getOperationTask, getOperationTaskContent, operationDecode, retryOperationTarget } from "@/lib/operations-api"
import type { OperationOutput, OperationTarget } from "@/types/operations"
import { OperationConfirm } from "./operation-confirm"
import { OperationStatus, retryableOperationStates } from "./operation-status"

export function OperationTaskDetail({ id, close }: { id: string; close: () => void }) {
 const { t } = useLocale()
 const resource = useOperationResource(useCallback(signal => getOperationTask(id, signal), [id]), 5000)
 const flow = useConfirmedOperation()
 const [output, setOutput] = useState<{ target: string; value: OperationOutput } | null>(null)
 const [content, setContent] = useState<string | null>(null)
 const [reason, setReason] = useState("")
 const retry = (target: OperationTarget) => flow.request({ title: t("operations.retry_this_target"), uncertain: target.state === "unknown" || target.restore_risk,
  description: t("operations.a_new_attempt_preserves_the_previous_record_chec"),
  execute: async () => { await retryOperationTarget(id, target.id, { reason: reason.trim(), acknowledge_unknown_risk: target.state === "unknown" || Boolean(target.restore_risk) }); setReason(""); setOutput(null); resource.reload() } })
 const inspect = (target: OperationTarget) => flow.request({ title: t("operations.inspect_output"), description: t("operations.output_may_contain_sensitive_content_and_is_kept"),
  execute: async () => { const value = await getOperationOutput(id, target.id); setOutput({ target: target.node_name, value }) } })
 return <Card><CardHeader className="flex-row items-center justify-between"><CardTitle>{t("operations.task_details")}</CardTitle><Button variant="ghost" onClick={close}>{t("operations.close")}</Button></CardHeader><CardContent className="space-y-4">
  <p className="break-all text-xs text-muted-foreground">{id}</p>
  <Button variant="outline" onClick={() => flow.request({ title: t("operations.view_task_content"), description: t("operations.task_content_may_contain_sensitive_arguments_and"), execute: async () => { const result = await getOperationTaskContent(id); setContent(result.payload.file ? `${result.payload.file.root_id}/${result.payload.file.relative_path}\n${result.payload.file.size} bytes\nSHA-256: ${result.payload.file.digest}` : operationDecode(result.payload.command)) } })}>{t("operations.view_task_content")}</Button>
  {content !== null && <section><Button size="sm" variant="ghost" onClick={() => setContent(null)}>{t("operations.clear_content")}</Button><OutputBlock title={t("operations.task_content")} value={content} /></section>}
  {resource.error && <p role="alert" className="text-destructive">{resource.error}</p>}
  {resource.loading && <p>{t("operations.loading_2")}</p>}
  {resource.data && <><div className="flex flex-wrap items-center gap-3"><OperationStatus state={resource.data.state ?? "queued"} />
   <Button variant="outline" disabled={resource.data.cancel_requested || ["succeeded", "failed", "canceled"].includes(resource.data.state ?? "")} onClick={() => flow.request({ title: t("operations.request_cancellation"), description: t("operations.effects_that_already_occurred_cannot_be_undone_f"), execute: async () => { await cancelOperationTask(id); resource.reload() } })}>{t("operations.cancel_task")}</Button></div>
   <label className="block space-y-2 text-sm">{t("operations.reason_for_retry")}<Input value={reason} maxLength={500} onChange={e => setReason(e.target.value)} /></label>
   <div className="divide-y rounded-md border">{resource.data.targets?.map(target => <div key={target.id} className="flex flex-wrap items-center gap-3 p-3"><span className="min-w-40 flex-1 truncate">{target.node_name}</span><OperationStatus state={target.state} /><Button size="sm" variant="outline" onClick={() => inspect(target)}>{t("operations.output")}</Button>
    {retryableOperationStates.has(target.state) && target.node_id && <Button size="sm" variant="outline" disabled={!reason.trim()} onClick={() => retry(target)}>{t("operations.retry")}</Button>}</div>)}</div></>}
  {output && <section className="space-y-2"><div className="flex items-center justify-between"><h3 className="font-medium">{output.target} · {t("operations.exit_code")} {output.value.exit_code}</h3><Button size="sm" variant="ghost" onClick={() => setOutput(null)}>{t("operations.clear_output")}</Button></div>
   <p className="break-all text-xs text-muted-foreground">{t("operations.attempt")}: {output.value.attempt_id}</p><OperationStatus state={output.value.state} />{output.value.truncated && <p role="status">{t("operations.output_was_truncated")}</p>}
   <OutputBlock title="stdout" value={operationDecode(output.value.stdout)} /><OutputBlock title="stderr" value={operationDecode(output.value.stderr)} />
  </section>}
  <OperationConfirm flow={flow} />
 </CardContent></Card>
}
function OutputBlock({ title, value }: { title: string; value: string }) { return <div><h4 className="text-xs text-muted-foreground">{title}</h4><pre className="max-h-80 overflow-auto whitespace-pre-wrap break-all rounded-md bg-muted p-3 text-xs">{value || "—"}</pre></div> }
