import { useEffect, useState } from "react"
import { createSnapshot, listSnapshots, revokeSnapshot, type DashboardSnapshot } from "@/lib/metric-collaboration-api"
import { Button } from "@/components/ui/button"
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { useCollaborationText } from "./collaboration-text"

const expiries = [
  { value: "0", key: "snapshot_expiry_none" },
  { value: "86400", key: "snapshot_expiry_day" },
  { value: "604800", key: "snapshot_expiry_week" },
] as const

/** 快照把当前看板定义冻结成只读分享链接，撤销后链接立即失效。 */
export function WorkspaceSnapshots(props: { dashboardId: string; dashboardName: string; close: () => void; onError: (value: string | null) => void }) {
  const text = useCollaborationText()
  const [values, setValues] = useState<DashboardSnapshot[]>([])
  const [name, setName] = useState(props.dashboardName)
  const [expiry, setExpiry] = useState(expiries[0].value)
  const [link, setLink] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)
  useEffect(() => {
    listSnapshots().then((all) => setValues(all.filter((value) => value.dashboard_id === props.dashboardId)))
      .catch(() => props.onError(text("load_failed")))
    // oxlint-disable-next-line react-hooks/exhaustive-deps -- 快照按看板拉取一次，文案与回调无需进入依赖
  }, [props.dashboardId])
  async function submit() {
    setBusy(true)
    try {
      const value = await createSnapshot({ dashboardId: props.dashboardId, name, expiresInSeconds: Number(expiry) })
      setValues((previous) => [value, ...previous])
      setLink(`${window.location.origin}/metrics/snapshots/${value.token}`)
    } catch { props.onError(text("save_failed")) } finally { setBusy(false) }
  }
  async function revoke(id: string) {
    try { await revokeSnapshot(id); setValues((previous) => previous.filter((value) => value.id !== id)) }
    catch { props.onError(text("save_failed")) }
  }
  return <Dialog open onOpenChange={(open) => { if (!open) props.close() }}><DialogContent className="sm:max-w-2xl"><DialogHeader>
    <DialogTitle>{text("snapshots")}</DialogTitle><DialogDescription>{text("snapshot_description")}</DialogDescription></DialogHeader>
    <div className="grid gap-4 sm:grid-cols-[2fr_1fr_auto] sm:items-end">
      <div className="grid gap-2"><Label htmlFor="snapshot-name">{text("snapshot_name")}</Label>
        <Input id="snapshot-name" value={name} maxLength={200} onChange={(event) => setName(event.target.value)} /></div>
      <div className="grid gap-2"><Label htmlFor="snapshot-expiry">{text("snapshot_expiry")}</Label>
        <select id="snapshot-expiry" className="h-9 rounded-md border bg-transparent px-3 text-sm" value={expiry} onChange={(event) => setExpiry(event.target.value as typeof expiry)}>
          {expiries.map((option) => <option key={option.value} value={option.value}>{text(option.key)}</option>)}</select></div>
      <Button disabled={busy || !name.trim()} onClick={() => void submit()}>{text("create_snapshot")}</Button></div>
    {link && <div className="grid gap-2"><Label htmlFor="snapshot-link">{text("snapshot_created")}</Label>
      <Input id="snapshot-link" readOnly value={link} onFocus={(event) => event.currentTarget.select()} />
      <Button variant="outline" size="sm" className="w-fit" onClick={() => { void navigator.clipboard.writeText(link).catch(() => props.onError(text("save_failed"))) }}>{text("copy_link")}</Button></div>}
    {values.length === 0 ? <p className="text-sm text-muted-foreground">{text("no_snapshots")}</p> :
      <ul className="space-y-2">{values.map((value) => <li key={value.id} className="flex items-center justify-between gap-3 rounded-md border px-3 py-2 text-sm">
        <span className="min-w-0"><span className="font-medium">{value.name}</span>
          <time className="block text-xs text-muted-foreground" dateTime={value.created_at}>{value.created_at.slice(0, 16).replace("T", " ")}</time></span>
        {value.revoked_at ? <span className="text-xs text-muted-foreground">{text("revoke_snapshot")} ✓</span> :
          <Button variant="outline" size="sm" onClick={() => void revoke(value.id)}>{text("revoke_snapshot")}</Button>}
      </li>)}</ul>}
    <DialogFooter><Button variant="outline" onClick={props.close}>{text("close")}</Button></DialogFooter>
  </DialogContent></Dialog>
}
