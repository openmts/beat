import { useState } from "react"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Checkbox } from "@/components/ui/checkbox"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { useAuth } from "@/context/auth"
import { operationInputNumber, operationMaximumScope, validOperationPolicy } from "./operation-policy-validation"
import { useLocale } from "@/context/locale"
import { useConfirmedOperation } from "@/hooks/use-confirmed-operation"
import { useOperationResource } from "@/hooks/use-operation-resource"
import { disableOperationPolicy, getOperationGroups, getOperationNodes, getOperationPolicies, getOperationServerStatus, saveOperationPolicy } from "@/lib/operations-api"
import type { OperationPolicyRecord } from "@/types/operations"
import { OperationRetention } from "./operation-retention"
import { OperationRoots } from "./operation-roots"
import { operationMutationError } from "./operation-mutation-error"
import { OperationConfirm } from "./operation-confirm"
const load = async (signal: AbortSignal) => { const [policies, nodes, groups, status] = await Promise.all([getOperationPolicies(signal), getOperationNodes(signal), getOperationGroups(signal), getOperationServerStatus(signal)]); return { policies, nodes, groups, status } }
const newPolicy = (): OperationPolicyRecord => ({ id: "", name: "", version: 0, node_ids: [], group_ids: [], policy: { enabled: true, kinds: ["shell", "snippet"], maximum_targets: 20, maximum_active_tasks: 20, concurrency: 1, timeout_seconds: 60, output_bytes: 65536, allow_root: false, require_scan: true, roots: [] } })
export function OperationPolicies() {
 const { principal } = useAuth(), { t } = useLocale()
 return principal?.user.role === "owner" && principal.user.id ? <OwnerPolicies key={principal.user.id} /> : <p role="status">{t("operations.only_owners_can_manage_execution_policies")}</p>
}
function OwnerPolicies() {
 const { t } = useLocale()
 const resource = useOperationResource(load), flow = useConfirmedOperation()
 const [draft, setDraft] = useState<OperationPolicyRecord | null>(null)
 const update = (patch: Partial<OperationPolicyRecord>) => setDraft(value => value && ({ ...value, ...patch }))
 const policyUpdate = (patch: Partial<OperationPolicyRecord["policy"]>) => setDraft(value => value && ({ ...value, policy: { ...value.policy, ...patch } }))
 const save = () => {
  if (!draft || !validOperationPolicy(draft)) return
  const input = structuredClone(draft)
  flow.request({ title: t("operations.save_execution_policy"), description: t("operations.the_stricter_server_or_local_agent_limit_applies"), execute: async () => { try { await saveOperationPolicy(input) } catch (error) { throw operationMutationError(error, t("operations.policy_version_changed_your_draft_is_preserved_r")) }; setDraft(null); resource.reload() } })
 }
 return <div className="space-y-4"><div className="flex items-center justify-between"><h2 className="font-semibold">{t("operations.execution_policies")}</h2><div className="flex gap-2"><Button variant="outline" disabled={resource.loading || flow.busy} onClick={resource.reload}>{t("operations.refresh")}</Button><Button disabled={flow.busy} onClick={() => setDraft(newPolicy())}>{t("operations.new_policy")}</Button></div></div>
  {resource.error && <p role="alert" className="text-destructive">{resource.error}</p>}
  {resource.loading && <p role="status">{t("operations.loading")}</p>}
  {resource.data?.policies.length === 0 && <p role="status">{t("operations.no_policies")}</p>}
  {resource.data?.policies.map(value => <Card key={value.id}><CardContent className="flex flex-wrap items-center gap-3 pt-4"><div className="flex-1"><p className="font-medium">{value.name}</p><p className="text-xs text-muted-foreground">v{value.version} · {value.policy.enabled ? (t("operations.enabled")) : (t("operations.disabled"))} · {value.node_ids.length} {t("operations.nodes")} / {value.group_ids.length} {t("operations.groups")}</p></div><Button variant="outline" disabled={flow.busy} onClick={() => setDraft({ ...structuredClone(value), policy: { ...structuredClone(value.policy), kinds: value.policy.kinds ?? [], roots: value.policy.roots ?? [] } })}>{t("operations.edit")}</Button><Button variant="outline" disabled={flow.busy || !value.policy.enabled} onClick={() => flow.request({ title: t("operations.disable_policy"), description: value.name, execute: async () => { await disableOperationPolicy(value.id); resource.reload() } })}>{t("operations.disable")}</Button></CardContent></Card>)}
  {draft && <Card><CardHeader><CardTitle>{draft.id ? (t("operations.edit_policy")) : (t("operations.new_policy_2"))}</CardTitle></CardHeader><CardContent className="space-y-4"><fieldset disabled={flow.busy} className="space-y-4">
   <label className="block space-y-2 text-sm">{t("operations.name")}<Input maxLength={200} value={draft.name} onChange={e => update({ name: e.target.value })} /></label>
   <div className="flex flex-wrap gap-4">{(["enabled", "allow_root", "require_scan"] as const).map((key, index) => <label key={key} className="flex items-center gap-2 text-sm"><Checkbox checked={draft.policy[key]} onCheckedChange={value => policyUpdate({ [key]: Boolean(value) })} />{t(["operations.policy_enabled", "operations.policy_allow_root", "operations.policy_require_scan"][index])}</label>)}</div>
   <div className="flex gap-4">{["shell", "snippet", "deploy", "collect"].map(kind => <label key={kind} className="flex items-center gap-2 text-sm"><Checkbox checked={draft.policy.kinds.includes(kind)} onCheckedChange={checked => policyUpdate({ kinds: checked ? [...draft.policy.kinds, kind] : draft.policy.kinds.filter(value => value !== kind) })} />{({ shell: t("operations.command_2"), snippet: t("operations.snippet"), deploy: t("operations.file_deploy"), collect: t("operations.file_collect") })[kind]}</label>)}</div>
   <div className="grid gap-3 sm:grid-cols-3">{([['maximum_targets',200,t("operations.maximum_targets")],['maximum_active_tasks',1000,t("operations.active_task_limit")],['concurrency',4,t("operations.concurrency")],['timeout_seconds',3600,t("operations.timeout_seconds_2")],['output_bytes',1048576,t("operations.output_bytes")]] as const).map(([key,max,label]) => <label key={key} className="space-y-2 text-sm">{label}<Input type="number" min={1} max={max} value={Number.isFinite(draft.policy[key]) ? draft.policy[key] : ""} onChange={e => policyUpdate({ [key]: operationInputNumber(e.target.value) })} /></label>)}</div>
   <ScopeSelect title={t("operations.nodes_2")} options={resource.data?.nodes.map(node => ({ id: node.id, name: node.alias || node.name })) ?? []} selected={draft.node_ids} limit={operationMaximumScope - draft.group_ids.length} change={node_ids => update({ node_ids })} />
   <ScopeSelect title={t("operations.groups_2")} options={resource.data?.groups ?? []} selected={draft.group_ids} limit={operationMaximumScope - draft.node_ids.length} change={group_ids => update({ group_ids })} />
   <div className="flex flex-wrap gap-3">{["sh", "powershell"].map(shell => <label key={shell} className="flex items-center gap-2 text-sm"><Checkbox checked={draft.policy.supported_shells?.includes(shell) ?? false} onCheckedChange={checked => policyUpdate({ supported_shells: checked ? [...(draft.policy.supported_shells ?? []), shell] : draft.policy.supported_shells?.filter(value => value !== shell) })} />{shell}</label>)}<span className="text-xs text-muted-foreground">{t("operations.if_unrestricted_any_shell_supported_by_the_agent")}</span></div>
   {draft.policy.require_scan && resource.data && !resource.data.status.scanner_configured && <p role="status" className="text-sm text-muted-foreground">{t("operations.no_file_scanner_is_configured_on_the_server_file")}</p>}
   <OperationRetention value={draft.policy.retention} change={retention => policyUpdate({ retention })} />
   <OperationRoots value={draft.policy.roots} change={roots => policyUpdate({ roots })} />
   {!validOperationPolicy(draft) && <p role="status" className="text-sm text-muted-foreground">{t("operations.complete_the_name_scope_and_valid_policy_limits")}</p>}
   <div className="flex justify-end gap-2"><Button variant="outline" onClick={() => setDraft(null)}>{t("operations.cancel")}</Button><Button disabled={!validOperationPolicy(draft)} onClick={save}>{t("operations.save")}</Button></div>
  </fieldset></CardContent></Card>}<OperationConfirm flow={flow} />
 </div>
}
function ScopeSelect({ title, options, selected, limit, change }: { title: string; options: { id: string; name: string }[]; selected: string[]; limit: number; change: (ids: string[]) => void }) {
 const [search, setSearch] = useState(""), { t } = useLocale()
 const filtered = options.filter(value => value.name.toLocaleLowerCase().includes(search.toLocaleLowerCase()))
 return <fieldset className="max-h-44 space-y-2 overflow-auto rounded-md border p-3"><legend className="px-1 text-sm">{title} ({selected.length}/{limit})</legend><Input aria-label={title + " search"} value={search} onChange={event => setSearch(event.target.value)} />{filtered.slice(0, 100).map(value => <label key={value.id} className="flex items-center gap-2 text-sm"><Checkbox checked={selected.includes(value.id)} disabled={!selected.includes(value.id) && selected.length >= limit} onCheckedChange={checked => change(checked ? [...selected, value.id] : selected.filter(id => id !== value.id))} />{value.name}</label>)}{filtered.length > 100 && <p className="text-xs text-muted-foreground">{t("operations.showing_the_first_100_matches_refine_your_search")}</p>}</fieldset>
}
