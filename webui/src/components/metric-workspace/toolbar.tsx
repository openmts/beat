import { useRef, useState } from "react"
import { MoreHorizontalIcon, PlusIcon, SaveIcon, Settings2Icon } from "lucide-react"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Badge } from "@/components/ui/badge"
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuSeparator, DropdownMenuTrigger } from "@/components/ui/dropdown-menu"
import { useLocale } from "@/context/locale"
import { WorkspaceSelect } from "./select"
import { dashboardMatchesQuery } from "@/lib/workspace-dashboard-tags"
import { useCollaborationText } from "./collaboration-text"
import type { WorkspaceEditor } from "@/hooks/use-workspace-editor"

export type WorkspaceToolbarAction = "new" | "save" | "copy" | "discard" | "reload" | "delete" | "rollback" | "details" | "publish" | "publish_node" | "export" | "share" | "snapshot" | "versions" | "panel_library" | "duplicate" | "paste_panel"

export function WorkspaceToolbar(props: { editor: WorkspaceEditor; owner: boolean; select: (id: string) => void; action: (action: WorkspaceToolbarAction) => void; importFile: (file: File) => void }) {
  const { t } = useLocale()
  const editor = props.editor
  const file = useRef<HTMLInputElement>(null)
  const [query, setQuery] = useState("")
  const options = editor.dashboards.filter((value) => value.id === editor.record.id || dashboardMatchesQuery(value.definition, query))
    .map((value) => ({ value: value.id, label: `${value.definition.title} · ${t(`workspace.scope_${value.scope}`)}` }))
  if (editor.record.id === "new") options.push({ value: "new", label: editor.record.definition.title })
  const copy = !props.owner && editor.record.scope !== "personal"
  return <div className="flex flex-wrap items-center justify-between gap-3">
    <input ref={file} type="file" accept="application/json,.json" className="hidden" aria-hidden data-workspace-import
      onChange={(event) => { const input = event.currentTarget, selected = input.files?.[0]; input.value = ""; if (selected) props.importFile(selected) }} />
    <div className="flex min-w-0 flex-wrap items-center gap-3"><WorkspaceSelect label={t("workspace.dashboards")} value={editor.record.id} options={options} onChange={props.select} disabled={editor.busy} className="max-w-full sm:min-w-64" />
      <Input aria-label={t("workspace.dashboard_search")} placeholder={t("workspace.dashboard_search")} value={query} onChange={(event) => setQuery(event.target.value)} className="max-w-full sm:max-w-48" />
      <Badge variant={editor.dirty ? "outline" : "secondary"}>{t(editor.dirty ? "workspace.dirty" : editor.record.revision === 0 ? "workspace.builtin" : "workspace.saved")}</Badge></div>
    <div className="flex flex-wrap items-center gap-2"><Button variant="outline" size="sm" disabled={editor.busy} onClick={() => props.action("new")}><PlusIcon />{t("workspace.new")}</Button>
      <Button variant="outline" size="icon-sm" disabled={editor.busy} aria-label={t("workspace.edit_details")} onClick={() => props.action("details")}><Settings2Icon /></Button>
      <Button size="sm" disabled={editor.busy || !editor.dirty && !copy} onClick={() => props.action(copy ? "copy" : "save")}><SaveIcon />{t(editor.busy ? "workspace.saving" : copy ? "workspace.save_copy" : "workspace.save")}</Button>
      <WorkspaceMenu editor={editor} owner={props.owner} action={props.action} requestImport={() => file.current?.click()} />
    </div>
  </div>
}

function WorkspaceMenu(props: { editor: WorkspaceEditor; owner: boolean; action: (action: WorkspaceToolbarAction) => void; requestImport: () => void }) {
  const { t } = useLocale()
  const text = useCollaborationText()
  return <DropdownMenu><DropdownMenuTrigger render={<Button variant="outline" size="icon-sm" disabled={props.editor.busy} aria-label={t("workspace.dashboards")} />}><MoreHorizontalIcon /></DropdownMenuTrigger><DropdownMenuContent align="end">
    <DropdownMenuItem onClick={() => props.action("copy")}>{t("workspace.save_copy")}</DropdownMenuItem>
    <DropdownMenuItem disabled={props.editor.record.id === "new"} onClick={() => props.action("duplicate")}>{t("workspace.duplicate")}</DropdownMenuItem>
    <DropdownMenuItem disabled={!props.editor.dirty} onClick={() => props.action("discard")}>{t("workspace.discard")}</DropdownMenuItem>
    <DropdownMenuItem onClick={() => props.action("reload")}>{t("workspace.reload")}</DropdownMenuItem>
    <DropdownMenuSeparator />
    <DropdownMenuItem onClick={() => props.action("export")}>{t("workspace.export_dashboard")}</DropdownMenuItem>
    <DropdownMenuItem onClick={props.requestImport}>{t("workspace.import_dashboard")}</DropdownMenuItem>
    <DropdownMenuItem onClick={() => props.action("paste_panel")}>{t("workspace.paste_panel")}</DropdownMenuItem>
    <DropdownMenuItem onClick={() => props.action("share")}>{t("workspace.share")}</DropdownMenuItem>
    <DropdownMenuItem onClick={() => props.action("snapshot")}>{text("snapshots")}</DropdownMenuItem>
    <DropdownMenuItem onClick={() => props.action("panel_library")}>{text("panel_library")}</DropdownMenuItem>
    <DropdownMenuSeparator />
    <DropdownMenuItem disabled={props.editor.record.id === "new" || props.editor.dirty} onClick={() => props.action("versions")}>{t("workspace.version_history")}</DropdownMenuItem>
    <DropdownMenuItem disabled={!props.editor.saved.has_previous || props.editor.dirty} onClick={() => props.action("rollback")}>{t("workspace.rollback")}</DropdownMenuItem>
    {props.owner && <><DropdownMenuSeparator /><DropdownMenuItem onClick={() => props.action("publish")}>{t("workspace.publish_public")}</DropdownMenuItem><DropdownMenuItem onClick={() => props.action("publish_node")}>{t("workspace.publish_node")}</DropdownMenuItem></>}
    <DropdownMenuSeparator /><DropdownMenuItem variant="destructive" disabled={props.editor.record.scope !== "personal" || props.editor.record.id === "new"} onClick={() => props.action("delete")}>{t("workspace.delete")}</DropdownMenuItem>
  </DropdownMenuContent></DropdownMenu>
}
