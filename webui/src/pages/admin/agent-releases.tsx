import { useState, type ReactNode } from "react"
import { Link } from "react-router"
import { ArrowRightIcon, GitBranchIcon, InfoIcon, ServerIcon } from "lucide-react"
import { useAuth } from "@/context/auth"
import { useLocale } from "@/context/locale"
import { Button } from "@/components/ui/button"
import { Badge } from "@/components/ui/badge"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Skeleton } from "@/components/ui/skeleton"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { getAgentRolloutStatus, listAgentReleases, listAgentRollouts } from "@/lib/agent-rollout-api"
import { useAgentRolloutData, useAgentRolloutPage } from "@/hooks/use-agent-rollout-data"
import { AgentReleaseImport } from "./agent-release-files"
import { AgentReleaseRemote } from "./agent-release-remote"
import { AgentReleaseDetailPanel } from "./agent-release-detail"
import AgentRetentionPanel from "./agent-retention-panel"
import { AgentRolloutCreate } from "./agent-rollout-create"
import { AgentPageControls, AgentRolloutDetailPanel } from "./agent-rollout-detail"

export default function AgentReleasesPage() {
  const { t } = useLocale(), { principal } = useAuth()
  if (principal?.user.role !== "owner") return <main className="space-y-3"><h1 className="text-2xl font-semibold">{t("agentRollout.title")}</h1><p role="status">{t("agentRollout.owner_only")}</p></main>
  return <AgentRolloutAvailability key={`${principal.user.id}/${principal.session.id}`} />
}

function AgentPageHeader({ children }: { children?: ReactNode }) {
  const { t } = useLocale()
  return <header className="space-y-2"><div className="flex flex-wrap items-center gap-2"><h1 className="text-2xl font-semibold">{t("agentRollout.title")}</h1><Badge variant="secondary">{t("agentRollout.advanced_tag")}</Badge></div><p className="max-w-3xl text-sm text-muted-foreground">{t("agentRollout.description")}</p>{children}</header>
}

function AgentUpgradeGuide() {
  const { t } = useLocale()
  return <section aria-labelledby="agent-upgrade-guide" className="space-y-3 rounded-xl border bg-muted/20 p-4"><div><h2 id="agent-upgrade-guide" className="font-semibold">{t("agentRollout.guide_title")}</h2><p className="text-sm text-muted-foreground">{t("agentRollout.guide_description")}</p></div>
    <div className="grid gap-3 md:grid-cols-2"><Card size="sm"><CardHeader><CardTitle className="flex items-center gap-2"><ServerIcon aria-hidden />{t("agentRollout.join_title")}</CardTitle><CardDescription>{t("agentRollout.join_description")}</CardDescription></CardHeader><CardContent><Button size="sm" variant="outline" nativeButton={false} render={<Link to="/admin/agent-setup" />}>{t("agentRollout.join_action")}<ArrowRightIcon /></Button></CardContent></Card>
      <Card size="sm" className="border-primary/25 bg-primary/5"><CardHeader><div className="flex flex-wrap items-center justify-between gap-2"><CardTitle className="flex items-center gap-2"><GitBranchIcon aria-hidden />{t("agentRollout.upgrade_title")}</CardTitle><Badge variant="outline">{t("agentRollout.current_area")}</Badge></div><CardDescription>{t("agentRollout.upgrade_description")}</CardDescription></CardHeader></Card></div>
  </section>
}

function AgentRolloutAvailability() {
  const { t } = useLocale(), source = useAgentRolloutData(getAgentRolloutStatus)
  if (source.data?.ready) return <AgentReleasesWorkspace />
  const title = t("agentRollout.unavailable_title")
  return <main className="space-y-5"><AgentPageHeader /><AgentUpgradeGuide />
    {source.loading ? <Skeleton className="h-40" /> : <section role={source.error ? "alert" : "status"} className="space-y-4 rounded-xl border bg-card p-4"><div className="flex items-start gap-3"><InfoIcon className="mt-0.5 size-5 shrink-0 text-muted-foreground" aria-hidden /><div className="space-y-1"><h2 className="font-semibold">{title}</h2><p className="text-sm text-muted-foreground">{source.error || t("agentRollout.unavailable_hint")}</p></div></div>{source.data && <details className="rounded-lg border bg-muted/20 px-3 py-2 text-sm"><summary className="cursor-pointer font-medium">{t("agentRollout.diagnostic_details")}</summary><div className="mt-2 space-y-1 text-muted-foreground"><p>{t("agentRollout.module_reason", { reason: source.data.reason })}</p><p>{t("agentRollout.status_line", { enabled: String(source.data.enabled), configured: String(source.data.configured), ready: String(source.data.ready) })}</p></div></details>}<div className="flex flex-wrap gap-2"><Button variant="outline" nativeButton={false} render={<Link to="/admin/agent-setup" />}><ServerIcon />{t("agentRollout.join_action")}<ArrowRightIcon /></Button><Button variant="outline" onClick={() => void source.refresh()}>{t("agentRollout.retry_status")}</Button></div></section>}
  </main>
}

function AgentReleasesWorkspace() {
  const { t } = useLocale(), releases = useAgentRolloutPage(listAgentReleases), rollouts = useAgentRolloutPage(listAgentRollouts)
  const [releaseID, setReleaseID] = useState(""), [rolloutID, setRolloutID] = useState(""), [creating, setCreating] = useState(false)
  const unavailable = Boolean(releases.error || rollouts.error), loaded = releases.data !== null && rollouts.data !== null
  const refresh = async () => { await Promise.all([releases.refresh(), rollouts.refresh()]) }
  return <main className="space-y-6">
    <AgentPageHeader><Button variant="outline" onClick={() => void refresh()}>{t("agentRollout.refresh_workspace")}</Button></AgentPageHeader>
    <AgentUpgradeGuide />
    {unavailable && <div role="alert" className="rounded-lg border border-destructive p-4">{releases.error || rollouts.error}</div>}
    {!loaded && !unavailable && <Skeleton className="h-48" />}
    <Tabs defaultValue="releases"><TabsList className="grid h-auto! w-full grid-cols-3 items-stretch gap-1">
      <TabsTrigger className="h-auto min-h-8 whitespace-normal px-1.5 py-1.5 text-center leading-tight" value="releases">{t("agentRollout.tab_releases")}</TabsTrigger>
      <TabsTrigger className="h-auto min-h-8 whitespace-normal px-1.5 py-1.5 text-center leading-tight" value="rollouts">{t("agentRollout.tab_rollouts")}</TabsTrigger>
      <TabsTrigger className="h-auto min-h-8 whitespace-normal px-1.5 py-1.5 text-center leading-tight" value="platforms">{t("agentRollout.tab_platforms")}</TabsTrigger>
    </TabsList>
      <TabsContent value="releases" className="mt-4 space-y-6">{releases.data && !unavailable && <>
        <AgentReleaseRemote releases={releases.data.items} saved={releases.refresh} onView={setReleaseID} />
        <section className="space-y-3">
          <div className="flex flex-wrap items-center justify-between gap-2"><div><h2 className="font-semibold">{t("agentRollout.local_releases")}</h2><p className="text-sm text-muted-foreground">{t("agentRollout.local_releases_hint")}</p></div>{releaseID && <Button variant="outline" onClick={() => setReleaseID("")}>{t("agentRollout.hide_detail")}</Button>}</div>
          <div className="grid gap-3">{releases.data.items.map(release => <article key={release.id} className="flex flex-wrap items-center justify-between gap-3 rounded-lg border p-4"><div className="space-y-2"><h3 className="font-semibold">{release.manifest.version} · {release.manifest.channel}</h3><p className="text-sm text-muted-foreground">{t("agentRollout.release_meta", { minimum: release.manifest.minimum_protocol, maximum: release.manifest.maximum_protocol, count: release.manifest.artifacts.length })}</p><Badge variant="outline">{release.revoked ? t("agentRollout.revoked") : release.verified ? t("agentRollout.verified_at_import") : t("agentRollout.unverified")}</Badge></div><Button variant="outline" onClick={() => setReleaseID(release.id)}>{t("agentRollout.view_version", { version: release.manifest.version })}</Button></article>)}</div>
          {!releases.data.items.length && <p>{t("agentRollout.no_releases")}</p>}<AgentPageControls previous={releases.previous} next={releases.next} />
        </section>
        {releaseID && <AgentReleaseDetailPanel key={releaseID} id={releaseID} saved={releases.refresh} removed={() => setReleaseID("")} />}
        {!releaseID && <details className="rounded-xl border p-4"><summary className="cursor-pointer font-semibold">{t("agentRollout.remote_manual_title")}</summary><p className="mt-2 text-sm text-muted-foreground">{t("agentRollout.remote_manual_hint")}</p><div className="mt-4"><AgentReleaseImport saved={releases.refresh} /></div></details>}
      </>}</TabsContent>
      <TabsContent value="rollouts" className="mt-4 space-y-6">{rollouts.data && !unavailable && <><div className="flex justify-end"><Button onClick={() => setCreating(true)}>{t("agentRollout.create_rollout")}</Button></div><div className="grid gap-3">{rollouts.data.items.map(rollout => <article key={rollout.id} className="flex flex-wrap items-center justify-between gap-3 rounded-lg border p-4"><div><h2 className="font-semibold">{rollout.name}</h2><p className="text-sm">{t("agentRollout.rollout_meta", { action: rollout.action, state: rollout.state, current: rollout.current_wave < 0 ? t("agentRollout.not_started") : rollout.current_wave + 1, total: rollout.total_waves })}</p><p className="text-sm text-muted-foreground">{rollout.reason}</p></div><Button variant="outline" onClick={() => setRolloutID(rollout.id)}>{t("agentRollout.view_name", { name: rollout.name })}</Button></article>)}</div>{!rollouts.data.items.length && <p>{t("agentRollout.no_rollouts")}</p>}<AgentPageControls previous={rollouts.previous} next={rollouts.next} />{rolloutID && <AgentRolloutDetailPanel key={rolloutID} id={rolloutID} removed={() => setRolloutID("")} />}</>}</TabsContent>
      <TabsContent value="platforms" className="mt-4 space-y-4"><h2 className="font-semibold">{t("agentRollout.platform_evidence")}</h2><p className="text-sm text-muted-foreground">{t("agentRollout.platform_hint")}</p>{releases.data && !unavailable && <>{releases.data.items.flatMap(release => release.manifest.artifacts.map(artifact => <div key={`${release.id}/${artifact.id}`} className="flex flex-wrap justify-between gap-2 rounded-lg border p-4"><span>{t("agentRollout.artifact_meta", { version: release.manifest.version, channel: release.manifest.channel, os: artifact.os, arch: artifact.arch, kind: artifact.kind })}</span><div className="flex gap-2"><Badge variant="outline">{artifact.support}</Badge><Badge variant="outline">{release.revoked ? t("agentRollout.revoked") : release.verified ? t("agentRollout.verified_at_import") : t("agentRollout.unverified")}</Badge></div></div>))}{!releases.data.items.length && <p>{t("agentRollout.no_platform_evidence")}</p>}<AgentPageControls previous={releases.previous} next={releases.next} /></>}</TabsContent>
    </Tabs>
    {creating && releases.data && !unavailable && <AgentRolloutCreate releases={releases.data.items} saved={rollouts.refresh} close={() => setCreating(false)} />}
    {loaded && !unavailable && <AgentRetentionPanel />}
  </main>
}
