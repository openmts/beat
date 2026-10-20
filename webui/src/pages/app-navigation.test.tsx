import type { ReactNode } from "react"
import { StrictMode } from "react"
import AxiosMockAdapter from "axios-mock-adapter"
import { render, screen, waitFor } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { api } from "@/lib/api-client"
import App from "@/App"

const mock = new AxiosMockAdapter(api)
const lifecycleChunkDelayMs = 250

vi.mock("@/context/auth", () => ({
  AuthProvider: ({ children }: { children: ReactNode }) => <>{children}</>,
  useAuth: () => ({
    authenticated: true,
    loading: false,
    setupRequired: false,
    principal: null,
    login: vi.fn(),
    bootstrap: vi.fn(),
    logout: vi.fn(),
    refresh: vi.fn(),
  }),
}))

vi.mock("@/hooks/use-public-fleet", () => ({
  usePublicFleet: () => ({
    nodes: [], groups: [], loading: false, error: "", connected: true,
    offline: false, stale: false, receivedAt: null, serverTime: null, refresh: vi.fn(),
  }),
}))

vi.mock("@/pages/admin/groups", () => ({ default: () => <h1>Groups page</h1> }))
vi.mock("@/pages/admin/lifecycle", async () => {
  await new Promise(resolve => setTimeout(resolve, lifecycleChunkDelayMs))
  return { default: () => <h1>Lifecycle page</h1> }
})

const siteSettings = {
  site_title: "Beat Monitor", site_description: "", logo_url: "", favicon_url: "/favicon.svg",
  default_theme: "system", public_address_mode: "full", show_network_quality: false, updated_at: "",
}

beforeEach(() => {
  mock.reset()
  mock.onGet("/api/v1/settings/site").reply(200, { code: 200, data: siteSettings })
  window.history.replaceState(null, "", "/")
})

afterEach(() => {
  window.history.replaceState(null, "", "/")
})

describe("front and admin navigation", () => {
  it("renders linked pages without a document reload", { timeout: 40000 }, async () => {
    const user = userEvent.setup()
    render(<StrictMode><App /></StrictMode>)

    expect(await screen.findByRole("link", { name: "Beat Monitor" })).toBeInTheDocument()
    // 仪表盘现在是路由级懒加载，首屏断言必须等待分片解析完成。
    expect(await screen.findByText(/Matching:/)).toBeInTheDocument()

    await user.click(screen.getByTitle("Admin"))
    expect(await screen.findByRole("link", { name: "Location & lifecycle" })).toBeInTheDocument()
    expect(window.location.pathname).toBe("/admin")

    await user.click(screen.getByRole("link", { name: "Location & lifecycle" }))
    expect(await screen.findByRole("heading", { name: "Lifecycle page" })).toBeInTheDocument()
    expect(window.location.pathname).toBe("/admin/lifecycle")
    expect(screen.queryByRole("heading", { name: "Groups page" })).not.toBeInTheDocument()

    await user.click(screen.getByRole("link", { name: "Manage Groups" }))
    expect(await screen.findByRole("heading", { name: "Groups page" })).toBeInTheDocument()
    expect(window.location.pathname).toBe("/admin/groups")

    await user.click(screen.getByRole("link", { name: "Dashboard" }))
    await waitFor(() => expect(window.location.pathname).toBe("/"))
    expect(await screen.findByText(/Matching:/)).toBeInTheDocument()
    expect(screen.queryByRole("heading", { name: "Groups page" })).not.toBeInTheDocument()
  })
})
