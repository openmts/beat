import { useState, type FormEvent } from "react"
import { useLocale } from "@/context/locale"
import { useGroups, useManagedNodes } from "@/hooks/use-api"
import { useConfirmedOperation } from "@/hooks/use-confirmed-operation"
import { Button } from "@/components/ui/button"
import { Checkbox } from "@/components/ui/checkbox"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Sheet, SheetContent, SheetDescription, SheetHeader, SheetTitle } from "@/components/ui/sheet"
import { AdminSelectField } from "@/components/admin-select-field"
import { createAgentRollout } from "@/lib/agent-rollout-api"
import type { AgentRelease, AgentRolloutInput } from "@/types/agent-rollouts"
import { AgentRolloutPolicyFields } from "./agent-rollout-policy"
import { defaultAgentRolloutPolicy, validateAgentRolloutPolicy } from "./agent-rollout-config"
import { OperationConfirm } from "./operation-confirm"
function TargetChoices({ title, values, selected, change }: { title: string; values: { id: string; name: string }[]; selected: string[]; change: (ids: string[]) => void }) {
 const { t } = useLocale()
 return <fieldset className="space-y-2"><legend className="font-medium">{title}</legend><div className="max-h-40 space-y-2 overflow-y-auto rounded border p-3">{values.length ? values.map(value => <label key={value.id} className="flex gap-2 text-sm"><Checkbox checked={selected.includes(value.id)} onCheckedChange={checked => change(checked ? [...selected, value.id] : selected.filter(id => id !== value.id))} />{value.name}</label>) : <p className="text-muted-foreground">{t("agentRollout.no_options")}</p>}</div></fieldset>
}
export function AgentRolloutCreate({ releases, saved, close }: { releases: AgentRelease[]; saved: () => Promise<unknown>; close: () => void }) {
 const { t } = useLocale(), nodes = useManagedNodes(), groups = useGroups(), flow = useConfirmedOperation(), [error, setError] = useState("")
 const [input, setInput] = useState<AgentRolloutInput>({ name: "", release_id: "", node_ids: [], group_ids: [], tags: [], policy: { ...defaultAgentRolloutPolicy }, action: "upgrade", acknowledge_rollback: false })
 const tags = [...new Set(nodes.data?.flatMap(node => node.tags ?? []) ?? [])].sort()
 const releaseOptions = releases.filter(release => release.verified && !release.revoked).map(release => ({ value: release.id, label: `${release.manifest.version} · ${release.manifest.channel}` }))
 const submit = (event: FormEvent) => {
  event.preventDefault()
  const invalid = validateAgentRolloutPolicy(input.policy, t)
  if (invalid) { setError(invalid); return }
  if (!input.name.trim() || new TextEncoder().encode(input.name.trim()).length > 128 || !input.release_id || !input.node_ids.length && !input.group_ids.length && !input.tags.length) { setError(t("agentRollout.error_targets")); return }
  if (input.action === "rollback" && !input.acknowledge_rollback) { setError(t("agentRollout.error_rollback_ack")); return }
  setError("")
  const releaseName = releaseOptions.find(release => release.value === input.release_id)?.label
  flow.request({ title: input.action === "rollback" ? t("agentRollout.create_rollback") : t("agentRollout.create_rollout"), description: t("agentRollout.create_description", { release: releaseName ?? "" }), execute: async () => { await createAgentRollout({ ...input, name: input.name.trim(), policy: { ...input.policy, window_start: new Date(input.policy.window_start).toISOString(), window_end: new Date(input.policy.window_end).toISOString() } }); await saved(); close() } })
 }
 return <Sheet open onOpenChange={open => { if (!open && !flow.busy) close() }}><SheetContent className="w-full overflow-y-auto p-6 sm:max-w-xl motion-reduce:transition-none"><SheetHeader><SheetTitle>{t("agentRollout.create_rollout")}</SheetTitle><SheetDescription>{t("agentRollout.sheet_description")}</SheetDescription></SheetHeader>
  <form onSubmit={submit} className="space-y-5"><Label htmlFor="rollout-name">{t("agentRollout.rollout_name")}</Label><Input id="rollout-name" required maxLength={128} value={input.name} onChange={event => setInput({ ...input, name: event.target.value })} />
   <AdminSelectField id="rollout-release" label={t("agentRollout.release")} value={input.release_id || "choose"} options={[{ value: "choose", label: t("agentRollout.choose_release") }, ...releaseOptions]} onValueChange={value => setInput({ ...input, release_id: value === "choose" ? "" : value })} />
   <AdminSelectField id="rollout-action" label={t("agentRollout.action")} value={input.action} options={[{ value: "upgrade", label: t("agentRollout.action_upgrade") }, { value: "rollback", label: t("agentRollout.action_rollback") }]} onValueChange={value => setInput({ ...input, action: value as "upgrade" | "rollback", acknowledge_rollback: false })} />
   {input.action === "rollback" && <label className="flex gap-2 text-sm"><Checkbox checked={input.acknowledge_rollback} onCheckedChange={checked => setInput({ ...input, acknowledge_rollback: Boolean(checked) })} />{t("agentRollout.rollback_ack")}</label>}
   {(nodes.error || groups.error) && <p role="alert">{nodes.error || groups.error}</p>}
   <TargetChoices title={t("agentRollout.nodes_including_private")} values={(nodes.data ?? []).map(node => ({ id: node.id, name: node.alias || node.name }))} selected={input.node_ids} change={node_ids => setInput({ ...input, node_ids })} />
   <TargetChoices title={t("agentRollout.groups")} values={groups.data ?? []} selected={input.group_ids} change={group_ids => setInput({ ...input, group_ids })} />
   <TargetChoices title={t("agentRollout.tags")} values={tags.map(tag => ({ id: tag, name: tag }))} selected={input.tags} change={tags => setInput({ ...input, tags })} />
   <AgentRolloutPolicyFields value={input.policy} change={policy => setInput({ ...input, policy })} />{error && <p role="alert" className="text-destructive">{error}</p>}<Button type="submit" disabled={nodes.loading || groups.loading || Boolean(nodes.error || groups.error) || flow.busy}>{t("agentRollout.review_targets")}</Button>
  </form><OperationConfirm flow={flow} />
 </SheetContent></Sheet>
}
