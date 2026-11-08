import { render, screen } from "@testing-library/react"
import { beforeEach, describe, expect, it, vi } from "vitest"
import { PublicThemeBoundary } from "@/components/public-theme-boundary"
import { defaultSiteSettings } from "@/context/site-settings"
import { THEME_PALETTE_TOKENS, type ThemeDefinition } from "@/types/theme-definition"

const state = vi.hoisted(() => ({
  active: { status: "ready", descriptor: null } as unknown,
  siteLoading: false,
  siteError: null as string | null,
  networkEnabled: false,
  network: vi.fn(),
  snapshots: [] as unknown[],
  setRange: undefined as unknown,
}))
vi.mock("@/hooks/use-active-theme", () => ({ useActiveThemeState: () => state.active }))
vi.mock("@/context/site-settings", async (original) => ({ ...(await original<object>()), useSiteSettings: () => ({ settings: { ...defaultSiteSettings, show_network_quality: state.networkEnabled }, loading: state.siteLoading, error: state.siteError, reportInteraction: vi.fn() }) }))
vi.mock("@/context/locale", async () => {
  const { testLocaleValue } = await import("@/test/catalogs")
  return { useLocale: () => testLocaleValue("zh-CN"), useOptionalLocale: () => testLocaleValue("zh-CN") }
})
vi.mock("@/context/theme", () => ({ useTheme: () => ({ theme: "dark" }) }))
vi.mock("@/hooks/use-network", () => ({ usePublicNetworkQuality: () => state.network() }))
vi.mock("@/components/sandboxed-theme-frame", () => ({
  SandboxedThemeFrame: ({ snapshot, onSetRange }: { snapshot: () => unknown; onSetRange?: unknown }) => {
    state.snapshots.push(snapshot())
    state.setRange = onSetRange
    return <div>safe frame</div>
  },
}))

const view = () => <PublicThemeBoundary route={{ kind: "dashboard" }} nodes={[]} groups={[]}><div>built-in</div></PublicThemeBoundary>
const sandbox = () => ({ status: "ready", descriptor: { runtime: "sandboxed", configuration: "{}" } })
describe("public theme privacy boundary", () => {
  beforeEach(() => {
    state.active = { status: "ready", descriptor: null }
    state.siteLoading = false
    state.siteError = null
    state.networkEnabled = false
    state.snapshots = []
    state.setRange = undefined
    state.network.mockReset()
  })
  it("shows a neutral boot surface while descriptors load and falls back only on failure", () => {
    state.active = { status: "loading", descriptor: null }
    const rendered = render(view())
    expect(screen.getByRole("status")).toBeInTheDocument()
    expect(screen.queryByText("built-in")).not.toBeInTheDocument()
    state.active = sandbox(); state.siteLoading = true
    rendered.rerender(view()); expect(screen.getByRole("status")).toBeInTheDocument()
    expect(screen.queryByText("built-in")).not.toBeInTheDocument()
    state.siteLoading = false; state.siteError = "failed"
    rendered.rerender(view()); expect(screen.getByText("built-in")).toBeInTheDocument()
    state.active = { status: "failed", descriptor: null }; state.siteError = null
    rendered.rerender(view()); expect(screen.getByText("built-in")).toBeInTheDocument()
    expect(state.snapshots).toEqual([])
  })
  it("passes locale and appearance without fetching disabled network data", () => {
    state.active = sandbox()
    render(view()); expect(screen.getByText("safe frame")).toBeInTheDocument()
    expect(state.snapshots[0]).toMatchObject({ locale: "zh-CN", theme: "dark", network: { visible: false, tasks: [] } })
    expect(state.network).not.toHaveBeenCalled()
  })
  it("uses only the public network hook and reports loading/error states", () => {
    state.active = sandbox(); state.networkEnabled = true
    state.network.mockReturnValue({ data: null, loading: true, error: null })
    const rendered = render(view()); expect(state.snapshots.at(-1)).toMatchObject({ state: { network: "loading" } })
    state.network.mockReturnValue({ data: [], loading: false, error: "PRIVATE_INTERNAL_ERROR" })
    rendered.rerender(view()); expect(state.snapshots.at(-1)).toMatchObject({ state: { network: "error" } })
    expect(JSON.stringify(state.snapshots)).not.toContain("PRIVATE_INTERNAL_ERROR")
    state.network.mockReturnValue({ data: [], loading: false, error: null })
    rendered.rerender(view()); expect(state.snapshots.at(-1)).toMatchObject({ state: { network: "ready" } })
  })
  it("reuses network data already loaded by the full-page caller", () => {
    state.active = sandbox()
    state.networkEnabled = true
    render(<PublicThemeBoundary route={{ kind: "dashboard" }} nodes={[]} groups={[]} state={{ nodes: "ready" }}
      network={{ data: [], state: "ready" }}><div>built-in</div></PublicThemeBoundary>)
    expect(state.network).not.toHaveBeenCalled()
    expect(state.snapshots.at(-1)).toMatchObject({ network: { visible: true, tasks: [] }, state: { nodes: "ready", network: "ready" } })
  })
  it("passes the selected range and range callback to node themes only", () => {
    state.active = sandbox()
    state.networkEnabled = true
    state.network.mockReturnValue({ data: [], loading: false, error: null })
    const onRangeChange = vi.fn()
    const node = { id: "node", name: "Node", alias: "Node", group_id: "group", status: "online", is_public: true, tags: [], public_remark: "", last_seen: "", metrics: {} } as never
    const rendered = render(<PublicThemeBoundary route={{ kind: "node", node_id: "node" }} nodes={[node]} groups={[]}
      range={{ mode: "relative", seconds: 21600 }} onRangeChange={onRangeChange} network={{ data: [], state: "ready" }}
      networkHistory={{ from: 100, to: 200, tasks: [] }}><div>built-in</div></PublicThemeBoundary>)
    expect(state.snapshots.at(-1)).toMatchObject({ range: { mode: "relative", seconds: 21600 }, range_options: [300, 900, 3600, 21600, 86400, 604800, 2592000],
      network_history: { from: 100, to: 200, tasks: [] } })
    expect(state.setRange).toBe(onRangeChange)
    rendered.rerender(<PublicThemeBoundary route={{ kind: "dashboard" }} nodes={[]} groups={[]}
      range={{ mode: "relative", seconds: 21600 }} onRangeChange={onRangeChange} network={{ data: [], state: "ready" }}><div>built-in</div></PublicThemeBoundary>)
    expect(state.setRange).toBeUndefined()
  })
  it("applies declarative styles through the public boundary and safely rejects invalid definitions", () => {
    const palette = Object.fromEntries(THEME_PALETTE_TOKENS.map((key) => [key, "#172033"])) as ThemeDefinition["light"]
    const definition: ThemeDefinition = { schema_version: 1, overview: "cards", detail: "balanced", density: "comfortable", radius: 6, light: palette, dark: palette }
    state.active = { status: "ready", descriptor: { runtime: "declarative", configuration: "{}", declarative: definition } }
    const rendered = render(view())
    expect(document.querySelector("[data-public-theme='cards']")).toBeInTheDocument()
    expect(screen.getByText("built-in")).toBeInTheDocument()
    state.active = { status: "ready", descriptor: { runtime: "declarative", configuration: "{}", declarative: { ...definition, radius: 10 } } }
    rendered.rerender(view())
    expect(document.querySelector("[data-public-theme]")).toBeNull()
    state.active = { status: "ready", descriptor: { runtime: "declarative" } }
    rendered.rerender(view())
    expect(screen.getByText("built-in")).toBeInTheDocument()
  })
})
