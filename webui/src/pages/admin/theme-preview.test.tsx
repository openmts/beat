import { fireEvent, render, screen, waitFor } from "@testing-library/react"
import { MemoryRouter } from "react-router"
import { beforeEach, describe, expect, it, vi } from "vitest"
import userEvent from "@testing-library/user-event"
import type { ReactNode } from "react"
import { ThemePreview } from "@/pages/admin/theme-preview"
import { LocaleProvider } from "@/context/locale"
import { defaultSiteSettings } from "@/context/site-settings"
import { THEME_PALETTE_TOKENS, type ThemeDefinition } from "@/types/theme-definition"
import type { ActiveThemeDescriptor, ThemePackage } from "@/types/theme"
import * as api from "@/lib/theme-api"

const source = vi.hoisted(() => ({ nodes: { loading: false, error: null as string | null, data: [
  { id: "public", name: "Visible node", alias: "", is_public: true, group_id: "group", status: "online", tags: ["edge"], public_remark: "Public note", metrics: { cpu: 24, memory: 40, disk: 50, private_sample: 99 },
    host: "PRIVATE_IP", ssh_public_key: "PRIVATE_KEY", private_remark: "PRIVATE_REMARK", os: "linux", last_seen: "2026-09-13T00:00:00Z" },
  { id: "private", name: "PRIVATE_NODE", is_public: false },
] }, metrics: { loading: false, error: null as string | null, data: { cpu: [{ timestamp: 20, value: 24 }], private_sample: [{ timestamp: 20, value: 999 }] }, from: 0, to: 100 } }))
vi.mock("@/lib/theme-api")
vi.mock("@/hooks/use-api", () => ({ useNodes: () => source.nodes, useGroups: () => ({ data: [{ id: "group", name: "Public group", sort_order: 0 }], loading: false, error: null }), useNodeMetrics: () => source.metrics }))
vi.mock("@/context/site-settings", async (original) => ({ ...await original<object>(), useSiteSettings: () => ({ settings: { ...defaultSiteSettings, site_title: "Public service", public_address_mode: "hidden" }, loading: false, error: null, reportInteraction: vi.fn() }) }))
vi.mock("@/components/theme-preview-viewport", () => ({ ThemePreviewViewport: ({ mobile, children }: { mobile: boolean; children: ReactNode }) => <section data-mobile={mobile}>{children}</section> }))
vi.mock("@/components/metrics-chart", () => ({ MetricsChart: ({ title }: { title: string }) => <p>History {title}</p> }))
const palette = Object.fromEntries(THEME_PALETTE_TOKENS.map((key) => [key, "#172033"])) as ThemeDefinition["light"]
const definition: ThemeDefinition = { schema_version: 1, overview: "cards", detail: "balanced", density: "comfortable", radius: 6, light: palette, dark: { ...palette, background: "#090c12" } }
const descriptor: ActiveThemeDescriptor = { package_id: "pkg", theme_id: "theme", name: "Slate", version: "1.0.0", protocol: 2, runtime: "declarative", built_in: false, configuration: "{}", declarative: definition }
const saved = vi.fn(async () => {})
function preview() { return render(<LocaleProvider><MemoryRouter><ThemePreview theme={{ id: "pkg", name: "Slate" } as ThemePackage} onClose={vi.fn()} onSaved={saved} /></MemoryRouter></LocaleProvider>) }

describe("read-only theme preview and constrained settings", () => {
  beforeEach(() => {
    vi.resetAllMocks()
    source.nodes.error = null
    source.nodes.loading = false
    source.metrics.error = null
    source.metrics.loading = false
    vi.mocked(api.previewTheme).mockResolvedValue(descriptor)
  })
  it("shows only public fields and keeps theme changes inside the preview", async () => {
    preview()
    expect(await screen.findByRole("link", { name: "Visible node" })).toBeInTheDocument()
    expect(document.body.textContent).not.toMatch(/PRIVATE_/)
    const root = document.documentElement.getAttribute("style")
    fireEvent.click(screen.getByRole("button", { name: "Dark" }))
    expect((document.querySelector("[data-public-theme]") as HTMLElement).style.getPropertyValue("--background")).toBe("#090c12")
    fireEvent.click(screen.getByRole("button", { name: "Mobile" }))
    expect(document.querySelector("[data-mobile='true']")).toBeInTheDocument()
    expect(document.documentElement.getAttribute("style")).toBe(root)
    expect(api.activateTheme).not.toHaveBeenCalled()
    expect(api.configureTheme).not.toHaveBeenCalled()
  })
  it("uses local navigation to preview a node and return", async () => {
    preview()
    fireEvent.click(await screen.findByRole("link", { name: "Visible node" }))
    expect(await screen.findByText("History CPU")).toBeInTheDocument()
    expect(document.querySelector("[data-theme-detail-layout='balanced']")).toBeInTheDocument()
    expect(document.body.textContent).not.toMatch(/PRIVATE_/)
    fireEvent.click(screen.getByRole("button", { name: "Back to overview" }))
    expect(screen.getByRole("link", { name: "Visible node" })).toBeInTheDocument()
  })
  it("saves and resets settings only after confirmation", async () => {
    vi.mocked(api.configureTheme).mockResolvedValue({ ...descriptor, configuration: '{"radius":0}', declarative: { ...definition, radius: 0 } })
    preview()
    fireEvent.change(await screen.findByLabelText("Corner radius (px)"), { target: { value: "0" } })
    fireEvent.click(screen.getByRole("button", { name: "Save settings" }))
    expect(api.configureTheme).not.toHaveBeenCalled()
    fireEvent.click(screen.getByRole("button", { name: "Confirm" }))
    await waitFor(() => expect(api.configureTheme).toHaveBeenCalledWith("pkg", { overview: "cards", detail: "balanced", density: "comfortable", radius: 0 }))
    await waitFor(() => expect(saved).toHaveBeenCalledOnce())
    fireEvent.click(screen.getByRole("button", { name: "Reset settings" }))
    fireEvent.click(screen.getByRole("button", { name: "Confirm" }))
    await waitFor(() => expect(api.configureTheme).toHaveBeenLastCalledWith("pkg", {}))
  })
  it("rejects invalid theme definitions and handles unavailable data", async () => {
    vi.mocked(api.previewTheme).mockResolvedValueOnce({ ...descriptor, declarative: { ...definition, radius: 99 } })
    const first = preview()
    expect(await screen.findByText(/The theme preview is unavailable/)).toBeInTheDocument()
    first.unmount()
    source.nodes.error = "offline"
    preview()
    expect(await screen.findByText(/The theme preview is unavailable/)).toBeInTheDocument()
  })
  it("shows metric failure and loading states", async () => {
    source.metrics.error = "failed"
    const first = preview()
    fireEvent.click(await screen.findByRole("link", { name: "Visible node" }))
    expect(screen.getByRole("alert")).toBeInTheDocument()
    first.unmount()
    source.metrics.error = null
    source.metrics.loading = true
    preview()
    fireEvent.click(await screen.findByRole("link", { name: "Visible node" }))
    expect(screen.getByRole("status")).toBeInTheDocument()
  })
  it("renders the fields a package declares and saves only those", async () => {
    const declared: ActiveThemeDescriptor = { ...descriptor, configuration_schema: { type: "managed", items: [
      { key: "overview", type: "select", options: ["cards", "table"] },
      { key: "font", type: "select", options: ["system", "mono"] },
      { key: "show_host", type: "switch" },
      { key: "radius", type: "number" },
    ] } }
    vi.mocked(api.previewTheme).mockResolvedValue(declared)
    vi.mocked(api.configureTheme).mockResolvedValue(declared)
    preview()
    const overview = await screen.findByRole("combobox", { name: "Overview layout" })
    expect(screen.queryByRole("combobox", { name: "Density" })).not.toBeInTheDocument()
    expect(screen.queryByRole("combobox", { name: "Detail layout" })).not.toBeInTheDocument()
    await userEvent.click(screen.getByRole("combobox", { name: "Font" }))
    expect(await screen.findByRole("option", { name: "System" })).toBeInTheDocument()
    await userEvent.click(await screen.findByRole("option", { name: "Monospace" }))
    await userEvent.click(overview)
    expect(screen.queryByRole("option", { name: "Spacious tiles" })).not.toBeInTheDocument()
    await userEvent.click(await screen.findByRole("option", { name: "Fleet table" }))
    fireEvent.click(screen.getByRole("switch", { name: "Host address" }))
    fireEvent.change(screen.getByLabelText("Corner radius (px)"), { target: { value: "3" } })
    fireEvent.click(screen.getByRole("button", { name: "Save settings" }))
    fireEvent.click(screen.getByRole("button", { name: "Confirm" }))
    await waitFor(() => expect(api.configureTheme).toHaveBeenCalledWith("pkg", {
      overview: "table", font: "mono", show_host: false, radius: 3,
    }))
  })
  it("previews all layout and density controls before saving", async () => {
    const user = userEvent.setup()
    preview()
    const overview = await screen.findByRole("combobox", { name: "Overview layout" })
    await user.click(overview)
    await user.click(await screen.findByRole("option", { name: "Fleet table" }))
    expect(document.querySelector("[data-theme-overview='table']")).toBeInTheDocument()
    await user.click(screen.getByRole("combobox", { name: "Detail layout" }))
    await user.click(await screen.findByRole("option", { name: "Compact" }))
    await user.click(screen.getByRole("combobox", { name: "Density" }))
    await user.click(await screen.findByRole("option", { name: "Compact" }))
    expect(document.querySelector("[data-theme-density='compact']")).toBeInTheDocument()
    fireEvent.click(screen.getByRole("link", { name: "Visible node" }))
    expect(document.querySelector("[data-theme-detail-layout='compact']")).toBeInTheDocument()
    await user.click(screen.getByRole("combobox", { name: "Detail layout" }))
    await user.click(await screen.findByRole("option", { name: "Spotlight" }))
    expect(document.querySelector("[data-theme-detail-layout='spotlight']")).toBeInTheDocument()
    fireEvent.change(screen.getByLabelText("Corner radius (px)"), { target: { value: "9" } })
    expect(screen.getByLabelText("Corner radius (px)")).toHaveValue(6)
    expect(api.configureTheme).not.toHaveBeenCalled()
  })
})
