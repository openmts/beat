import { render, screen } from "@testing-library/react"
import { MemoryRouter } from "react-router"
import { describe, expect, it } from "vitest"
import { LocaleProvider } from "@/context/locale"
import { PublicThemeProvider } from "@/components/public-theme-provider"
import { ThemedNodeCollection } from "@/components/themed-node-collection"
import { publicThemeStyles } from "@/lib/theme-presentation"
import { buildThemeSnapshot } from "@/lib/theme-snapshot"
import { defaultSiteSettings } from "@/context/site-settings"
import { THEME_PALETTE_TOKENS, type ThemeDefinition } from "@/types/theme-definition"
import type { Node } from "@/types"

function definition(overview: ThemeDefinition["overview"], content: Partial<ThemeDefinition> = {}): ThemeDefinition {
  const palette = Object.fromEntries(THEME_PALETTE_TOKENS.map((key) => [key, "#172033"])) as ThemeDefinition["light"]
  return { schema_version: 1, overview, detail: "balanced", density: "comfortable", radius: 6, light: palette, dark: { ...palette, background: "#111827" }, ...content }
}
const snapshot = (mode: "hidden" | "full" = "hidden") => buildThemeSnapshot({ route: { kind: "dashboard" }, site: { ...defaultSiteSettings, public_address_mode: mode },
  nodes: [{ id: "node", name: "Visible node", status: "online", is_public: true, group_id: "g", tags: ["edge"], public_remark: "Rack 12", host: "192.0.2.9",
    metrics: { cpu: 10, memory: 20, disk: 30 } } as unknown as Node,
    { id: "hidden", name: "Hidden node", is_public: false } as Node], groups: [{ id: "g", name: "Group", sort_order: 0 } as never], locale: "en", theme: "light" })

function renderCollection(value: ThemeDefinition, layout: ThemeDefinition["overview"], mode: "hidden" | "full" = "hidden") {
  render(<LocaleProvider><MemoryRouter><PublicThemeProvider definition={value} snapshot={snapshot(mode)}>
    <ThemedNodeCollection selectedGroup="" fleetLayout={layout === "cards" || layout === "table" ? layout : undefined} />
  </PublicThemeProvider></MemoryRouter></LocaleProvider>)
}

describe("declarative public themes", () => {
  it("uses scoped validated variables without changing the document root", () => {
    const root = document.documentElement.getAttribute("style")
    render(<PublicThemeProvider definition={definition("cards")} snapshot={snapshot()}><div>content</div></PublicThemeProvider>)
    const scoped = document.querySelector("[data-public-theme]") as HTMLElement
    expect(scoped.style.getPropertyValue("--background")).toBe("#172033")
    expect(scoped.style.getPropertyValue("--radius")).toBe("6px")
    expect(document.documentElement.getAttribute("style")).toBe(root)
    expect(() => publicThemeStyles({ ...definition("cards"), light: { ...definition("cards").light, background: "url(https://evil)" } }, "light")).toThrow()
  })
  it.each(["cards", "table", "tiles", "list"] as const)("renders a real %s layout using public values", (layout) => {
    render(<LocaleProvider><MemoryRouter><PublicThemeProvider definition={definition(layout)} snapshot={snapshot()}>
      <ThemedNodeCollection selectedGroup="" />
    </PublicThemeProvider></MemoryRouter></LocaleProvider>)
    expect(document.querySelector(`[data-theme-overview='${layout}']`)).toBeInTheDocument()
    const link = screen.getByRole("link", { name: /Visible node/ })
    expect(link).toHaveAttribute("href", "/node/node")
    // 截断的节点名必须保留完整 tooltip。
    expect(link).toHaveAttribute("title", "Visible node")
    expect(screen.queryByText("Hidden node")).not.toBeInTheDocument()
    if (layout === "table") expect(screen.getByRole("table")).toBeInTheDocument()
    if (layout === "list") expect(screen.getByRole("list")).toBeInTheDocument()
  })

  it("renders author-selected resources in the declared order", () => {
    renderCollection(definition("cards", { resources: ["memory", "cpu"] }), "cards")
    const labels = screen.getAllByText(/CPU|Memory|Root filesystem/)
    expect(labels.map((label) => label.textContent)).toEqual(["Memory", "CPU"])
    expect(screen.queryByText("Root filesystem")).not.toBeInTheDocument()
  })

  it("shows the declared content by default", () => {
    renderCollection(definition("tiles"), "tiles", "full")
    expect(screen.getByText("Rack 12")).toBeInTheDocument()
    expect(screen.getByText("edge")).toBeInTheDocument()
    expect(screen.getByText("192.0.2.9")).toBeInTheDocument()
    renderCollection(definition("cards"), "cards", "full")
    expect(screen.getAllByText(/↓ /).length).toBeGreaterThan(0)
  })

  it("hides content the author turns off", () => {
    renderCollection(definition("tiles", { show_host: false, show_tags: false, show_remark: false }), "tiles", "full")
    expect(screen.queryByText("Rack 12")).toBeNull()
    expect(screen.queryByText("edge")).toBeNull()
    expect(screen.queryByText("192.0.2.9")).toBeNull()
    renderCollection(definition("cards", { show_network: false }), "cards", "full")
    expect(screen.queryByText(/↓ /)).toBeNull()
  })

  it("rejects resource lists the renderer cannot honour", () => {
    const palette = definition("cards")
    expect(() => publicThemeStyles({ ...palette, resources: ["cpu", "cpu"] }, "light")).toThrow()
    expect(() => publicThemeStyles({ ...palette, resources: ["gpu" as never] }, "light")).toThrow()
    expect(() => publicThemeStyles({ ...palette, resources: [] }, "light")).toThrow()
    expect(() => publicThemeStyles({ ...palette, show_network: "no" as never }, "light")).toThrow()
    expect((publicThemeStyles({ ...palette, resources: ["disk"] }, "light") as Record<string, string>)["--radius"]).toBe("6px")
  })
  it("applies author-declared font stacks and keeps the default when omitted", () => {
    const palette = definition("cards")
    expect((publicThemeStyles({ ...palette, font: "mono" }, "light") as Record<string, string>).fontFamily).toMatch(/monospace/)
    expect((publicThemeStyles({ ...palette, font: "serif" }, "light") as Record<string, string>).fontFamily).toMatch(/serif/)
    expect((publicThemeStyles(palette, "light") as Record<string, string>).fontFamily).toBeUndefined()
    expect(() => publicThemeStyles({ ...palette, font: "url(https://evil)" as never }, "light")).toThrow()
    expect(() => publicThemeStyles({ ...palette, font: "comic" as never }, "light")).toThrow()
    render(<LocaleProvider><MemoryRouter><PublicThemeProvider definition={{ ...palette, font: "mono" }} snapshot={snapshot()}>
      <div>content</div>
    </PublicThemeProvider></MemoryRouter></LocaleProvider>)
    const scoped = document.querySelector("[data-public-theme]") as HTMLElement
    expect(scoped.style.fontFamily).toMatch(/monospace/)
  })
})
