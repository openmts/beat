import { GripVerticalIcon, InfoIcon, MaximizeIcon, MoreHorizontalIcon, SearchIcon } from "lucide-react"
import { useState, type ReactNode } from "react"
import { Button } from "@/components/ui/button"
import { Dialog, DialogContent, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuSeparator, DropdownMenuTrigger } from "@/components/ui/dropdown-menu"
import { Skeleton } from "@/components/ui/skeleton"
import { useLocale } from "@/context/locale"
import { panelTimeLabel } from "@/lib/workspace-time-override"
import { panelLinks, resolvePanelLinkURL } from "@/lib/workspace-links"
import { WorkspaceDataView } from "./data-view"
import { WorkspaceLinkIcon } from "./link-icon"
import { WorkspaceTextPanel } from "./text-panel"
import { PanelAlertBadge } from "./panel-alerts"
import { useCollaborationText } from "./collaboration-text"
import type { DashboardAnnotation, DashboardPanelAlert } from "@/lib/metric-collaboration-api"
import type { DashboardPanel, PanelQueryState, PanelSize, WorkspaceRange } from "@/types/metric-workspace"
import { formatTimeWithSeconds } from "@/lib/intl-format"

export type WorkspacePanelAction = "edit" | "duplicate" | "remove" | "up" | "down" | "link_alerts" | "save_library" | PanelSize
type PanelProps = {
  panel: DashboardPanel; state?: PanelQueryState; inspect: () => void; onRange: (range: WorkspaceRange) => void; retry?: () => void
  /** 面板级告警入口：深链到规则页并预填该面板的查询；只读看板不传。 */
  alertLink?: string
  /** v25 面板级告警关联：有关联时在面板头渲染聚合状态徽标。 */
  alerts?: DashboardPanelAlert[]
  /** v25 看板注释：在面板时间轴上按窗口叠加标记。 */
  annotations?: DashboardAnnotation[]
  /** 数据链接的占位符取值；缺省表示没有可替换的上下文。 */
  linkValues?: Record<string, string>
  /** 只读视图在 `hide_links` 打开时连面板数据链接一起隐藏；后台工作区始终保留。 */
  hideLinks?: boolean
} & ({ readOnly: true } | {
  readOnly?: false; index: number; count: number; busy: boolean; action: (action: WorkspacePanelAction) => void; drop: (id: string) => void
})

export function WorkspacePanel(props: PanelProps) {
  const { t, locale } = useLocale()
  const state = props.state
  const result = !props.readOnly && state?.result && state.loading && state.query ? { ...state.result, from: state.query.from, to: state.query.to } : state?.result
  const retry = props.retry
  const override = panelTimeLabel(props.panel)
  const [viewing, setViewing] = useState(false)
  const links = props.hideLinks ? [] : panelLinks(props.panel)
  // 文本面板不查询、不检查、也没有状态行：正文由 Markdown 渲染，链接条跟在正文下方。
  const text = props.panel.type === "text"
  const spans: Record<PanelSize, string> = { small: "lg:col-span-4", medium: "lg:col-span-6", large: "lg:col-span-12" }
  return <section className={`min-w-0 overflow-hidden rounded-lg border bg-card shadow-xs ${spans[props.panel.size]}`} aria-label={props.panel.title}
    onDragOver={props.readOnly ? undefined : (event) => { event.preventDefault(); event.dataTransfer.dropEffect = "move" }} onDrop={props.readOnly ? undefined : (event) => { event.preventDefault(); props.drop(event.dataTransfer.getData("text/beat-panel")) }}>
    <div className="flex items-center justify-between gap-2 border-b px-3 py-2">
      <div className="flex min-w-0 items-center gap-2" draggable={!props.readOnly && !props.busy} onDragStart={props.readOnly ? undefined : (event) => event.dataTransfer.setData("text/beat-panel", props.panel.id)}>
        {!props.readOnly && <GripVerticalIcon className="size-3.5 shrink-0 text-muted-foreground" aria-hidden="true" />}<h3 className="truncate text-sm font-medium" title={props.panel.title}>{props.panel.title}</h3>{props.panel.description && <span data-panel-description className="shrink-0 text-muted-foreground" role="img"
          title={props.panel.description} aria-label={props.panel.description}><InfoIcon className="size-3.5" aria-hidden="true" /></span>}{override && <span className="shrink-0 rounded border px-1.5 py-0.5 text-[10px] text-muted-foreground" title={t("workspace.query_options")}>{override}</span>}
      </div>
      <div className="flex items-center">{!props.readOnly && !text && <PanelAlertBadge alerts={props.alerts ?? []} context={props.panel.title} />}{!text && <HeaderIcon label={t("workspace.inspect")} context={props.panel.title} onClick={props.inspect}><SearchIcon /></HeaderIcon>}
        {!text && state?.result && <HeaderIcon label={t("workspace.view_panel")} context={props.panel.title} onClick={() => setViewing(true)}><MaximizeIcon /></HeaderIcon>}{!props.readOnly && <PanelMenu {...props} />}</div>
    </div>
    {text ? <WorkspaceTextPanel panel={props.panel} linkValues={props.linkValues} hideLinks={props.hideLinks} /> : <>
      {state?.error && <div role="alert" className="flex flex-wrap items-center justify-between gap-2 border-b bg-destructive/5 px-4 py-2 text-xs text-destructive"><span>{t(state.error)}</span>{retry && <Button variant="outline" size="sm" onClick={retry}>{t("workspace.retry")}</Button>}</div>}
      {result && (state?.loading || state?.error) && <p className="px-4 pt-2 text-xs text-muted-foreground">{t("workspace.last_good_range")}</p>}
      {links.length > 0 && <div data-workspace-links className="flex flex-wrap items-center justify-center gap-3 border-t px-4 py-2">
        {links.map((link, index) => <a key={index} href={resolvePanelLinkURL(link.url, props.linkValues ?? {})} target={link.new_tab ? "_blank" : undefined} rel={link.new_tab ? "noopener noreferrer" : undefined}
          className="inline-flex items-center gap-1 text-xs text-primary underline-offset-4 hover:underline"><WorkspaceLinkIcon link={link} />{link.title}</a>)}
      </div>}
      {result ? <WorkspaceDataView panel={props.panel} result={result} expanded={state?.resultExpanded} onRange={props.onRange} annotations={props.annotations} /> : state?.loading ? <Skeleton className="m-4 h-52" /> : <div className="grid h-60 place-content-center text-sm text-muted-foreground">{t("workspace.no_data")}</div>}
      <div className="flex min-h-8 items-center justify-between gap-2 border-t px-4 py-1.5 text-[11px] text-muted-foreground">
        <span>{state?.updatedAt ? t("workspace.last_success", { time: formatTimeWithSeconds(locale, state.updatedAt) }) : "—"}</span><span aria-live="polite">{state?.loading ? t("workspace.updating") : ""}</span>
      </div>
    </>}
{viewing && state && <PanelView panel={props.panel} state={state} onRange={props.onRange} annotations={props.annotations} close={() => setViewing(false)} />}
  </section>
}

/** 面板头图标按钮没有可见文字，必须带可读名称和 tooltip。 */
function HeaderIcon(props: { label: string; context: string; onClick: () => void; children: ReactNode }) {
  return <Button variant="ghost" size="icon-sm" title={props.label} aria-label={`${props.label} · ${props.context}`} onClick={props.onClick}>{props.children}</Button>
}

/** 面板大图查看：与 Grafana 的面板视图一致，只放大当前面板，不改变看板定义。 */
function PanelView(props: { panel: DashboardPanel; state: PanelQueryState; onRange: (range: WorkspaceRange) => void; close: () => void; annotations?: DashboardAnnotation[] }) {
  const result = props.state.result
  if (!result) return null
  return <Dialog open onOpenChange={(open) => { if (!open) props.close() }}>
    <DialogContent className="sm:max-w-6xl"><DialogHeader><DialogTitle>{props.panel.title}</DialogTitle></DialogHeader>
      <div className="min-w-0"><WorkspaceDataView panel={props.panel} result={result} expanded={props.state.resultExpanded} onRange={props.onRange} annotations={props.annotations} height={360} /></div>
    </DialogContent></Dialog>
}

function PanelMenu(props: { panel: DashboardPanel; index: number; count: number; busy: boolean; action: (action: WorkspacePanelAction) => void; alertLink?: string }) {
  const { t } = useLocale()
  const text = useCollaborationText()
  const actions = [["edit", "edit_panel"], ["duplicate", "duplicate_panel"], ["up", "move_up"], ["down", "move_down"]] as const
  const label = t("workspace.edit_panel")
  // 面板级告警是 Grafana 的常用入口：深链预填仍然保留，v25 起还可以持久化关联规则。
  const alertItem = props.alertLink ? <DropdownMenuItem render={<a href={props.alertLink} />}>{t("workspace.create_alert")}</DropdownMenuItem> : null
  return <DropdownMenu><DropdownMenuTrigger render={<Button variant="ghost" size="icon-sm" disabled={props.busy} title={label} aria-label={`${label} · ${props.panel.title}`} />}><MoreHorizontalIcon /></DropdownMenuTrigger>
    <DropdownMenuContent align="end">{alertItem}
      <DropdownMenuItem onClick={() => props.action("link_alerts")}>{text("link_alerts")}</DropdownMenuItem>
      <DropdownMenuItem onClick={() => props.action("save_library")}>{text("save_to_library")}</DropdownMenuItem>
      <DropdownMenuSeparator />{actions.map(([action, label]) => <DropdownMenuItem key={action} disabled={action === "up" && props.index === 0 || action === "down" && props.index === props.count - 1 || action === "duplicate" && props.count >= 24} onClick={() => props.action(action)}>{t(`workspace.${label}`)}</DropdownMenuItem>)}
      <DropdownMenuSeparator />{(["small", "medium", "large"] as const).map((size) => <DropdownMenuItem key={size} onClick={() => props.action(size)}>{t(`workspace.${size}`)}</DropdownMenuItem>)}
      <DropdownMenuSeparator /><DropdownMenuItem variant="destructive" onClick={() => props.action("remove")}>{t("workspace.remove_panel")}</DropdownMenuItem>
    </DropdownMenuContent>
  </DropdownMenu>
}
