import { useCallback, useEffect, useRef, useState } from "react"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { useLocale } from "@/context/locale"
import { formatDateTime } from "@/lib/intl-format"
import { useOperationResource } from "@/hooks/use-operation-resource"
import { useConfirmedOperation } from "@/hooks/use-confirmed-operation"
import { cancelOperationTransfer, discardOperationTransferDraft, finalizeOperationTransfer, getOperationTransfer, operationDownloadURL, uploadOperationFile } from "@/lib/operation-transfers"
import { OperationConfirm } from "./operation-confirm"
import { OperationStatus } from "./operation-status"
export function OperationTransferDetail({ id, initialFile, close, task }: { id: string; initialFile?: File; close: () => void; task: (id: string) => void }) {
 const { locale, t } = useLocale()
 const resource = useOperationResource(useCallback(signal => getOperationTransfer(id, signal), [id]), 5000), flow = useConfirmedOperation()
 const [file, setFile] = useState(initialFile), [busy, setBusy] = useState(false), [bytes, setBytes] = useState(0), [error, setError] = useState("")
 const controller = useRef<AbortController | null>(null), mounted = useRef(true)
 useEffect(() => { mounted.current = true; return () => { mounted.current = false; controller.current?.abort() } }, [])
 const upload = async () => {
  if (!file || controller.current) return
  const active = new AbortController(); controller.current = active; setBusy(true); setError("")
  try {
   const current = await getOperationTransfer(id, active.signal)
   await uploadOperationFile(current, file, { signal: active.signal, progress: value => { if (mounted.current) setBytes(value) } })
   active.signal.throwIfAborted()
   await finalizeOperationTransfer(id)
   if (mounted.current) { setFile(undefined); resource.reload() }
  } catch (error) { if (mounted.current) setError(active.signal.aborted ? (t("operations.upload_paused_resume_with_the_same_file")) : error instanceof Error ? error.message : "Transfer failed") }
  finally { controller.current = null; if (mounted.current) setBusy(false) }
 }
 const transfer = resource.data
 return <Card><CardHeader className="flex-row items-center justify-between"><CardTitle>{t("operations.transfer_details")}</CardTitle><Button variant="ghost" onClick={close}>{t("operations.close")}</Button></CardHeader><CardContent className="space-y-4">
  <p className="break-all text-xs text-muted-foreground">{id}</p>{(resource.error || error) && <p role="alert" className="text-destructive">{resource.error || error}</p>}
  {transfer && <><div className="flex flex-wrap items-center gap-3"><OperationStatus state={transfer.state} /><span className="text-sm">{(transfer.size / 1048576).toFixed(2)} MiB</span>{transfer.task_id && <Button variant="outline" onClick={() => task(transfer.task_id!)}>{t("operations.view_node_task")}</Button>}</div>
   <p className="text-xs text-muted-foreground">{t("operations.expires")}: {formatDateTime(locale, transfer.expires_at)}</p>
   {transfer.direction === "deploy" && !transfer.task_id && !["failed", "canceled", "expired"].includes(transfer.state) && <div className="space-y-3"><Input type="file" disabled={busy} aria-label={t("operations.select_same_file_to_resume")} onChange={event => setFile(event.target.files?.[0])} />{file && <p className="text-sm">{file.name}</p>}
    <progress className="w-full" max={Math.max(transfer.size, 1)} value={bytes} aria-label={t("operations.uploaded_bytes")} />
    <div className="flex gap-2"><Button disabled={!file || busy} onClick={() => flow.request({ title: t("operations.upload_and_dispatch"), description: t("operations.only_missing_chunks_are_uploaded_node_tasks_are"), execute: async () => { void upload() } })}>{t("operations.upload_resume")}</Button>{busy && <Button variant="outline" onClick={() => controller.current?.abort()}>{t("operations.pause_upload")}</Button>}
    <Button variant="outline" disabled={busy || Boolean(transfer.missing_chunks?.length)} onClick={() => flow.request({ title: t("operations.verify_and_dispatch"), description: t("operations.if_scanning_is_still_running_check_again_later"), execute: async () => { await finalizeOperationTransfer(id); resource.reload() } })}>{t("operations.finalize")}</Button></div></div>}
   {transfer.download_available && <Button onClick={() => flow.request({ title: t("operations.download_collected_file"), description: t("operations.the_file_may_contain_sensitive_data_your_browser"), execute: async () => { const link = document.createElement("a"); link.href = operationDownloadURL(id); link.download = ""; link.click() } })}>{t("operations.download")}</Button>}
   {!["failed", "canceled", "expired"].includes(transfer.state) && <Button variant="outline" onClick={() => flow.request({ title: t("operations.cancel_transfer"), description: t("operations.changes_already_applied_on_a_node_cannot_be_auto"), execute: async () => { await cancelOperationTransfer(id); controller.current?.abort(); resource.reload() } })}>{t("operations.cancel_transfer")}</Button>}
   {!transfer.task_id && !["failed", "canceled", "expired"].includes(transfer.state) && <Button variant="outline" onClick={() => flow.request({ title: t("operations.discard_draft"), description: t("operations.discard_draft_description"), execute: async () => { await discardOperationTransferDraft(id); controller.current?.abort(); resource.reload() } })}>{t("operations.discard_draft")}</Button>}
   <div className="divide-y">{transfer.targets?.map(target => <div key={target.id} className="flex items-center justify-between gap-2 py-2 text-sm"><span>{target.node_name}</span><span>{(target.progress_bytes / 1048576).toFixed(2)} MiB</span><OperationStatus state={target.state} /></div>)}</div>
  </>}<OperationConfirm flow={flow} />
 </CardContent></Card>
}
