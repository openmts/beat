import { useEffect, useState } from "react"
import { CalendarDaysIcon, CloudDownloadIcon, ExternalLinkIcon, PackageCheckIcon } from "lucide-react"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardFooter, CardHeader, CardTitle } from "@/components/ui/card"
import { Skeleton } from "@/components/ui/skeleton"
import { useLocale } from "@/context/locale"
import { agentRolloutError } from "@/hooks/use-agent-rollout-data"
import { useConfirmedOperation } from "@/hooks/use-confirmed-operation"
import { listAgentRemoteReleases, syncAgentRemoteRelease } from "@/lib/agent-rollout-api"
import type { AgentRelease, AgentRemotePage } from "@/types/agent-rollouts"
import { OperationConfirm } from "./operation-confirm"

interface RemoteReleasePanelProps {
  releases: AgentRelease[]
  saved: () => Promise<unknown>
  onView: (id: string) => void
}

export function AgentReleaseRemote({ releases, saved, onView }: RemoteReleasePanelProps) {
  const { locale, t } = useLocale(), flow = useConfirmedOperation()
  const [page, setPage] = useState(1), [source, setSource] = useState<AgentRemotePage | null>(null)
  const [loading, setLoading] = useState(true), [error, setError] = useState(""), [busy, setBusy] = useState("")
  const [notice, setNotice] = useState(""), [revision, setRevision] = useState(0)
  useEffect(() => {
    const controller = new AbortController()
    setLoading(true); setError("")
    listAgentRemoteReleases(page, controller.signal).then(setSource).catch(error => {
      if (!controller.signal.aborted) setError(agentRolloutError(error, t))
    }).finally(() => {
      if (!controller.signal.aborted) setLoading(false)
    })
    return () => controller.abort()
  }, [page, revision, t])
  const sync = async (releaseTag: string, version: string) => {
    setBusy(releaseTag); setNotice("")
    try {
      await syncAgentRemoteRelease(releaseTag)
      await saved()
      setNotice(t("agentRollout.remote_sync_success", { version }))
    } finally { setBusy("") }
  }
  const sourceURL = source ? `https://github.com/${source.repository}` : ""
  return <section aria-labelledby="remote-release-source" className="space-y-4 rounded-xl border bg-muted/20 p-4">
    <div className="flex flex-wrap items-start justify-between gap-3"><div className="space-y-1"><h2 id="remote-release-source" className="flex items-center gap-2 font-semibold"><CloudDownloadIcon aria-hidden />{t("agentRollout.remote_title")}</h2><p className="max-w-3xl text-sm text-muted-foreground">{t("agentRollout.remote_description")}</p>{source && <a className="inline-flex items-center gap-1 text-sm text-primary underline-offset-4 hover:underline" href={sourceURL} target="_blank" rel="noreferrer">{source.repository}<ExternalLinkIcon className="size-3" aria-hidden /></a>}</div><Button variant="outline" disabled={loading} onClick={() => setRevision(value => value + 1)}>{t("agentRollout.remote_refresh")}</Button></div>
    {loading && !source && <Skeleton className="h-40" />}{error && <p role="alert" className="rounded-lg border border-destructive/40 bg-destructive/5 p-3 text-sm text-destructive">{error}</p>}{notice && <p role="status" className="rounded-lg border border-primary/30 bg-primary/5 p-3 text-sm">{notice}</p>}
    {source && <><div className="grid gap-3 lg:grid-cols-2">{source.items.map(item => {
      const local = releases.find(release => release.manifest.version === item.version)
      return <Card key={item.tag_name} size="sm" className={local ? "border-primary/30" : undefined}><CardHeader><div className="flex flex-wrap items-start justify-between gap-2"><div><CardTitle>{item.version}</CardTitle><CardDescription>{item.name || item.tag_name}</CardDescription></div><div className="flex flex-wrap gap-1.5"><Badge variant={item.channel === "preview" ? "secondary" : "outline"}>{item.channel}</Badge>{local && <Badge><PackageCheckIcon aria-hidden />{t("agentRollout.remote_synced")}</Badge>}</div></div></CardHeader><CardContent className="space-y-2 text-sm"><p className="flex items-center gap-2 text-muted-foreground"><CalendarDaysIcon aria-hidden />{new Intl.DateTimeFormat(locale, { dateStyle: "medium", timeStyle: "short" }).format(new Date(item.published_at))}</p><p className="text-muted-foreground">{item.ready ? t("agentRollout.remote_ready_hint") : t("agentRollout.remote_unavailable_hint")}</p></CardContent><CardFooter className="justify-end gap-2">{local ? <Button variant="outline" onClick={() => onView(local.id)}>{t("agentRollout.remote_view_local")}</Button> : <Button disabled={!item.ready || Boolean(busy) || flow.busy} onClick={() => flow.request({ title: t("agentRollout.remote_sync_title", { version: item.version }), description: t("agentRollout.remote_sync_description", { version: item.version }), execute: () => sync(item.tag_name, item.version) })}>{busy === item.tag_name ? t("agentRollout.remote_syncing") : t("agentRollout.remote_sync")}</Button>}</CardFooter></Card>
    })}</div>{!source.items.length && <p role="status">{t("agentRollout.remote_empty")}</p>}<div className="flex justify-between gap-2"><Button variant="outline" disabled={page <= 1 || loading} onClick={() => setPage(current => Math.max(1, current - 1))}>{t("agentRollout.previous_page")}</Button><Button variant="outline" disabled={!source.next_page || loading} onClick={() => setPage(current => source.next_page ?? current)}>{t("agentRollout.next_page")}</Button></div></>}
    <OperationConfirm flow={flow} />
  </section>
}
