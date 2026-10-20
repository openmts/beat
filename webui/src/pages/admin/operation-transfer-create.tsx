import { useEffect, useRef, useState } from "react"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Checkbox } from "@/components/ui/checkbox"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { useLocale } from "@/context/locale"
import { useOperationResource } from "@/hooks/use-operation-resource"
import { useConfirmedOperation } from "@/hooks/use-confirmed-operation"
import { getExecutablePolicies, getOperationNodes } from "@/lib/operations-api"
import { createOperationTransfer, hashOperationFile } from "@/lib/operation-transfers"
import { OperationConfirm } from "./operation-confirm"
const load = async (signal: AbortSignal) => { const [policies, nodes] = await Promise.all([getExecutablePolicies(signal), getOperationNodes(signal)]); return { policies, nodes } }
export function OperationTransferCreate({ close, created }: { close: () => void; created: (id: string, file?: File) => void }) {
 const { t } = useLocale()
 const resource = useOperationResource(load), flow = useConfirmedOperation()
 const [direction, setDirection] = useState<"deploy" | "collect">("deploy"), [policyID, setPolicy] = useState(""), [rootID, setRoot] = useState("")
 const [nodes, setNodes] = useState<string[]>([]), [path, setPath] = useState(""), [file, setFile] = useState<File>(), [overwrite, setOverwrite] = useState(false), [executable, setExecutable] = useState(false)
 const [hashed, setHashed] = useState(0), controller = useRef<AbortController | null>(null)
 useEffect(() => () => controller.current?.abort(), [])
 const policies = resource.data?.policies.filter(value => value.policy.kinds.includes(direction)) ?? []
 const policy = policies.find(value => value.id === policyID)
 const roots = policy?.policy.roots.filter(root => direction === "deploy" ? root.write : root.read) ?? []
 const root = roots.find(value => value.id === rootID)
 const available = resource.data?.nodes.filter(node => policy?.node_ids.includes(node.id) || policy?.group_ids.includes(node.group_id)) ?? []
 const valid = Boolean(root && path.trim() && nodes.length && nodes.length <= (policy?.policy.maximum_targets ?? 0) && (direction === "collect" ? nodes.length === 1 : file && file.size <= root.maximum_bytes && file.size <= 2147483648))
 const prepare = () => {
  if (!valid || !root) return
  flow.request({ title: t("operations.create_file_transfer"), description: `${rootID}/${path} · ${nodes.length} ${t("operations.nodes_3")}`, execute: async () => {
   const active = new AbortController(); controller.current = active
   const digest = direction === "deploy" && file ? await hashOperationFile(file, { signal: active.signal, progress: setHashed }) : ""
   const transfer = await createOperationTransfer({ direction, filename: file?.name, policy_id: policyID, node_ids: nodes, group_ids: [], file: { root_id: rootID, relative_path: path, size: direction === "deploy" ? file?.size ?? 0 : 0, digest, mode: executable && root.executable ? 448 : 384, overwrite: overwrite && root.overwrite } })
   created(transfer.id, direction === "deploy" ? file : undefined)
  } })
 }
 return <Dialog open onOpenChange={open => { if (!open && !flow.busy) close() }}><DialogContent className="max-h-[90vh] overflow-auto sm:max-w-xl"><DialogHeader><DialogTitle>{t("operations.file_transfer")}</DialogTitle><DialogDescription>{t("operations.use_configured_agent_roots_deployments_are_uploa")}</DialogDescription></DialogHeader>
  {resource.error && <p role="alert" className="text-destructive">{resource.error}</p>}
  <Select items={[{ value: "deploy", label: t("operations.deploy_file") }, { value: "collect", label: t("operations.collect_file") }]} value={direction} onValueChange={value => { setDirection(value === "collect" ? "collect" : "deploy"); setPolicy(""); setRoot(""); setNodes([]) }}><SelectTrigger aria-label={t("operations.direction")}><SelectValue /></SelectTrigger><SelectContent><SelectItem value="deploy">{t("operations.deploy_file")}</SelectItem><SelectItem value="collect">{t("operations.collect_file")}</SelectItem></SelectContent></Select>
  <label className="space-y-2 text-sm">{t("operations.policy")}<Select items={policies.map(value => ({ value: value.id, label: value.name }))} value={policyID} onValueChange={value => { setPolicy(value ?? ""); setRoot(""); setNodes([]); setOverwrite(false); setExecutable(false) }}><SelectTrigger><SelectValue /></SelectTrigger><SelectContent>{policies.map(value => <SelectItem key={value.id} value={value.id}>{value.name}</SelectItem>)}</SelectContent></Select></label>
  <label className="space-y-2 text-sm">{t("operations.root")}<Select items={roots.map(value => ({ value: value.id, label: value.name || value.id }))} value={rootID} onValueChange={value => { setRoot(value ?? ""); setOverwrite(false); setExecutable(false) }}><SelectTrigger><SelectValue /></SelectTrigger><SelectContent>{roots.map(value => <SelectItem key={value.id} value={value.id}>{value.name || value.id}</SelectItem>)}</SelectContent></Select></label>
  <label className="space-y-2 text-sm">{t("operations.relative_path")}<Input value={path} maxLength={4096} placeholder="releases/app.tar.gz" onChange={event => setPath(event.target.value)} /></label>
  <fieldset className="max-h-36 space-y-2 overflow-auto rounded-md border p-3"><legend>{t("operations.nodes_4")}</legend>{available.map(node => <label key={node.id} className="flex gap-2 text-sm"><Checkbox checked={nodes.includes(node.id)} onCheckedChange={checked => setNodes(previous => checked ? direction === "collect" ? [node.id] : [...previous, node.id] : previous.filter(id => id !== node.id))} />{node.alias || node.name}</label>)}</fieldset>
  {direction === "deploy" && <><Input type="file" aria-label={t("operations.local_file")} onChange={event => { setFile(event.target.files?.[0]); setHashed(0) }} />{root && <p className="text-xs text-muted-foreground">{t("operations.size_limit")}: {(root.maximum_bytes / 1048576).toFixed(0)} MiB</p>}
   {root?.overwrite && <label className="flex gap-2 text-sm"><Checkbox checked={overwrite} onCheckedChange={value => setOverwrite(Boolean(value))} />{t("operations.allow_replacing_target_file")}</label>}{root?.executable && <label className="flex gap-2 text-sm"><Checkbox checked={executable} onCheckedChange={value => setExecutable(Boolean(value))} />{t("operations.make_executable")}</label>}</>}
  {flow.busy && file && <p role="status">{t("operations.hashing_file")}: {file.size ? Math.round(hashed / file.size * 100) : 100}%</p>}
  <DialogFooter><Button variant="outline" onClick={close} disabled={flow.busy}>{t("operations.cancel")}</Button><Button disabled={!valid || flow.busy} onClick={prepare}>{t("operations.continue")}</Button></DialogFooter><OperationConfirm flow={flow} />
 </DialogContent></Dialog>
}
