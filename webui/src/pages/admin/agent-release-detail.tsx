import { useCallback } from "react"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Skeleton } from "@/components/ui/skeleton"
import { useAgentRolloutData } from "@/hooks/use-agent-rollout-data"
import { useLocale } from "@/context/locale"
import { useConfirmedOperation } from "@/hooks/use-confirmed-operation"
import { deleteAgentRelease, getAgentRelease, revokeAgentRelease } from "@/lib/agent-rollout-api"
import { AgentArtifactUpload } from "./agent-release-files"
import { OperationConfirm } from "./operation-confirm"
export function AgentReleaseDetailPanel({ id, saved, removed = () => {} }: { id: string; saved: () => Promise<unknown>; removed?: () => void }) {
 const { t } = useLocale(), read = useCallback((signal: AbortSignal) => getAgentRelease(id, signal), [id]), source = useAgentRolloutData(read), flow = useConfirmedOperation()
 const release = source.data
 const refresh = async () => { await source.refresh(); await saved() }
 if (source.loading) return <Skeleton className="h-64" />
 if (!release) return <p role="alert">{source.error}</p>
 return <section className="space-y-4" aria-label={t("agentRollout.release_detail")}><div className="flex flex-wrap items-center gap-3"><h2 className="text-lg font-semibold">{release.manifest.version} · {release.manifest.channel}</h2><Badge variant="outline">{release.revoked ? t("agentRollout.revoked") : release.verified ? t("agentRollout.verified_at_import") : t("agentRollout.unverified")}</Badge><Button variant="destructive" disabled={release.revoked} onClick={() => flow.request({ title: t("agentRollout.revoke_release"), description: t("agentRollout.revoke_description", { version: release.manifest.version, channel: release.manifest.channel }), execute: async () => { await revokeAgentRelease(id); await refresh() } })}>{t("agentRollout.revoke_release")}</Button><Button variant="destructive" disabled={!release.revoked} onClick={() => flow.request({ title: t("agentRollout.delete_release"), description: t("agentRollout.delete_release_description", { version: release.manifest.version, channel: release.manifest.channel }), execute: async () => { await deleteAgentRelease(id); await saved(); removed() } })}>{t("agentRollout.delete_release")}</Button></div>
  <p className="text-sm text-muted-foreground">{t("agentRollout.release_facts", { expires: release.manifest.expires_at, minimum: release.manifest.minimum_protocol, maximum: release.manifest.maximum_protocol, schema: release.manifest.minimum_server_schema })}</p>
  {release.manifest.artifacts.map(artifact => <AgentArtifactUpload key={artifact.id} releaseID={id} artifact={artifact} state={release.artifacts.find(item => item.id === artifact.id)?.state ?? t("agentRollout.state_unavailable")} disabled={release.revoked || !release.verified} saved={refresh} />)}<OperationConfirm flow={flow} />
 </section>
}
