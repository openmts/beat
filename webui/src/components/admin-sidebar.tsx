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
  KeyIcon,
  TerminalIcon,
  BellIcon,
  LayoutDashboardIcon,
  ActivityIcon,
  Settings2Icon,
  ShieldCheckIcon,
} from "lucide-react"
import { useLocale } from "@/context/locale"
import { useSiteSettings } from "@/context/site-settings"
import { cn } from "@/lib/utils"

const navItems = [
  {
    labelKey: "security.title",
    icon: ShieldCheckIcon,
    to: "/admin/security",
  },
  {
    labelKey: "group.manage",
    icon: FolderTreeIcon,
    to: "/admin/groups",
  },
  {
    labelKey: "app.nodes",
    icon: ServerIcon,
    to: "/admin/nodes",
  },
  {
    labelKey: "ssh.keys",
    icon: KeyIcon,
    to: "/admin/ssh-keys",
  },
  {
    labelKey: "terminal.title",
    icon: TerminalIcon,
    to: "/admin/terminal",
  },
  {
    labelKey: "alert.manage",
    icon: BellIcon,
    to: "/admin/alerts",
  },
  {
    labelKey: "network.manage",
    icon: ActivityIcon,
    to: "/admin/network",
  },
  {
    labelKey: "settings.site",
    icon: Settings2Icon,
    to: "/admin/settings",
  },
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
        <SidebarGroup>
          <SidebarGroupLabel>{t("app.admin")}</SidebarGroupLabel>
          <SidebarGroupContent>
            {navItems.map((item) => {
              const isActive =
                location.pathname === item.to ||
                (item.to !== "/admin/settings" && location.pathname.startsWith(item.to))
              return (
                <SidebarMenuItem key={item.to}>
                  <SidebarMenuButton
                    render={<Link to={item.to} />}
                    isActive={isActive}
                    tooltip={t(item.labelKey)}
                  >
                    <item.icon data-icon="inline-start" />
                    <span className={cn(isActive && "font-medium")}>{t(item.labelKey)}</span>
                  </SidebarMenuButton>
                </SidebarMenuItem>
              )
            })}
          </SidebarGroupContent>
        </SidebarGroup>
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
