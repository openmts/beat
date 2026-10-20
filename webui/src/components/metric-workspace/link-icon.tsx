import { AlertTriangleIcon, BookOpenIcon, ExternalLinkIcon, InfoIcon, LayoutDashboardIcon, LinkIcon } from "lucide-react"
import { useLocale } from "@/context/locale"
import { linkIcon } from "@/lib/workspace-links"
import { dashboardLinkIcons, type DashboardLinkIcon, type DashboardPanelLink } from "@/types/metric-workspace"
import { WorkspaceSelect } from "./select"

const icons = {
  external: ExternalLinkIcon, dashboard: LayoutDashboardIcon, alert: AlertTriangleIcon,
  docs: BookOpenIcon, info: InfoIcon, link: LinkIcon,
}

/** 链接按钮图标：与服务端 model.DashboardLinkIcons 一一对应；未知值已在 linkIcon 里退回 external。 */
export function WorkspaceLinkIcon(props: { link: DashboardPanelLink; className?: string }) {
  const Component = icons[linkIcon(props.link)]
  return <Component className={props.className ?? "size-3"} aria-hidden />
}

/** 图标选择器：面板链接与看板链接共用；选回 external 时省略字段，配置文件里只留非默认项。 */
export function WorkspaceLinkIconSelect(props: { link: DashboardPanelLink; label: string; onChange: (icon: DashboardLinkIcon | undefined) => void }) {
  const { t } = useLocale()
  const options = dashboardLinkIcons.map((value) => ({ value, label: t(`workspace.link_icon_${value}`) }))
  return <WorkspaceSelect label={props.label} className="max-w-36" value={linkIcon(props.link)} options={options}
    onChange={(value) => props.onChange(value === "external" ? undefined : value as DashboardLinkIcon)} />
}
