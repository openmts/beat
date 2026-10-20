import { useEffect, useState } from "react"
import { PlusIcon, SearchIcon, Trash2Icon } from "lucide-react"
import { createLibraryPanel, deleteLibraryPanel, listLibraryPanels, type DashboardLibraryPanel } from "@/lib/metric-collaboration-api"
import { Button } from "@/components/ui/button"
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { useCollaborationText } from "./collaboration-text"
import { randomUUID } from "@/lib/random-id"
import type { DashboardPanel } from "@/types/metric-workspace"

/** 面板库保存可复用的单面板定义；插入时复制定义，后续库内修改不影响已保存看板。 */
export function WorkspacePanelLibrary(props: {
  panel?: DashboardPanel; close: () => void; insert?: (panel: DashboardPanel) => void
  onError: (value: string | null) => void
}) {
  const text = useCollaborationText()
  const [values, setValues] = useState<DashboardLibraryPanel[]>([])
  const [name, setName] = useState(props.panel?.title ?? "")
  const [busy, setBusy] = useState(false)
  const [query, setQuery] = useState("")
  const visible = values.filter((value) => libraryMatches(value, query))
  useEffect(() => {
    listLibraryPanels().then(setValues).catch(() => props.onError(text("load_failed")))
    // oxlint-disable-next-line react-hooks/exhaustive-deps -- 面板库只拉取一次，文案与回调通过闭包捕获
  }, [])
  async function save() {
    if (!props.panel) return
    setBusy(true)
    try {
      const value = await createLibraryPanel({ name, panel: props.panel })
      setValues((previous) => [value, ...previous])
    } catch { props.onError(text("save_failed")) } finally { setBusy(false) }
  }
  async function remove(id: string) {
    try { await deleteLibraryPanel(id); setValues((previous) => previous.filter((value) => value.id !== id)) }
    catch { props.onError(text("save_failed")) }
  }
  return <Dialog open onOpenChange={(open) => { if (!open) props.close() }}><DialogContent className="sm:max-w-2xl"><DialogHeader>
    <DialogTitle>{text("panel_library")}</DialogTitle><DialogDescription>{props.panel ? props.panel.title : text("insert_from_library")}</DialogDescription></DialogHeader>
    {props.panel && <div className="grid gap-2 sm:grid-cols-[2fr_auto] sm:items-end">
      <div className="grid gap-2"><Label htmlFor="library-name">{text("panel_library_name")}</Label>
        <Input id="library-name" value={name} maxLength={200} onChange={(event) => setName(event.target.value)} /></div>
      <Button disabled={busy || !name.trim()} onClick={() => void save()}><PlusIcon />{text("save_to_library")}</Button></div>}
    {values.length === 0 ? <p className="text-sm text-muted-foreground">{text("no_library_panels")}</p> : <div className="space-y-2">
      <div className="relative"><SearchIcon className="absolute top-1/2 left-2 size-4 -translate-y-1/2 text-muted-foreground" aria-hidden />
        <Input className="pl-8" value={query} aria-label={text("search")} placeholder={text("search")} onChange={(event) => setQuery(event.target.value)} /></div>
      {visible.length === 0 ? <p className="text-sm text-muted-foreground">{text("no_library_match")}</p> :
      <ul className="space-y-2">{visible.map((value) => <li key={value.id} className="flex items-center justify-between gap-3 rounded-md border px-3 py-2 text-sm">
        <span className="min-w-0"><span className="font-medium">{value.name}</span>
          <span className="block truncate text-xs text-muted-foreground">{value.panel.title} · {value.panel.type}</span></span>
        <span className="flex items-center gap-1">{props.insert && <Button variant="outline" size="sm"
          onClick={() => { props.insert!({ ...structuredClone(value.panel), id: randomUUID() }); props.close() }}>{text("insert")}</Button>}
          <Button variant="ghost" size="icon-sm" title={text("delete")} aria-label={`${text("delete")} · ${value.name}`}
            onClick={() => void remove(value.id)}><Trash2Icon /></Button></span>
      </li>)}</ul>}</div>}
    <DialogFooter><Button variant="outline" onClick={props.close}>{text("close")}</Button></DialogFooter>
  </DialogContent></Dialog>
}

/** 检索只按名称与面板标题做不区分大小写的包含匹配，空查询保留全部条目。 */
function libraryMatches(value: DashboardLibraryPanel, query: string): boolean {
  const needle = query.trim().toLowerCase()
  if (!needle) return true
  return `${value.name} ${value.panel.title} ${value.panel.type}`.toLowerCase().includes(needle)
}
