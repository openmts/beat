import { useState } from "react"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Textarea } from "@/components/ui/textarea"
import { Checkbox } from "@/components/ui/checkbox"
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { useLocale } from "@/context/locale"
import { useOperationResource } from "@/hooks/use-operation-resource"
import { useConfirmedOperation } from "@/hooks/use-confirmed-operation"
import { createOperationTask, getExecutablePolicies, getOperationNodes, operationEncode, operationRequestID } from "@/lib/operations-api"
import type { OperationSnippetDraft } from "./operation-snippets"
import { OperationConfirm } from "./operation-confirm"

const loadSources = async (signal: AbortSignal) => { const [policies, nodes] = await Promise.all([getExecutablePolicies(signal), getOperationNodes(signal)]); return { policies, nodes } }
export function OperationCreate({ close, created, preset }: { close: () => void; created: (id: string) => void; preset?: OperationSnippetDraft }) {
 const { t } = useLocale()
 const source = useOperationResource(loadSources)
 const flow = useConfirmedOperation()
 const [policyId, setPolicy] = useState(""), [nodes, setNodes] = useState<string[]>([]), [command, setCommand] = useState(preset?.content ?? "")
 const [shell, setShell] = useState(preset?.snippet.shell_family ?? "sh"), [timeout, setTimeoutSeconds] = useState(60), [root, setRoot] = useState(false)
 const [dedupe, setDedupe] = useState(() => operationRequestID())
 const policy = source.data?.policies.find(value => value.id === policyId)
 const available = source.data?.nodes.filter(node => policy?.node_ids.includes(node.id) || policy?.group_ids.includes(node.group_id)) ?? []
 const changed = () => setDedupe(operationRequestID())
 const valid = policy?.policy.enabled && nodes.length > 0 && nodes.length <= policy.policy.maximum_targets && command.trim() && new TextEncoder().encode(command).length <= 65536 && !command.includes("\0") && timeout >= 1 && timeout <= policy.policy.timeout_seconds
 const submit = () => {
  if (!policy || !valid) return
  const payload = { snippet_id: preset?.snippet.id, snippet_version: preset?.snippet.version, dedupe_key: dedupe, policy_id: policy.id, shell_family: shell, command: operationEncode(command), node_ids: nodes, group_ids: [], limits: { kind: preset ? "snippet" : "shell", targets: nodes.length, timeout_seconds: timeout, output_bytes: policy.policy.output_bytes, run_as_root: root } }
  flow.request({ title: t("operations.confirm_command_dispatch"), description: t("operations.execute_on_nodes_with_timeout", { nodes: nodes.length, timeout }),
   execute: async () => { const result = await createOperationTask(payload); setCommand(""); created(result.id) } })
 }
 return <Dialog open onOpenChange={open => { if (!open && !flow.busy) close() }}><DialogContent className="max-h-[90vh] overflow-auto sm:max-w-2xl"><DialogHeader><DialogTitle>{t("operations.create_remote_command_task")}</DialogTitle><DialogDescription>{t("operations.select_targets_within_an_enabled_policy_agents_e")}</DialogDescription></DialogHeader>
  {source.error && <p role="alert" className="text-destructive">{source.error}</p>}
  <label className="space-y-2 text-sm">{t("operations.execution_policy")}<Select items={(source.data?.policies ?? []).filter(p => p.policy.kinds.includes(preset ? "snippet" : "shell")).map(p => ({ value: p.id, label: p.name }))} value={policyId} onValueChange={value => { setPolicy(value ?? ""); setNodes([]); setRoot(false); changed() }}><SelectTrigger><SelectValue placeholder={t("operations.select_policy")} /></SelectTrigger><SelectContent>{source.data?.policies.filter(p => p.policy.kinds.includes(preset ? "snippet" : "shell")).map(p => <SelectItem key={p.id} value={p.id}>{p.name}</SelectItem>)}</SelectContent></Select></label>
  {!source.loading && !source.data?.policies.length && <p className="text-sm text-muted-foreground">{t("operations.no_executable_policies_ask_an_owner_to_configure")}</p>}
  <fieldset className="max-h-44 space-y-2 overflow-auto rounded-md border p-3"><legend className="px-1 text-sm">{t("operations.target_nodes")} ({nodes.length})</legend>{available.map(node => <label key={node.id} className="flex items-center gap-2 text-sm"><Checkbox checked={nodes.includes(node.id)} onCheckedChange={value => { setNodes(previous => value ? [...previous, node.id] : previous.filter(id => id !== node.id)); changed() }} />{node.alias || node.name}<span className="text-xs text-muted-foreground">{node.status}</span></label>)}</fieldset>
  <div className="grid grid-cols-2 gap-3"><label className="space-y-2 text-sm">Shell<Select items={[{ value: "sh", label: "POSIX sh" }, { value: "powershell", label: "PowerShell" }]} value={shell} disabled={Boolean(preset)} onValueChange={value => { setShell(value ?? "sh"); changed() }}><SelectTrigger><SelectValue /></SelectTrigger><SelectContent><SelectItem value="sh">POSIX sh</SelectItem><SelectItem value="powershell">PowerShell</SelectItem></SelectContent></Select></label><label className="space-y-2 text-sm">{t("operations.timeout_seconds")}<Input type="number" min={1} max={policy?.policy.timeout_seconds ?? 3600} value={timeout} onChange={e => { setTimeoutSeconds(Number(e.target.value)); changed() }} /></label></div>
  <label className="space-y-2 text-sm">{t("operations.command")}<Textarea className="min-h-36 font-mono" readOnly={Boolean(preset)} value={command} onChange={e => { setCommand(e.target.value); changed() }} spellCheck={false} /></label>
  {policy?.policy.allow_root && <label className="flex items-center gap-2 text-sm"><Checkbox checked={root} onCheckedChange={value => { setRoot(Boolean(value)); changed() }} />{t("operations.request_root_execution_requires_local_agent_perm")}</label>}
  <p className="text-xs text-muted-foreground">{t("operations.if_the_response_is_lost_check_task_history_first")}</p>
  <DialogFooter><Button variant="outline" onClick={close} disabled={flow.busy}>{t("operations.cancel")}</Button><Button onClick={submit} disabled={!valid || flow.busy}>{t("operations.review_and_dispatch")}</Button></DialogFooter><OperationConfirm flow={flow} />
 </DialogContent></Dialog>
}
