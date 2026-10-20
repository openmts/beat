import { render, screen } from "@testing-library/react"
import { beforeEach, describe, expect, it, vi } from "vitest"
import { PublicThemeBoundary } from "@/components/public-theme-boundary"
import { defaultSiteSettings } from "@/context/site-settings"
import { THEME_PALETTE_TOKENS, type ThemeDefinition } from "@/types/theme-definition"

const state = vi.hoisted(() => ({ descriptor: null as unknown, siteLoading: false, siteError: null as string | null, networkEnabled: false, network: vi.fn(), snapshots: [] as unknown[] }))
vi.mock("@/hooks/use-active-theme", () => ({ useActiveTheme: () => state.descriptor }))
vi.mock("@/context/site-settings", async (original) => ({ ...(await original<object>()), useSiteSettings: () => ({ settings: { ...defaultSiteSettings, show_network_quality: state.networkEnabled }, loading: state.siteLoading, error: state.siteError, reportInteraction: vi.fn() }) }))
vi.mock("@/context/locale", async () => {
  const { testLocaleValue } = await import("@/test/catalogs")
  return { useLocale: () => testLocaleValue("zh-CN"), useOptionalLocale: () => testLocaleValue("zh-CN") }
})
vi.mock("@/context/theme", () => ({ useTheme: () => ({ theme: "dark" }) }))
vi.mock("@/hooks/use-network", () => ({ usePublicNetworkQuality: () => state.network() }))
vi.mock("@/components/sandboxed-theme-frame", () => ({ SandboxedThemeFrame: ({ snapshot }: { snapshot: () => unknown }) => { state.snapshots.push(snapshot()); return <div>safe frame</div> } }))

const view = () => <PublicThemeBoundary route={{ kind: "dashboard" }} nodes={[]} groups={[]}><div>built-in</div></PublicThemeBoundary>
describe("public theme privacy boundary", () => {
  beforeEach(() => { state.descriptor = null; state.siteLoading = false; state.siteError = null; state.networkEnabled = false; state.snapshots = []; state.network.mockReset() })
  it("keeps the built-in page until public settings are known", () => {
    const rendered = render(view()); expect(screen.getByText("built-in")).toBeInTheDocument()
    state.descriptor = { runtime: "sandboxed", configuration: "{}" }; state.siteLoading = true
    rendered.rerender(view()); expect(screen.getByText("built-in")).toBeInTheDocument()
    state.siteLoading = false; state.siteError = "failed"
    rendered.rerender(view()); expect(screen.getByText("built-in")).toBeInTheDocument()
    expect(state.snapshots).toEqual([])
  })
  it("passes locale and appearance without fetching disabled network data", () => {
    state.descriptor = { runtime: "sandboxed", configuration: "{}" }
    render(view()); expect(screen.getByText("safe frame")).toBeInTheDocument()
    expect(state.snapshots[0]).toMatchObject({ locale: "zh-CN", theme: "dark", network: { visible: false, tasks: [] } })
    expect(state.network).not.toHaveBeenCalled()
  })
  it("uses only the public network hook and reports loading/error states", () => {
    state.descriptor = { runtime: "sandboxed", configuration: "{}" }; state.networkEnabled = true
    state.network.mockReturnValue({ data: null, loading: true, error: null })
    const rendered = render(view()); expect(state.snapshots.at(-1)).toMatchObject({ state: { network: "loading" } })
    state.network.mockReturnValue({ data: [], loading: false, error: "PRIVATE_INTERNAL_ERROR" })
    rendered.rerender(view()); expect(state.snapshots.at(-1)).toMatchObject({ state: { network: "error" } })
    expect(JSON.stringify(state.snapshots)).not.toContain("PRIVATE_INTERNAL_ERROR")
    state.network.mockReturnValue({ data: [], loading: false, error: null })
    rendered.rerender(view()); expect(state.snapshots.at(-1)).toMatchObject({ state: { network: "ready" } })
  })
  it("applies declarative styles through the public boundary and safely rejects invalid definitions", () => {
    const palette = Object.fromEntries(THEME_PALETTE_TOKENS.map((key) => [key, "#172033"])) as ThemeDefinition["light"]
    const definition: ThemeDefinition = { schema_version: 1, overview: "cards", detail: "balanced", density: "comfortable", radius: 6, light: palette, dark: palette }
    state.descriptor = { runtime: "declarative", configuration: "{}", declarative: definition }
    const rendered = render(view())
    expect(document.querySelector("[data-public-theme='cards']")).toBeInTheDocument()
    expect(screen.getByText("built-in")).toBeInTheDocument()
    state.descriptor = { runtime: "declarative", configuration: "{}", declarative: { ...definition, radius: 10 } }
    rendered.rerender(view())
    expect(document.querySelector("[data-public-theme]")).toBeNull()
    state.descriptor = { runtime: "declarative" }
    rendered.rerender(view())
    expect(screen.getByText("built-in")).toBeInTheDocument()
  })
})
