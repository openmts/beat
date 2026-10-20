import { useEffect, useState } from "react"
import { Link, useLocation } from "react-router"
import { SearchXIcon, ArrowLeftIcon } from "lucide-react"
import { useLocale } from "@/context/locale"
import { buttonVariants } from "@/components/ui/button"

const snapshotPrefix = "/metrics/snapshots/"

// 公开兜底页同时承载只读看板快照：两者都是无登录访问的公开 URL。
// 快照视图刻意留在本模块内，避免新增动态入口而扰动 beat-public-shell 的 250 KiB 分片图。
function PublicFallback() {
  const { pathname } = useLocation()
  if (pathname.startsWith(snapshotPrefix)) return <SnapshotView token={pathname.slice(snapshotPrefix.length)} />
  return <NotFound />
}

function NotFound() {
  const { t } = useLocale()
  return (
    <div className="flex min-h-svh flex-col items-center justify-center gap-4 px-4 text-center">
      <SearchXIcon className="size-12 text-muted-foreground" aria-hidden />
      <h1 className="text-4xl font-bold tracking-tight">404</h1>
      <p className="max-w-sm text-muted-foreground">{t("app.not_found")}</p>
      <Link to="/" className={buttonVariants({ variant: "default", size: "default" })}>
        <ArrowLeftIcon data-icon="inline-start" />
        {t("app.dashboard")}
      </Link>
    </div>
  )
}

interface SnapshotSeries { ref_id: string; node_id: string; metric: string; aggregation: string; label?: string }
interface SnapshotPanel { id: string; title: string; type: string; size: string; series: SnapshotSeries[] }
interface SnapshotPayload { name: string; definition: { title: string; description?: string; panels: SnapshotPanel[] } }

/** 只读看板快照：冻结某次看板定义并分享给任何人，不跟随源看板变化。 */
function SnapshotView(props: { token: string }) {
  const { t } = useLocale()
  const [value, setValue] = useState<SnapshotPayload | null>(null)
  const [missing, setMissing] = useState(false)
  useEffect(() => {
    const controller = new AbortController()
    fetch(`/api/v1/metrics/snapshots/${encodeURIComponent(props.token)}`, { signal: controller.signal, credentials: "same-origin" })
      .then((response) => { if (!response.ok) throw new Error("missing"); return response.json() as Promise<{ data: SnapshotPayload }> })
      .then((payload) => setValue(payload.data))
      .catch(() => { if (!controller.signal.aborted) setMissing(true) })
    return () => controller.abort()
  }, [props.token])
  if (missing) return <main className="container-page py-10"><p role="alert" className="text-sm text-muted-foreground">{t("snapshot.missing")}</p></main>
  if (!value) return <main className="container-page py-10"><p className="text-sm text-muted-foreground">…</p></main>
  return <main className="container-page space-y-6 py-10">
    <header className="space-y-2"><div className="flex flex-wrap items-center gap-3">
      <h1 className="font-heading text-2xl font-semibold tracking-tight">{value.name}</h1>
      <span className="rounded-md border px-2 py-0.5 text-xs text-muted-foreground">{t("snapshot.badge")}</span></div>
      <p className="text-sm text-muted-foreground">{value.definition.title}{value.definition.description ? ` · ${value.definition.description}` : ""}</p>
      <p className="text-xs text-muted-foreground">{t("snapshot.frozen")}</p></header>
    <div className="grid gap-4 lg:grid-cols-2">{value.definition.panels.map((panel) => <section key={panel.id} className="rounded-lg border p-4">
      <h2 className="text-base font-medium">{panel.title}</h2>
      <p className="text-xs text-muted-foreground">{panel.type} · {panel.size}</p>
      <ul className="mt-3 space-y-2">{panel.series.map((series) => <li key={series.ref_id} className="flex items-center justify-between gap-3 rounded-md border px-3 py-2 text-sm">
        <span className="min-w-0 truncate">{series.label || `${series.metric} · ${series.aggregation}`}</span>
        <span className="shrink-0 font-mono text-xs text-muted-foreground">{series.ref_id} · {series.node_id}</span></li>)}</ul>
    </section>)}</div>
  </main>
}

export default PublicFallback
