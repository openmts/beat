import { render, screen } from "@testing-library/react"
import { MemoryRouter } from "react-router"
import { beforeEach, describe, expect, it, vi } from "vitest"
import Dashboard from "./dashboard"
import { workspaceNode } from "@/test/metric-workspace-fixtures"
const state = vi.hoisted(() => ({ nodes: [] as ReturnType<typeof workspaceNode>[], groups: [{ id: "group-one", name: "Production", sort_order: 0 }], loading: false, error: "", connected: true, offline: false, receivedAt: 1000, serverTime: 1000, stale: false, refresh: vi.fn() }))
const settings = vi.hoisted(() => ({ settings: { site_title: "Beat", public_address_mode: "hidden", show_network_quality: true }, loading: false, error: null as string | null }))
vi.mock("@/hooks/use-public-fleet", () => ({ usePublicFleet: () => state }))
vi.mock("@/context/site-settings", () => ({ useSiteSettings: () => ({ ...settings, reportInteraction: vi.fn() }) }))
vi.mock("@/context/locale", async () => {
  const { testLocaleValue } = await import("@/test/catalogs")
  return { useLocale: () => testLocaleValue("en"), useOptionalLocale: () => testLocaleValue("en") }
})
vi.mock("@/context/theme", () => ({ useTheme: () => ({ theme: "light", toggleTheme: vi.fn() }) }))
vi.mock("@/components/header", () => ({ Header: () => <header>Header</header> }))
vi.mock("@/components/public-footer", () => ({ PublicFooter: () => <footer>Footer</footer> }))
vi.mock("@/components/public-theme-boundary", () => ({ PublicThemeBoundary: ({ children }: { children: React.ReactNode }) => children }))
vi.mock("@/components/node-card", () => ({ NodeCard: ({ loading }: { loading?: boolean }) => <article>{loading ? "Loading card" : "Node card"}</article> }))
vi.mock("@/components/network-quality-band", () => ({ NetworkQualityBand: () => <aside>Network quality</aside> }))
vi.mock("@/pages/fleet-results", () => ({ FleetResults: ({ nodes }: { nodes: { host: string }[] }) => <section data-testid="results">{nodes.map(node => node.host).join(",")}</section> }))
beforeEach(() => { state.nodes = [workspaceNode()]; state.loading = false; state.offline = false; state.error = ""; settings.loading = false; settings.error = null })
describe("public overview integration", () => {
 it("keeps host fields hidden until public settings load and masks offline network widgets", () => {
  const view = render(<MemoryRouter><Dashboard /></MemoryRouter>); expect(screen.getByTestId("results")).not.toHaveTextContent("192.0.2.1"); expect(screen.getByText("Network quality")).toBeInTheDocument()
  state.loading = true; view.rerender(<MemoryRouter><Dashboard /></MemoryRouter>); expect(screen.getAllByText("Loading card")).toHaveLength(8)
  state.loading = false; state.offline = true; settings.error = "unavailable"; view.rerender(<MemoryRouter><Dashboard /></MemoryRouter>); expect(screen.queryByText("Network quality")).not.toBeInTheDocument()
 })
})
