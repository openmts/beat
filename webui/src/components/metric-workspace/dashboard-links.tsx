import { WorkspaceLinkIcon } from "./link-icon"
import { dashboardLinks, linksHiddenInReadOnly, resolvePanelLinkURL } from "@/lib/workspace-links"
import type { DashboardDefinition } from "@/types/metric-workspace"

/** 看板级导航链接条：与 Grafana 的 Dashboard links 一致，渲染在标题下方、面板之前。
 *  只读视图（公开节点页）在 `hide_links` 打开时整条隐藏，后台工作区照常显示。 */
export function WorkspaceDashboardLinks(props: { definition: DashboardDefinition; values: Record<string, string>; readOnly?: boolean }) {
  const links = dashboardLinks(props.definition)
  if (links.length === 0) return null
  if (props.readOnly && linksHiddenInReadOnly(props.definition)) return null
  return <nav data-workspace-dashboard-links aria-label={props.definition.title} className="flex flex-wrap items-center gap-2 rounded-md border bg-muted/40 px-3 py-2">
    {links.map((link, index) => <a key={index} href={resolvePanelLinkURL(link.url, props.values)} target={link.new_tab ? "_blank" : undefined} rel={link.new_tab ? "noopener noreferrer" : undefined}
      className="inline-flex items-center gap-1 rounded-md border bg-background px-2 py-1 text-xs font-medium text-primary underline-offset-4 hover:underline">
      <WorkspaceLinkIcon link={link} />{link.title}</a>)}
  </nav>
}
