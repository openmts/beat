import { useLocation, Link } from "react-router"
import {
  Sidebar,
  SidebarContent,
  SidebarFooter,
  SidebarGroup,
  SidebarGroupContent,
  SidebarGroupLabel,
  SidebarHeader,
  SidebarMenuButton,
  SidebarMenuItem,
  SidebarSeparator,
} from "@/components/ui/sidebar"
import {
  FolderTreeIcon,
  ServerIcon,
  PlugZapIcon,
  KeyIcon,
  TerminalIcon,
  BellIcon,
  LayoutDashboardIcon,
  ActivityIcon,
  Settings2Icon,
  ShieldCheckIcon,
  GaugeIcon,
  ChartLineIcon,
  UserRoundPlusIcon,
} from "lucide-react"
import { useLocale } from "@/context/locale"
import { useSiteSettings } from "@/context/site-settings"
import { cn } from "@/lib/utils"

const accessNavItems = [
  { labelKey: "setup.title", icon: PlugZapIcon, to: "/admin/agent-setup" },
  { labelKey: "setup.enrollment", icon: UserRoundPlusIcon, to: "/admin/enrollment" },
  { labelKey: "app.nodes", icon: ServerIcon, to: "/admin/nodes" },
  { labelKey: "group.manage", icon: FolderTreeIcon, to: "/admin/groups" },
  { labelKey: "ssh.keys", icon: KeyIcon, to: "/admin/ssh-keys" },
]
const managementNavItems = [
  { labelKey: "accelerator.title", icon: GaugeIcon, to: "/admin/accelerators" },
  { labelKey: "lifecycle.title", icon: ServerIcon, to: "/admin/lifecycle" },
  { labelKey: "custom_metric.title", icon: GaugeIcon, to: "/admin/custom-metrics" },
  { labelKey: "workspace.title", icon: ChartLineIcon, to: "/admin/metrics" },
  { labelKey: "alert.manage", icon: BellIcon, to: "/admin/alerts" },
  { labelKey: "network.manage", icon: ActivityIcon, to: "/admin/network" },
  { labelKey: "settings.site", icon: Settings2Icon, to: "/admin/settings" },
]
const advancedNavItems = [
  { labelKey: "agent.releases", icon: PlugZapIcon, to: "/admin/agent-releases" },
  { labelKey: "operations.title", icon: TerminalIcon, to: "/admin/operations" },
  { labelKey: "terminal.title", icon: TerminalIcon, to: "/admin/terminal" },
  { labelKey: "security.title", icon: ShieldCheckIcon, to: "/admin/security" },
]
const navGroups = [
  { labelKey: "nav.access_and_nodes", items: accessNavItems },
  { labelKey: "nav.platform_management", items: managementNavItems },
  { labelKey: "nav.advanced_operations", items: advancedNavItems },
]

function AdminSidebar() {
  const location = useLocation()
  const { t } = useLocale()
  const { settings } = useSiteSettings()

  return (
    <Sidebar collapsible="icon">
      <SidebarHeader className="flex h-12 items-center gap-2 border-b px-3">
        {settings.logo_url && (
          <img
            src={settings.logo_url}
            alt=""
            className="size-6 shrink-0 object-contain"
            referrerPolicy="no-referrer"
          />
        )}
        <span className="truncate font-heading text-sm font-semibold group-data-[collapsible=icon]:hidden">
          {settings.site_title || t("app.title")}
        </span>
      </SidebarHeader>
      <SidebarContent>
        {navGroups.map(group => <SidebarGroup key={group.labelKey}>
          <SidebarGroupLabel>{t(group.labelKey)}</SidebarGroupLabel>
          <SidebarGroupContent>
            {group.items.map(item => {
              const isActive = location.pathname === item.to || (item.to !== "/admin/settings" && location.pathname.startsWith(item.to))
              return <SidebarMenuItem key={item.to}><SidebarMenuButton render={<Link to={item.to} />} isActive={isActive} tooltip={t(item.labelKey)}><item.icon data-icon="inline-start" /><span className={cn(isActive && "font-medium")}>{t(item.labelKey)}</span></SidebarMenuButton></SidebarMenuItem>
            })}
          </SidebarGroupContent>
        </SidebarGroup>)}
      </SidebarContent>
      <SidebarSeparator />
      <SidebarFooter>
        <SidebarMenuButton
          render={<Link to="/" />}
          tooltip={t("app.dashboard")}
        >
          <LayoutDashboardIcon data-icon="inline-start" />
          <span>{t("app.dashboard")}</span>
        </SidebarMenuButton>
      </SidebarFooter>
    </Sidebar>
  )
}

export default AdminSidebar
