import { useEffect, useState } from "react"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { useLocale } from "@/context/locale"
import { useConfirmedOperation } from "@/hooks/use-confirmed-operation"
import { getRemoteMMDBRelease, installRemoteMMDB } from "@/lib/lifecycle-api"
import { formatDateTime } from "@/lib/intl-format"
import { formatBytes } from "@/lib/metric-format"
import type { GeoIPSettings, RemoteMMDBRelease } from "@/types/node-lifecycle"
import { DownloadIcon, RefreshCwIcon } from "lucide-react"
import { OperationConfirm } from "./operation-confirm"

const repositoryURL = "https://github.com/P3TERX/GeoLite.mmdb"

export function RemoteMMDBPanel({ value, saved }: { value: GeoIPSettings; saved: () => void }) {
  const { t, locale } = useLocale()
  const flow = useConfirmedOperation()
  const [release, setRelease] = useState<RemoteMMDBRelease | null>(null)
  const [errorMessage, setErrorMessage] = useState("")
  const [loadFailed, setLoadFailed] = useState(false)
  const [loading, setLoading] = useState(true)
  const [generation, setGeneration] = useState(0)

  useEffect(() => {
    const controller = new AbortController()
    setErrorMessage("")
    setLoadFailed(false)
    setLoading(true)
    getRemoteMMDBRelease(controller.signal).then(value => {
      if (!controller.signal.aborted) setRelease(value)
    }).catch((failure: unknown) => {
      if (!controller.signal.aborted) {
        setLoadFailed(true)
        setErrorMessage(failure instanceof Error ? failure.message : "")
      }
    }).finally(() => { if (!controller.signal.aborted) setLoading(false) })
    return () => controller.abort()
  }, [generation])

  const install = () => {
    if (!release || !release.update_available) return
    flow.request({
      title: t("lifecycle.remote_mmdb_install"),
      description: t("lifecycle.remote_mmdb_install_description", { version: release.version }),
      execute: async () => {
        await installRemoteMMDB({ version: release.version, sha256: release.sha256, expected_revision: value.revision })
        setRelease(current => current && current.version === release.version && current.sha256 === release.sha256
          ? { ...current, update_available: false }
          : current)
        saved()
      },
    })
  }

  const action = value.mmdb.state === "ready"
    ? t("lifecycle.remote_mmdb_update")
    : t("lifecycle.remote_mmdb_download")
  const error = loadFailed ? errorMessage || t("lifecycle.remote_mmdb_failed") : ""

  return <section className="space-y-3 rounded-lg border bg-muted/20 p-4" aria-labelledby="remote-mmdb-title">
    <div className="flex flex-wrap items-start justify-between gap-2">
      <div><h4 id="remote-mmdb-title" className="font-medium">{t("lifecycle.remote_mmdb_title")}</h4>
        <p className="mt-1 text-xs text-muted-foreground">{t("lifecycle.remote_mmdb_description", { repository: release?.repository || "P3TERX/GeoLite.mmdb" })}</p></div>
      <Button size="sm" variant="ghost" onClick={() => setGeneration(current => current + 1)} disabled={loading} aria-label={t("lifecycle.remote_mmdb_check_again")}>
        <RefreshCwIcon data-icon="inline-start" className={loading ? "animate-spin" : undefined} />
        {t("lifecycle.remote_mmdb_check_again")}
      </Button>
    </div>
    {error && <div className="flex flex-wrap items-center justify-between gap-2 rounded-md border border-destructive/30 p-3 text-sm text-destructive"><span role="alert">{error}</span><Button size="sm" variant="outline" onClick={() => setGeneration(current => current + 1)}>{t("lifecycle.retry")}</Button></div>}
    {!error && loading && !release && <p role="status" className="text-sm text-muted-foreground">{t("lifecycle.remote_mmdb_loading")}</p>}
    {release && <div className="space-y-3">
      <div className="flex flex-wrap items-center gap-2 text-sm">
        <a className="font-medium underline underline-offset-4" href={repositoryURL} target="_blank" rel="noreferrer">{release.repository}</a>
        <span aria-hidden>·</span><span>{t("lifecycle.remote_mmdb_version", { version: release.version })}</span>
        <Badge variant={release.update_available ? "secondary" : "outline"}>{release.update_available ? t("lifecycle.remote_mmdb_update_available") : t("lifecycle.remote_mmdb_up_to_date")}</Badge>
      </div>
      <p className="text-xs text-muted-foreground">{t("lifecycle.remote_mmdb_published", { date: formatDateTime(locale, release.published_at), size: formatBytes(release.size_bytes, locale) })}</p>
      <div className="flex flex-wrap items-center gap-3"><Button size="sm" onClick={install} disabled={!release.update_available || flow.busy}><DownloadIcon data-icon="inline-start" />{action}</Button><code className="break-all text-[11px] text-muted-foreground">SHA-256 {release.sha256}</code></div>
    </div>}
    <OperationConfirm flow={flow} />
  </section>
}
