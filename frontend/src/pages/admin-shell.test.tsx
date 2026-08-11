import type { ReactNode } from "react"
import { fireEvent, render, screen } from "@testing-library/react"
import { MemoryRouter, Route, Routes } from "react-router"
import { beforeEach, describe, expect, it, vi } from "vitest"
import { LocaleProvider } from "@/context/locale"
import { ThemeProvider } from "@/context/theme"
import Admin from "@/pages/admin"
import AdminHeader from "@/components/admin-header"
import AdminSidebar from "@/components/admin-sidebar"
import AdminLayout from "@/pages/admin/layout"

const auth = vi.hoisted(() => ({ authenticated: false, loading: false, setupRequired: false,
  principal: null, login: vi.fn(), bootstrap: vi.fn(), logout: vi.fn(), refresh: vi.fn() }))
vi.mock("@/context/auth", () => ({
  AuthProvider: ({ children }: { children: ReactNode }) => <>{children}</>,
  useAuth: () => auth,
}))
vi.mock("@/pages/admin/login", () => ({ default: () => <div>login-page</div> }))
vi.mock("@/pages/admin/groups", () => ({ default: () => <div>groups-page</div> }))
vi.mock("@/pages/admin/nodes", () => ({ default: () => <div>nodes-page</div> }))
vi.mock("@/pages/admin/ssh-keys", () => ({ default: () => <div>keys-page</div> }))
vi.mock("@/pages/admin/terminal", () => ({ default: () => <div>terminal-page</div> }))
vi.mock("@/pages/admin/alerts", () => ({ default: () => <div>alerts-page</div> }))
vi.mock("@/pages/admin/settings", () => ({ default: () => <div>settings-page</div> }))
vi.mock("@/pages/admin/security", () => ({ default: () => <div>security-page</div> }))
vi.mock("@/components/ui/sidebar", () => ({
  SidebarProvider: ({ children }: { children: ReactNode }) => <div>{children}</div>,
  SidebarInset: ({ children }: { children: ReactNode }) => <div>{children}</div>,
  Sidebar: ({ children }: { children: ReactNode }) => <aside>{children}</aside>,
  SidebarContent: ({ children }: { children: ReactNode }) => <div>{children}</div>,
  SidebarFooter: ({ children }: { children: ReactNode }) => <footer>{children}</footer>,
  SidebarGroup: ({ children }: { children: ReactNode }) => <div>{children}</div>,
  SidebarGroupContent: ({ children }: { children: ReactNode }) => <div>{children}</div>,
  SidebarGroupLabel: ({ children }: { children: ReactNode }) => <div>{children}</div>,
  SidebarHeader: ({ children }: { children: ReactNode }) => <div>{children}</div>,
  SidebarMenuItem: ({ children }: { children: ReactNode }) => <div>{children}</div>,
  SidebarSeparator: () => <hr />,
  SidebarTrigger: () => <button>sidebar</button>,
  SidebarMenuButton: ({ children, render: rendered }: { children: ReactNode; render?: ReactNode }) => rendered ? <span>{rendered}{children}</span> : <button>{children}</button>,
}))

function providers(children: ReactNode, path = "/admin/groups") {
  return <ThemeProvider><LocaleProvider><MemoryRouter initialEntries={[path]}>{children}</MemoryRouter></LocaleProvider></ThemeProvider>
}

describe("admin shell", () => {
  beforeEach(() => {
    vi.resetAllMocks()
    auth.authenticated = false
  })

  it("gates and routes admin pages", () => {
    const adminRoutes = <Routes><Route path="/admin/*" element={<Admin />} /></Routes>
    const { unmount } = render(providers(adminRoutes))
    expect(screen.getByText("login-page")).toBeInTheDocument()
    unmount()
    auth.authenticated = true
    render(providers(adminRoutes))
    expect(screen.getByText("groups-page")).toBeInTheDocument()
  })

  it("renders layout outlet", () => {
    render(providers(<Routes><Route element={<AdminLayout />}><Route path="/admin/groups" element={<div>outlet-page</div>} /></Route></Routes>))
    expect(screen.getByText("outlet-page")).toBeInTheDocument()
  })

  it("renders sidebar navigation", () => {
    render(providers(<AdminSidebar />, "/admin/nodes"))
    expect(screen.getByText("Manage Groups")).toBeInTheDocument()
    expect(screen.getByText("Dashboard")).toBeInTheDocument()
    expect(screen.getByText("Site settings")).toBeInTheDocument()
    expect(document.querySelector('a[href="/admin/nodes"]')).toBeInTheDocument()
  })

  it("runs header actions", () => {
    render(providers(<AdminHeader />))
    fireEvent.click(screen.getByRole("button", { name: "Theme" }))
    expect(document.documentElement).toHaveClass("dark")
    fireEvent.click(screen.getByRole("button", { name: "Sign out" }))
    fireEvent.click(screen.getByRole("button", { name: "Language" }))
    expect(auth.logout).toHaveBeenCalled()
  })
})
