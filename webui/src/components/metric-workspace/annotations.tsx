import { useEffect, useState } from "react"
import { PlusIcon, Trash2Icon } from "lucide-react"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Textarea } from "@/components/ui/textarea"
import { createAnnotation, deleteAnnotation, listAnnotations, type DashboardAnnotation } from "@/lib/metric-collaboration-api"
import { WorkspaceSelect } from "./select"
import { useCollaborationText } from "./collaboration-text"
import { workspaceAnnotationScopeLabel } from "@/lib/workspace-annotations"
import type { PanelColor } from "@/types/metric-workspace"

const colors: PanelColor[] = ["blue", "green", "amber", "rose", "violet", "cyan", "slate"]
const chipColor: Record<PanelColor, string> = {
  blue: "border-sky-500/40 bg-sky-500/10", green: "border-emerald-500/40 bg-emerald-500/10",
  amber: "border-amber-500/40 bg-amber-500/10", rose: "border-rose-500/40 bg-rose-500/10",
  violet: "border-violet-500/40 bg-violet-500/10", cyan: "border-cyan-500/40 bg-cyan-500/10",
  slate: "border-slate-500/40 bg-slate-500/10",
}

/** 看板注释在时间轴上按窗口展示，可编辑看板还能就地创建与删除。 */
export function WorkspaceAnnotations(props: {
  dashboardId: string; from: string; to: string; editable: boolean; reload: number
  onError: (value: string | null) => void; onChanged: () => void
  /** 把当前窗口内的注释上报给看板，供面板时间轴叠加标记。 */
  onLoaded?: (values: DashboardAnnotation[]) => void
  /** 可创建面板级注释的可选面板。 */
  panels?: { id: string; title: string }[]
}) {
  const text = useCollaborationText()
  const panelTitles = Object.fromEntries((props.panels ?? []).map((item) => [item.id, item.title]))
  const [values, setValues] = useState<DashboardAnnotation[]>([])
  const [draft, setDraft] = useState(false)
  useEffect(() => {
    const controller = new AbortController()
    listAnnotations(props.dashboardId, props.from, props.to, controller.signal)
      .then((values) => { setValues(values); props.onLoaded?.(values) })
      .catch(() => { if (!controller.signal.aborted) props.onError(text("load_failed")) })
    return () => controller.abort()
    // oxlint-disable-next-line react-hooks/exhaustive-deps -- 仅在窗口/看板变化时重新拉取，文案与回调无需进入依赖
  }, [props.dashboardId, props.from, props.to, props.reload])
  async function remove(id: string) {
    try {
      await deleteAnnotation(id)
      const next = values.filter((value) => value.id !== id)
      setValues(next)
      props.onLoaded?.(next)
    } catch { props.onError(text("save_failed")) }
  }
  return <section aria-label={text("annotations")} className="space-y-2">
    <div className="flex items-center gap-2"><h2 className="text-xs font-medium uppercase text-muted-foreground">{text("annotations")}</h2>
      {props.editable && <Button variant="outline" size="sm" onClick={() => setDraft(true)}><PlusIcon />{text("add_annotation")}</Button>}</div>
    {values.length === 0 ? <p className="text-xs text-muted-foreground">{text("no_annotations")}</p> :
      <ul className="flex flex-wrap gap-2">{values.map((value) => {
        const scope = workspaceAnnotationScopeLabel(value, panelTitles)
        return <li key={value.id} className={`flex items-center gap-2 rounded-md border px-2 py-1 text-xs ${chipColor[value.color]}`}>
          <span className="font-medium">{value.title}</span>
          <time className="font-mono text-[10px] text-muted-foreground" dateTime={value.from}>{value.from.replace("T", " ").slice(0, 16)}</time>
          {scope && <Badge variant="secondary">{scope}</Badge>}
          {value.tags.map((tag) => <Badge key={tag} variant="outline">{tag}</Badge>)}
          {props.editable && <Button variant="ghost" size="icon-sm" title={text("delete_annotation")}
            aria-label={`${text("delete_annotation")} · ${value.title}`} onClick={() => void remove(value.id)}><Trash2Icon /></Button>}
        </li>
      })}</ul>}
    {draft && <AnnotationDialog dashboardId={props.dashboardId} from={props.from} to={props.to} panels={props.panels ?? []}
      close={() => setDraft(false)} saved={(value) => { setValues((previous) => [...previous, value]); props.onChanged() }} onError={props.onError} />}
  </section>
}

function AnnotationDialog(props: {
  dashboardId: string; from: string; to: string; close: () => void
  saved: (value: DashboardAnnotation) => void; onError: (value: string | null) => void
  panels: { id: string; title: string }[]
}) {
  const text = useCollaborationText()
  const [title, setTitle] = useState("")
  const [note, setNote] = useState("")
  const [tags, setTags] = useState("")
  const [color, setColor] = useState<PanelColor>("blue")
  const [visibility, setVisibility] = useState<"private" | "public">("private")
  const [scope, setScope] = useState("")
  const [busy, setBusy] = useState(false)
  async function submit() {
    setBusy(true)
    try {
      const value = await createAnnotation({ dashboard_id: props.dashboardId, title, text: note, color, visibility,
        panel_id: scope || undefined,
        tags: tags.split(",").map((tag) => tag.trim().toLowerCase()).filter(Boolean), from: props.from, to: props.to })
      props.saved(value); props.close()
    } catch { props.onError(text("save_failed")) } finally { setBusy(false) }
  }
  return <Dialog open onOpenChange={(open) => { if (!open && !busy) props.close() }}><DialogContent><DialogHeader>
    <DialogTitle>{text("add_annotation")}</DialogTitle><DialogDescription>{props.from} → {props.to}</DialogDescription></DialogHeader>
    <div className="grid gap-2"><Label htmlFor="annotation-title">{text("annotation_title")}</Label>
      <Input id="annotation-title" value={title} maxLength={200} onChange={(event) => setTitle(event.target.value)} /></div>
    <div className="grid gap-2"><Label htmlFor="annotation-text">{text("annotation_text")}</Label>
      <Textarea id="annotation-text" value={note} maxLength={2000} onChange={(event) => setNote(event.target.value)} /></div>
    {props.panels.length > 0 && <div className="grid gap-2"><Label>{text("annotation_scope")}</Label>
      <WorkspaceSelect label={text("annotation_scope")} value={scope} onChange={setScope} options={[
        { value: "", label: text("annotation_scope_dashboard") },
        ...props.panels.map((panel) => ({ value: panel.id, label: panel.title }))]} /></div>}
    <div className="grid gap-2 sm:grid-cols-3"><div className="grid gap-2"><Label htmlFor="annotation-tags">{text("annotation_tags")}</Label>
        <Input id="annotation-tags" value={tags} placeholder="deploy, update" onChange={(event) => setTags(event.target.value)} /></div>
      <div className="grid gap-2"><Label>{text("annotation_color")}</Label>
        <WorkspaceSelect label={text("annotation_color")} value={color} options={colors.map((value) => ({ value, label: value }))} onChange={(value) => setColor(value as PanelColor)} /></div>
      <div className="grid gap-2"><Label>{text("annotation_visibility")}</Label>
        <WorkspaceSelect label={text("annotation_visibility")} value={visibility} options={[
          { value: "private", label: text("annotation_private") }, { value: "public", label: text("annotation_public") }]}
          onChange={(value) => setVisibility(value as "private" | "public")} /></div></div>
    <DialogFooter><Button variant="outline" disabled={busy} onClick={props.close}>{text("cancel")}</Button>
      <Button disabled={busy || !title.trim()} onClick={() => void submit()}>{text("save")}</Button></DialogFooter>
  </DialogContent></Dialog>
}
