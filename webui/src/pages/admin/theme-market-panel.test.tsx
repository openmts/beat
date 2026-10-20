import { fireEvent, render, screen, waitFor, within } from "@testing-library/react"
import { beforeEach, describe, expect, it, vi } from "vitest"
import { LocaleProvider } from "@/context/locale"
import { APIError } from "@/lib/api-error"
import * as api from "@/lib/theme-api"
import { reauthenticateAdmin } from "@/lib/security-api"
import { MarketSources } from "@/pages/admin/theme-market-panel"
import type { ThemeCatalog, ThemeMarketSource, ThemePackage } from "@/types/theme"

vi.mock("@/lib/theme-api")
vi.mock("@/lib/security-api", () => ({ reauthenticateAdmin: vi.fn() }))
const source: ThemeMarketSource = { id: "market", name: "Example publisher", catalog_url: "https://example.com/catalog.json", enabled: true,
  sort_order: 0, key_fingerprint: `sha256:${"c".repeat(64)}`, last_status: "ok", created_at: "2026-09-01T00:00:00Z", updated_at: "2026-09-01T00:00:00Z" }
const catalog: ThemeCatalog = { schema_version: 1, source_id: source.key_fingerprint, expires_at: "2099-01-01T00:00:00Z", catalog_digest: "d".repeat(64),
  cached: true, stale: false, fetched_at: "2026-09-01T00:00:00Z", cache_expires_at: "2099-01-01T00:00:00Z", themes: [
    { theme_id: "atlas", name: "Atlas", version: "2.0.0", author: "Example", description: "A public fleet layout", runtime: "declarative",
      package_url: "https://example.com/atlas.zip", package_sha256: "a".repeat(64), manifest_sha256: "b".repeat(64), minimum_beat_version: "0.1.0",
      preview_url: "https://not-for-browser.example/preview.png" },
  ] }
const pkg = { id: "package", theme_id: "atlas", version: "2.0.0", state: "installed", archive_digest: "a".repeat(64) } as ThemePackage

async function refreshCatalog() {
  fireEvent.click(await screen.findByRole("button", { name: "Refresh catalog" }))
  await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument())
}

function renderMarket(onInstalled = vi.fn()) {
  render(<LocaleProvider><MarketSources onInstalled={onInstalled} /></LocaleProvider>)
  return onInstalled
}

describe("verified theme market", () => {
  beforeEach(() => {
    vi.resetAllMocks()
    vi.mocked(api.listThemeMarkets).mockResolvedValue([source])
    vi.mocked(api.listAdminThemes).mockResolvedValue([])
    vi.mocked(api.refreshThemeCatalog).mockResolvedValue(catalog)
  })

  it("renders a full cached catalog once and never fetches remote preview URLs", async () => {
    renderMarket()
    await refreshCatalog()
    expect(screen.getAllByText("Atlas")).toHaveLength(1)
    expect(screen.getByText(source.key_fingerprint)).toBeInTheDocument()
    expect(screen.getByText("Verified cache")).toBeInTheDocument()
    expect(screen.queryByRole("img")).not.toBeInTheDocument()
    expect(screen.getByRole("button", { name: "Install" })).toBeEnabled()
  })

  it("explains stale fallback and both expiration times", async () => {
    vi.mocked(api.refreshThemeCatalog).mockResolvedValue({ ...catalog, stale: true, error_category: "unavailable" })
    renderMarket()
    await refreshCatalog()
    expect(screen.getByText("Source unavailable")).toBeInTheDocument()
    expect(screen.getByRole("status")).toHaveTextContent("previously verified catalog")
    expect(screen.getByText(/Cache valid until/)).toBeInTheDocument()
    expect(screen.getByText(/Catalog expires/)).toBeInTheDocument()
  })

  it("installs exactly the selected version and refreshes the library without activating", async () => {
    let installed = false
    vi.mocked(api.listAdminThemes).mockImplementation(async () => installed ? [pkg] : [])
    vi.mocked(api.installMarketTheme).mockImplementation(async () => { installed = true; return pkg })
    const onInstalled = renderMarket()
    await refreshCatalog()
    fireEvent.click(screen.getByRole("button", { name: "Install" }))
    expect(await screen.findByText("Installed Atlas 2.0.0. Open Installed to preview and activate it.")).toBeInTheDocument()
    expect(api.installMarketTheme).toHaveBeenCalledWith(source.id, catalog.themes[0])
    expect(onInstalled).toHaveBeenCalledOnce()
    expect(api.activateTheme).not.toHaveBeenCalled()
    expect(screen.getByRole("button", { name: "Installed" })).toBeDisabled()
  })

  it("keeps the selected entry after 409 and requires refresh before selecting again", async () => {
    vi.mocked(api.installMarketTheme).mockRejectedValueOnce(new APIError("changed", 409)).mockResolvedValue(pkg)
    renderMarket()
    await refreshCatalog()
    fireEvent.click(screen.getByRole("button", { name: "Install" }))
    expect(await screen.findByText(/Atlas 2.0.0 has changed/)).toBeInTheDocument()
    expect(screen.getByText("Atlas")).toBeInTheDocument()
    expect(screen.getByRole("button", { name: "Install" })).toBeDisabled()
    expect(api.installMarketTheme).toHaveBeenCalledOnce()
    vi.mocked(api.refreshThemeCatalog).mockResolvedValue({ ...catalog, cached: false, themes: [{ ...catalog.themes[0], version: "3.0.0" }] })
    await refreshCatalog()
    expect(screen.getByText("3.0.0")).toBeInTheDocument()
    expect(screen.getByRole("button", { name: "Install" })).toBeEnabled()
    expect(screen.queryByText(/has changed/)).not.toBeInTheDocument()
  })

  it("clears the displayed catalog if a new verification fails", async () => {
    renderMarket()
    await refreshCatalog()
    vi.mocked(api.refreshThemeCatalog).mockRejectedValue(new Error("invalid signature"))
    vi.mocked(api.listThemeMarkets).mockResolvedValue([{ ...source, last_status: "failed" }])
    fireEvent.click(screen.getByRole("button", { name: "Refresh catalog" }))
    expect(await screen.findByText("invalid signature")).toBeInTheDocument()
    fireEvent.click(screen.getByRole("button", { name: "Cancel" }))
    expect(screen.queryByText("Atlas")).not.toBeInTheDocument()
    expect(screen.getByText(/No usable catalog is available/)).toBeInTheDocument()
  })

  it("retains source form input across recent authentication", async () => {
    vi.mocked(api.createThemeMarket).mockRejectedValueOnce(new APIError("proof", 428)).mockResolvedValue(source)
    renderMarket()
    fireEvent.click(await screen.findByRole("button", { name: "Add source" }))
    fireEvent.change(screen.getByLabelText("Source name"), { target: { value: "Trusted publisher" } })
    fireEvent.change(screen.getByLabelText("Catalog URL"), { target: { value: source.catalog_url } })
    fireEvent.change(screen.getByLabelText("Public key"), { target: { value: "A".repeat(43) + "=" } })
    fireEvent.click(within(screen.getByRole("dialog")).getByRole("button", { name: "Add source" }))
    fireEvent.change(await screen.findByLabelText("Current password"), { target: { value: "owner proof" } })
    fireEvent.click(screen.getByRole("button", { name: "Verify and continue" }))
    await waitFor(() => expect(api.createThemeMarket).toHaveBeenCalledTimes(2))
    expect(vi.mocked(api.createThemeMarket).mock.calls[0]).toEqual(vi.mocked(api.createThemeMarket).mock.calls[1])
    expect(reauthenticateAdmin).toHaveBeenCalledWith("owner proof", "")
    await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument())
  })

  it("can close an unfinished source form without changing trust settings", async () => {
    renderMarket()
    fireEvent.click(await screen.findByRole("button", { name: "Add source" }))
    fireEvent.change(screen.getByLabelText("Source name"), { target: { value: "Unfinished source" } })
    fireEvent.click(within(screen.getByRole("dialog")).getByRole("button", { name: "Close" }))
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument()
    expect(api.createThemeMarket).not.toHaveBeenCalled()
  })

  it("edits source identity fields and discards the old catalog", async () => {
    vi.mocked(api.updateThemeMarket).mockResolvedValue(source)
    renderMarket()
    await refreshCatalog()
    fireEvent.click(screen.getByRole("button", { name: "Edit source" }))
    expect(screen.getByLabelText("Public key")).not.toBeRequired()
    fireEvent.change(screen.getByLabelText("Source name"), { target: { value: "Renamed publisher" } })
    fireEvent.change(screen.getByLabelText("Catalog URL"), { target: { value: "https://new.example/catalog.json" } })
    fireEvent.change(screen.getByLabelText("Key fingerprint"), { target: { value: source.key_fingerprint } })
    fireEvent.click(within(screen.getByRole("dialog")).getByRole("switch"))
    fireEvent.click(screen.getByRole("button", { name: "Save source" }))
    await waitFor(() => expect(api.updateThemeMarket).toHaveBeenCalledWith(source.id, expect.objectContaining({ name: "Renamed publisher", public_key: "", enabled: false })))
    await waitFor(() => expect(screen.queryByText("Atlas")).not.toBeInTheDocument())
  })

  it("supports empty catalogs and disables refresh for disabled sources", async () => {
    vi.mocked(api.refreshThemeCatalog).mockResolvedValue({ ...catalog, themes: [] })
    renderMarket()
    await refreshCatalog()
    expect(screen.getByText("This signed catalog contains no themes.")).toBeInTheDocument()
    vi.mocked(api.updateThemeMarket).mockResolvedValue({ ...source, enabled: false })
    vi.mocked(api.listThemeMarkets).mockResolvedValue([{ ...source, enabled: false }])
    fireEvent.click(screen.getByRole("switch"))
    expect(api.updateThemeMarket).not.toHaveBeenCalled()
    fireEvent.click(screen.getByRole("button", { name: "Confirm" }))
    await waitFor(() => expect(screen.getByRole("button", { name: "Refresh catalog" })).toBeDisabled())
    expect(api.updateThemeMarket).toHaveBeenCalledWith(source.id, {
      name: source.name, catalog_url: source.catalog_url, public_key: "", key_fingerprint: "", enabled: false, sort_order: 0,
    })
  })
})
