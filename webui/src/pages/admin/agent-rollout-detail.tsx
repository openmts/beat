import { useCallback } from "react"
import { PlayIcon, PauseIcon, StepForwardIcon, BanIcon } from "lucide-react"
import { Button } from "@/components/ui/button"
import { Badge } from "@/components/ui/badge"
import { Skeleton } from "@/components/ui/skeleton"
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip"
import { useLocale } from "@/context/locale"
import { useAgentRolloutData, useAgentRolloutPage } from "@/hooks/use-agent-rollout-data"
import { useConfirmedOperation } from "@/hooks/use-confirmed-operation"
import { commandAgentRollout, deleteAgentRollout, getAgentRelease, getAgentRollout, listAgentTargets } from "@/lib/agent-rollout-api"
import type { AgentRollout } from "@/types/agent-rollouts"
import { agentObservationRemaining, canCommandAgentRollout, canDeleteAgentRollout } from "./agent-rollout-config"
import { OperationConfirm } from "./operation-confirm"
const columns = ["agentRollout.column_node", "agentRollout.column_source_version", "agentRollout.column_platform", "agentRollout.column_wave", "agentRollout.column_state", "agentRollout.column_reason", "agentRollout.column_health"] as const
export function AgentPageControls({ previous, next }: { previous?: () => void; next?: () => void }) { const { t } = useLocale(); return <div className="flex gap-2"><Button variant="outline" disabled={!previous} onClick={previous}>{t("agentRollout.previous_page")}</Button><Button variant="outline" disabled={!next} onClick={next}>{t("agentRollout.next_page")}</Button></div> }
// 命令表第二列是文案键：标签与确认弹窗共用一份映射，命令本身仍是协议里的稳定值。
const commands = [ { command: "promote", labelKey: "agentRollout.command_promote", detailKey: "agentRollout.command_promote_detail", icon: StepForwardIcon }, { command: "pause", labelKey: "agentRollout.command_pause", detailKey: "agentRollout.command_abort_detail", icon: PauseIcon }, { command: "resume", labelKey: "agentRollout.command_resume", detailKey: "agentRollout.command_resume_detail", icon: PlayIcon }, { command: "abort", labelKey: "agentRollout.command_abort", detailKey: "agentRollout.command_abort_detail", icon: BanIcon } ] as const
function AgentRolloutCommands({ value, saved, removed }: { value: AgentRollout; saved: () => Promise<unknown>; removed: () => void }) {
 const { t } = useLocale(), flow = useConfirmedOperation()
 return <><div className="flex flex-wrap gap-2">{commands.map(({ command, labelKey, detailKey, icon: Icon }) => <Tooltip key={command}><TooltipTrigger render={<Button variant={command === "abort" ? "destructive" : "outline"} disabled={!canCommandAgentRollout(value, command)} onClick={() => flow.request({ title: t(labelKey), description: t("agentRollout.command_description", { label: t(labelKey), name: value.name, detail: t(detailKey) }), uncertain: command === "resume", execute: async () => { await commandAgentRollout(value.id, command); await saved() } })} />}><Icon />{t(labelKey)}</TooltipTrigger><TooltipContent>{t(labelKey)}</TooltipContent></Tooltip>)}<Button variant="destructive" disabled={!canDeleteAgentRollout(value)} onClick={() => flow.request({ title: t("agentRollout.delete_rollout"), description: t("agentRollout.delete_rollout_description", { name: value.name }), execute: async () => { await deleteAgentRollout(value.id); await saved(); removed() } })}>{t("agentRollout.delete_rollout")}</Button></div><OperationConfirm flow={flow} /></>
}
function AgentRolloutRelease({ id }: { id: string }) {
 const { t } = useLocale(), read = useCallback((signal: AbortSignal) => getAgentRelease(id, signal), [id]), source = useAgentRolloutData(read)
 return <p className="text-sm text-muted-foreground">{t("agentRollout.target_release", { release: source.data ? `${source.data.manifest.version} · ${source.data.manifest.channel}` : source.error || t("agentRollout.loading") })}</p>
}
export function AgentRolloutDetailPanel({ id, removed = () => {} }: { id: string; removed?: () => void }) {
 const { t } = useLocale(), read = useCallback((signal: AbortSignal) => getAgentRollout(id, signal), [id]), targetsRead = useCallback((cursor: string, signal: AbortSignal) => listAgentTargets(id, cursor, signal), [id])
 const source = useAgentRolloutData(read), targets = useAgentRolloutPage(targetsRead), value = source.data
 const refresh = async () => { await source.refresh(); await targets.refresh() }
 if (source.loading) return <Skeleton className="h-64" />
 if (!value) return <p role="alert">{source.error}</p>
 return <section className="space-y-4" aria-label={t("agentRollout.rollout_detail")}><div className="flex flex-wrap gap-3"><h2 className="text-lg font-semibold">{value.name}</h2><Badge variant="outline">{value.action} · {value.state}</Badge></div>
  <AgentRolloutRelease id={value.release_id} /><p>{t("agentRollout.wave_meta", { current: value.current_wave < 0 ? t("agentRollout.not_started") : value.current_wave + 1, total: value.total_waves, reason: value.reason || t("agentRollout.no_hold_reason") })}</p><p className="text-sm">{t("agentRollout.policy_observation", { observation: value.policy.observation_seconds, freshness: value.policy.report_freshness_seconds, concurrent: value.policy.max_concurrent })}</p>
  <p className="text-sm">{t("agentRollout.policy_health", { restarts: value.policy.max_restarts, protocol: value.policy.max_protocol_errors, metric: value.policy.max_metric_errors, task: value.policy.max_task_failures, canary: value.policy.canary_count, wave: value.policy.wave_size })}</p>
  <p className="text-sm text-muted-foreground">{t("agentRollout.policy_maintenance", { start: value.policy.window_start, end: value.policy.window_end })}</p><AgentRolloutCommands value={value} saved={refresh} removed={removed} />
  <h3 className="font-semibold">{t("agentRollout.frozen_targets")}</h3>{targets.error && <p role="alert">{targets.error}</p>}{targets.loading && <Skeleton className="h-32" />}
  {targets.data && <><p className="text-sm text-muted-foreground">{t("agentRollout.targets_summary", { total: targets.data.items.length, succeeded: targets.data.items.filter(target => target.state === "succeeded").length, held: targets.data.items.filter(target => target.state === "held").length, failed: targets.data.items.filter(target => target.state === "failed").length, rolledBack: targets.data.items.filter(target => target.state === "rolled_back").length })}</p><div className="overflow-x-auto"><table className="w-full text-left text-sm"><thead><tr>{columns.map(column => <th key={column} className="p-2">{t(column)}</th>)}</tr></thead><tbody>{targets.data.items.map(target => <tr key={target.id} className="border-t"><td className="p-2">{target.name || t("agentRollout.unnamed_node")}</td><td className="p-2">{target.source_version || t("agentRollout.unknown")}</td><td className="p-2">{target.os}/{target.arch}</td><td className="p-2">{target.wave + 1}</td><td className="p-2">{target.state}</td><td className="p-2">{target.reason || "—"}</td><td className="p-2"><p>{agentObservationRemaining({ target, serverTime: targets.data?.server_time, seconds: value.policy.observation_seconds, t })}</p><p className="text-xs text-muted-foreground">{t("agentRollout.target_report", { last: target.last_report_at || t("agentRollout.none"), lease: target.lease_expires_at || t("agentRollout.none") })}</p></td></tr>)}</tbody></table></div>{!targets.data.items.length && <p>{t("agentRollout.no_targets")}</p>}</>}
  <AgentPageControls previous={targets.previous} next={targets.next} />
 </section>
}
