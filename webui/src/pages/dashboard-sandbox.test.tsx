import type { ReactNode } from "react"
import { render, screen, waitFor } from "@testing-library/react"
import { MemoryRouter } from "react-router"
import { beforeEach, describe, expect, it, vi } from "vitest"
import { LocaleProvider } from "@/context/locale"
import { ThemeProvider } from "@/context/theme"
import Dashboard from "@/pages/dashboard"
import type { ActiveThemeDescriptor } from "@/types/theme"

const activeTheme = vi.hoisted(() => ({ get: vi.fn() }))
vi.mock("@/lib/theme-api", () => ({ getActiveTheme: () => activeTheme.get() }))
vi.mock("@/hooks/use-public-fleet", () => ({ usePublicFleet: () => ({ nodes: [], groups: [], loading: false, error: "", offline: false, receivedAt: null, serverTime: null, stale: false, connected: true, refresh: vi.fn() }) }))
vi.mock("@/hooks/use-api", () => ({
  useLiveNodes: () => ({ data: [], loading: false, error: null, refresh: vi.fn() }),
  useGroups: () => ({ data: [], loading: false, error: null, refresh: vi.fn() }),
}))
vi.mock("@/context/site-settings", () => ({
  useSiteSettings: () => ({ settings: { show_network_quality: false }, loading: false, error: null, reportInteraction: vi.fn() }),
}))
vi.mock("@/context/auth", () => ({ useAuth: () => ({}) }))

function Providers({ children }: { children: ReactNode }) {
  return <ThemeProvider><LocaleProvider><MemoryRouter initialEntries={["/"]}>{children}</MemoryRouter></LocaleProvider></ThemeProvider>
}

const sandboxDescriptor: ActiveThemeDescriptor = {
  package_id: "p1", theme_id: "t1", name: "Sandbox Theme", version: "1.0.0",
  runtime: "sandboxed", built_in: false, protocol: 2,
  sandbox_entry: "index.html", archive_digest: "a".repeat(64),
}

describe("dashboard sandbox rendering", () => {
  beforeEach(() => { vi.resetAllMocks() })

  it("renders the built-in dashboard when no sandbox theme is active", async () => {
    activeTheme.get.mockResolvedValue({ theme_id: "built-in", name: "Built-in", version: "1.0.0", runtime: "declarative", built_in: true, protocol: 2 })
    render(<Providers><Dashboard /></Providers>)
    expect(await screen.findByText("No matching public nodes")).toBeInTheDocument()
  })

  it("renders the sandbox frame for an active sandboxed theme", async () => {
    activeTheme.get.mockResolvedValue(sandboxDescriptor)
    render(<Providers><Dashboard /></Providers>)
    await waitFor(() => expect(screen.getByTitle("Sandbox Theme")).toBeInTheDocument())
    const frame = screen.getByTitle("Sandbox Theme") as HTMLIFrameElement
    expect(frame.getAttribute("sandbox")).toBe("allow-scripts")
  })
})