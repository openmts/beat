import { useState } from "react"
import { XIcon } from "lucide-react"
import { Button } from "@/components/ui/button"
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { Checkbox } from "@/components/ui/checkbox"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Textarea } from "@/components/ui/textarea"
import { useLocale } from "@/context/locale"
import { maximumNowDelaySeconds, maximumQueryPoints, minimumQueryPoints } from "@/lib/workspace-query"
import { applyWorkspaceVariables, type WorkspaceVariableDraft } from "@/lib/workspace-variables"
import { maximumVariableRegexBytes, variableRegexError } from "@/lib/workspace-variable-options"
import { dashboardTagError, maximumDashboardTagsInputLength, parseDashboardTags } from "@/lib/workspace-dashboard-tags"
import { dashboardLinks, dashboardLinksError, emptyPanelLink, maximumPanelLinkTitleLength, maximumDashboardLinks, panelLinkError } from "@/lib/workspace-links"
import { WorkspaceSelect } from "./select"
import { WorkspaceLinkIconSelect } from "./link-icon"
import { randomUUID } from "@/lib/random-id"
import type { DashboardDefinition, DashboardPanelLink, DashboardVariable } from "@/types/metric-workspace"

export function WorkspaceDefinitionEditor(props: { definition: DashboardDefinition; close: () => void; apply: (value: DashboardDefinition) => void }) {
  const { t } = useLocale()
  const [definition, setDefinition] = useState(() => structuredClone(props.definition))
  const [variables, setVariables] = useState<WorkspaceVariableDraft[]>(() => props.definition.variables.map((value) => ({ id: randomUUID(), original: value.name, value: { ...value } })))
  const [error, setError] = useState<string | null>(null)
  function apply() {
    try {
      if (!definition.title.trim() || new TextEncoder().encode(definition.title).length > 120 || new TextEncoder().encode(definition.description).length > 2048) throw new Error("workspace.invalid_panel")
      if (definition.max_points !== undefined && !(Number.isInteger(definition.max_points) && definition.max_points >= minimumQueryPoints && definition.max_points <= maximumQueryPoints)) throw new Error("workspace.invalid_points")
      if (definition.now_delay_seconds !== undefined && !(Number.isInteger(definition.now_delay_seconds) && definition.now_delay_seconds >= 0 && definition.now_delay_seconds <= maximumNowDelaySeconds)) throw new Error("workspace.invalid_now_delay")
      const tagError = dashboardTagError(definition.tags ?? [])
      if (tagError) throw new Error(tagError)
      const linkError = dashboardLinksError(dashboardLinks(definition))
      if (linkError) throw new Error(linkError)
      props.apply(applyWorkspaceVariables(definition, variables))
      props.close()
    } catch (error) { setError(error instanceof Error ? error.message : "workspace.invalid_variable") }
  }
  return <Dialog open onOpenChange={(open) => { if (!open) props.close() }}><DialogContent className="max-h-[90svh] overflow-y-auto sm:max-w-2xl">
    <DialogHeader><DialogTitle>{t("workspace.edit_details")}</DialogTitle><DialogDescription>{t("workspace.description")}</DialogDescription></DialogHeader>
    <div className="grid gap-2"><Label htmlFor="dashboard-title">{t("workspace.dashboard_title")}</Label><Input id="dashboard-title" value={definition.title} maxLength={120} onChange={(event) => setDefinition({ ...definition, title: event.target.value })} /></div>
    <div className="grid gap-2"><Label htmlFor="dashboard-description">{t("workspace.dashboard_description")}</Label><Textarea id="dashboard-description" value={definition.description} maxLength={2048} onChange={(event) => setDefinition({ ...definition, description: event.target.value })} /></div>
    <WorkspaceTags definition={definition} change={setDefinition} />
    <WorkspaceQueryDefaults definition={definition} change={setDefinition} />
    <WorkspaceVariableFields variables={variables} change={setVariables} />
    <WorkspaceDashboardLinkFields definition={definition} change={setDefinition} />
    {error && <p role="alert" className="text-sm text-destructive">{t(error)}</p>}
    <DialogFooter><Button variant="outline" onClick={props.close}>{t("workspace.cancel")}</Button><Button onClick={apply}>{t("workspace.apply")}</Button></DialogFooter>
  </DialogContent></Dialog>
}

/** 标签输入保留原始文本，避免回写吃掉正在输入的逗号；只把解析后的标签交给看板定义。 */
function WorkspaceTags(props: { definition: DashboardDefinition; change: (value: DashboardDefinition) => void }) {
  const { t } = useLocale()
  const [text, setText] = useState(() => (props.definition.tags ?? []).join(", "))
  const tags = parseDashboardTags(text)
  const error = dashboardTagError(tags)
  const update = (value: string) => {
    setText(value)
    const next = parseDashboardTags(value)
    props.change({ ...props.definition, ...(next.length ? { tags: next } : { tags: undefined }) })
  }
  return <div className="grid gap-2"><Label htmlFor="dashboard-tags">{t("workspace.dashboard_tags")}</Label>
    <Input id="dashboard-tags" value={text} maxLength={maximumDashboardTagsInputLength} placeholder="prod, hk" onChange={(event) => update(event.target.value)} />
    <p className="text-xs text-muted-foreground">{t("workspace.dashboard_tags_hint")}</p>
    {error && <p role="alert" className="text-xs text-destructive">{t(error)}</p>}</div>
}

/** 看板级查询默认值：面板未覆盖时生效，服务端会再校验一次。 */
function WorkspaceQueryDefaults(props: { definition: DashboardDefinition; change: (value: DashboardDefinition) => void }) {
  const { t } = useLocale()
  const change = (key: "max_points" | "now_delay_seconds", value: string) => props.change({
    ...props.definition, [key]: value === "" ? undefined : Number(value) })
  return <fieldset className="grid gap-4 border-t pt-4 sm:grid-cols-2"><legend className="px-1 text-sm font-medium">{t("workspace.dashboard_defaults")}</legend>
    {([["max_points", String(maximumQueryPoints), minimumQueryPoints, maximumQueryPoints], ["now_delay_seconds", "0", 0, maximumNowDelaySeconds]] as const).map(([key, example, min, max]) =>
      <div key={key} className="grid gap-2"><Label htmlFor={`dashboard-${key}`}>{t(`workspace.${key}`)}</Label>
        <Input id={`dashboard-${key}`} type="number" min={min} max={max} placeholder={example} value={props.definition[key] ?? ""} onChange={(event) => change(key, event.target.value)} />
        <p className="text-xs text-muted-foreground">{t(`workspace.${key}_hint`)}</p></div>)}
  </fieldset>
}

function WorkspaceVariableFields(props: { variables: WorkspaceVariableDraft[]; change: (value: WorkspaceVariableDraft[]) => void }) {
  const { t } = useLocale()
  const change = (drafts: WorkspaceVariableDraft[], id: string, patch: Partial<DashboardVariable>) =>
    props.change(drafts.map((draft) => draft.id === id ? { ...draft, value: { ...draft.value, ...patch } } : draft))
  return <fieldset className="space-y-3 border-t pt-4"><legend className="px-1 text-sm font-medium">{t("workspace.variables")}</legend>
    {props.variables.map((draft, index) => <WorkspaceVariableRow key={draft.id} draft={draft} index={index} variables={props.variables}
      change={(id, patch) => change(props.variables, id, patch)} remove={(id) => props.change(props.variables.filter((value) => value.id !== id))} />)}
    <Button variant="outline" size="sm" disabled={props.variables.length >= 4} onClick={() => props.change([...props.variables, { id: randomUUID(), value: { name: `group${props.variables.length + 1}`, type: "group", label: t("workspace.group") } }])}>{t("workspace.add_variable")}</Button>
  </fieldset>
}

function WorkspaceVariableRow(props: {
  draft: WorkspaceVariableDraft; index: number; variables: WorkspaceVariableDraft[]
  change: (id: string, patch: Partial<DashboardVariable>) => void; remove: (id: string) => void
}) {
  const { t } = useLocale()
  const regex = props.draft.value.regex ?? ""
  const error = variableRegexError(regex)
  return <div className="grid gap-2 rounded-md border p-2 sm:grid-cols-[1fr_1fr_7rem_2rem]">
    <Input aria-label={`${t("workspace.variable_name")} ${props.index + 1}`} value={props.draft.value.name} maxLength={32} onChange={(event) => props.change(props.draft.id, { name: event.target.value })} />
    <Input aria-label={`${t("workspace.variable_label")} ${props.index + 1}`} value={props.draft.value.label} maxLength={80} onChange={(event) => props.change(props.draft.id, { label: event.target.value })} />
    <WorkspaceSelect label={`${t("workspace.variable_type")} ${props.index + 1}`} value={props.draft.value.type} options={[{ value: "node", label: t("workspace.node") }, { value: "group", label: t("workspace.group") }]} onChange={(type) => props.change(props.draft.id, { type: type as "node" | "group" })} />
    <Button size="icon-sm" variant="ghost" aria-label={t("workspace.variable_remove")} onClick={() => props.remove(props.draft.id)}><XIcon /></Button>
    <div className="flex flex-wrap items-center gap-3 sm:col-span-4">
      <span className="flex items-center gap-2"><Checkbox id={`variable-multi-${props.draft.id}`} checked={props.draft.value.multi === true}
        onCheckedChange={(value) => props.change(props.draft.id, value === true ? { multi: true } : { multi: false, include_all: undefined })} />
        <Label htmlFor={`variable-multi-${props.draft.id}`} className="font-normal">{t("workspace.variable_multi")}</Label></span>
      {props.draft.value.multi === true && <span className="flex items-center gap-2"><Checkbox id={`variable-all-${props.draft.id}`} checked={props.draft.value.include_all === true}
        onCheckedChange={(value) => props.change(props.draft.id, { include_all: value === true })} />
        <Label htmlFor={`variable-all-${props.draft.id}`} className="font-normal">{t("workspace.variable_include_all")}</Label></span>}
      <Input className="max-w-64" title={t("workspace.variable_regex_hint")} aria-label={`${t("workspace.variable_regex")} ${props.index + 1}`}
        placeholder={t("workspace.variable_regex")} value={regex} maxLength={maximumVariableRegexBytes}
        onChange={(event) => props.change(props.draft.id, { regex: event.target.value })} />
      {error && <span role="alert" className="text-xs text-destructive">{t(error)}</span>}
    </div>
  </div>
}

/** 看板级导航链接：标题、URL 与是否新标签页；URL 支持与面板链接相同的占位符。 */
function WorkspaceDashboardLinkFields(props: { definition: DashboardDefinition; change: (value: DashboardDefinition) => void }) {
  const { t } = useLocale()
  const links = dashboardLinks(props.definition)
  const change = (index: number, patch: Partial<DashboardPanelLink>) =>
    props.change({ ...props.definition, links: links.map((link, current) => current === index ? { ...link, ...patch } : link) })
  return <fieldset className="space-y-3 border-t pt-4"><legend className="px-1 text-sm font-medium">{t("workspace.dashboard_links")}</legend>
    <p className="text-xs text-muted-foreground">{t("workspace.dashboard_links_hint")}</p>
    {links.map((link, index) => <div key={index} className="flex flex-wrap items-center gap-3 rounded-md border p-2">
      <Input className="max-w-48" maxLength={maximumPanelLinkTitleLength} aria-label={`${t("workspace.link_title")} ${index + 1}`} placeholder={t("workspace.link_title")}
        value={link.title} onChange={(event) => change(index, { title: event.target.value })} />
      <Input className="min-w-64 flex-1" aria-label={`${t("workspace.link_url")} ${index + 1}`} placeholder="https://wiki.example.test/beat"
        value={link.url} onChange={(event) => change(index, { url: event.target.value })} />
      <WorkspaceLinkIconSelect link={link} label={`${t("workspace.link_icon")} ${index + 1}`} onChange={(icon) => change(index, { icon })} />
      <label className="flex items-center gap-2 text-sm"><Checkbox checked={link.new_tab === true} onCheckedChange={(checked) => change(index, { new_tab: checked === true ? true : undefined })} />{t("workspace.link_new_tab")}</label>
      <Button size="icon-sm" variant="ghost" aria-label={`${t("workspace.link_remove")} ${index + 1}`} onClick={() => props.change({ ...props.definition, links: links.filter((_, current) => current !== index) })}><XIcon /></Button>
      {panelLinkError(link) && <p role="alert" className="w-full text-xs text-destructive">{t("workspace.link_invalid")}</p>}
    </div>)}
    <Button variant="outline" size="sm" disabled={links.length >= maximumDashboardLinks} onClick={() => props.change({ ...props.definition, links: [...links, emptyPanelLink()] })}>{t("workspace.add_dashboard_link")}</Button>
    <div className="flex items-start gap-2 border-t pt-3"><Checkbox id="dashboard-hide-links" checked={props.definition.hide_links === true}
      onCheckedChange={(checked) => props.change({ ...props.definition, hide_links: checked === true ? true : undefined })} />
      <div className="grid gap-1"><Label htmlFor="dashboard-hide-links" className="font-normal">{t("workspace.hide_links")}</Label>
        <p className="text-xs text-muted-foreground">{t("workspace.hide_links_hint")}</p></div></div>
  </fieldset>
}
