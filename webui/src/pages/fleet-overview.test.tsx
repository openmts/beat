import { fireEvent, render, screen, within } from "@testing-library/react"
import { MemoryRouter } from "react-router"
import { describe, expect, it, vi } from "vitest"
import { PublicThemeProvider } from "@/components/public-theme-provider"
import { LocaleProvider } from "@/context/locale"
import { defaultSiteSettings } from "@/context/site-settings"
import { buildThemeSnapshot } from "@/lib/theme-snapshot"
import { FleetGroupTabs, FleetOverview } from "@/pages/fleet-overview"
import { THEME_PALETTE_TOKENS, type ThemeDefinition, type ThemeSummaryLayout, type ThemeSummaryMetric } from "@/types/theme-definition"
import type { Group, Node } from "@/types"

const palette = Object.fromEntries(THEME_PALETTE_TOKENS.map((key) => [key, "#172033"])) as ThemeDefinition["light"]

function definition(layout: ThemeSummaryLayout, metrics: ThemeSummaryMetric[] = ["availability", "busiest", "traffic", "throughput", "groups"]): ThemeDefinition {
  return {
    schema_version: 1, overview: "cards", detail: "balanced", density: "comfortable", radius: 6,
    surface: "soft", summary: { layout, metrics }, shell: { background: "grid", header: "solid", footer: "minimal" },
    light: palette, dark: palette,
  }
}

const nodes = [
  { id: "a", name: "Node A", alias: "Primary", group_id: "g1", status: "online", is_public: true, last_seen: "2026-01-01T00:00:00Z", telemetry_state: "fresh", telemetry_at: "2026-01-01T00:00:00Z", telemetry_age_seconds: 0,
    location: { scope: "public", country_code: "JP", resolved_at: "2026-01-01T00:00:00Z" },
    metrics: { cpu: 20, net_sent_total: 1024, net_recv_total: 2048, net_sent: 128, net_recv: 256 } },
  { id: "b", name: "Node B", alias: "", group_id: "g2", status: "online", is_public: true, last_seen: "2026-01-01T00:00:00Z", telemetry_state: "fresh", telemetry_at: "2026-01-01T00:00:00Z", telemetry_age_seconds: 0,
    location: { scope: "public", country_code: "DE", resolved_at: "2026-01-01T00:00:00Z" },
    metrics: { cpu: 55, net_sent_total: 4096, net_recv_total: 8192, net_sent: 64, net_recv: 128 } },
  { id: "c", name: "Node C", alias: "", group_id: "g1", status: "offline", is_public: true, last_seen: "2026-01-01T00:00:00Z", telemetry_state: "stale", telemetry_at: "2026-01-01T00:00:00Z", telemetry_age_seconds: 120,
    metrics: { cpu: 90 } },
] as unknown as Node[]

const groups = [
  { id: "g1", name: "Production", sort_order: 0 },
  { id: "g2", name: "Edge", sort_order: 1 },
] as Group[]

function renderOverview(layout: ThemeSummaryLayout, unavailable = false, metrics?: ThemeSummaryMetric[]) {
  const snapshot = buildThemeSnapshot({ route: { kind: "dashboard" }, site: defaultSiteSettings, nodes, groups, locale: "en", theme: "light" })
  return render(<LocaleProvider><MemoryRouter><PublicThemeProvider definition={definition(layout, metrics)} snapshot={snapshot}>
    <FleetOverview nodes={nodes} unavailable={unavailable} />
  </PublicThemeProvider></MemoryRouter></LocaleProvider>)
}

describe("theme-controlled fleet overview", () => {
  it.each(["cards", "strip", "focus", "minimal"] as const)("renders declared metrics in order for the %s layout", (layout) => {
    renderOverview(layout)
    const overview = screen.getByRole("region", { name: "Fleet overview" })
    expect(overview).toHaveAttribute("data-theme-summary", layout)
    expect([...overview.querySelectorAll("[data-theme-summary-metric]")].map((item) => item.getAttribute("data-theme-summary-metric")))
      .toEqual(["availability", "busiest", "traffic", "throughput", "groups"])
    expect(within(overview).getByText("Public nodes").parentElement).toHaveTextContent("2 / 3")
    expect(within(overview).getByText("Busiest node").parentElement).toHaveTextContent("55%")
    expect(within(overview).getByText("Busiest node").parentElement).toHaveTextContent("Node B")
    expect(overview.querySelector('[data-theme-summary-metric="busiest"] [data-country-code="DE"]')).toHaveTextContent("DE")
    expect(overview.querySelector('[data-theme-summary-metric="groups"]')).toHaveTextContent("2 Countries")
    if (layout === "focus") expect(overview.querySelector('[data-theme-summary-metric="availability"]')).toHaveClass("lg:row-span-2")
  })

  it("honours a reduced metric set and renders unavailable values as neutral", () => {
    renderOverview("minimal", true, ["availability", "traffic", "groups"])
    const overview = screen.getByRole("region", { name: "Fleet overview" })
    expect([...overview.querySelectorAll("[data-theme-summary-metric]")].map((item) => item.getAttribute("data-theme-summary-metric")))
      .toEqual(["availability", "traffic", "groups"])
    expect(within(overview).getByText("Public nodes").parentElement).toHaveTextContent("— / —")
    expect(within(overview).getAllByText("—").length).toBeGreaterThanOrEqual(3)
    expect(within(overview).queryByText("Busiest node")).not.toBeInTheDocument()
  })
})

describe("fleet group tabs", () => {
  it("shows every group count and emits the selected filters", () => {
    const change = vi.fn()
    render(<LocaleProvider><FleetGroupTabs nodes={nodes} groups={groups} value={[]} change={change} /></LocaleProvider>)
    fireEvent.click(screen.getByRole("button", { name: /Production/ }))
    expect(change).toHaveBeenCalledWith(["g1"])
    expect(screen.getByRole("button", { name: /^All/ })).toHaveAttribute("aria-pressed", "true")
    expect(screen.getByRole("button", { name: /Edge/ })).toHaveTextContent("1")
  })
})
