import { fireEvent, render, screen, waitFor } from "@testing-library/react"
import { beforeEach, describe, expect, it, vi } from "vitest"
import { LocaleProvider } from "@/context/locale"
import { ThemesPanel } from "@/pages/admin/theme-panels"
import { THEME_PALETTE_TOKENS } from "@/types/theme-definition"

const api = vi.hoisted(() => ({
  list: vi.fn(), upload: vi.fn(), activate: vi.fn(), rollback: vi.fn(), remove: vi.fn(),
  exportTheme: vi.fn(), markets: vi.fn(), createMarket: vi.fn(), deleteMarket: vi.fn(),
  updateMarket: vi.fn(), refresh: vi.fn(), install: vi.fn(), preview: vi.fn(),
}))
vi.mock("@/lib/theme-api", () => ({
  listAdminThemes: () => api.list(),
  previewTheme: (id: string) => api.preview(id),
  uploadTheme: () => api.upload(),
  activateTheme: (id: string) => api.activate(id),
  rollbackTheme: () => api.rollback(),
  deleteTheme: (id: string) => api.remove(id),
  exportTheme: (id: string) => api.exportTheme(id),
  listThemeMarkets: () => api.markets(),
  createThemeMarket: () => api.createMarket(),
  deleteThemeMarket: (id: string) => api.deleteMarket(id),
  updateThemeMarket: () => api.updateMarket(),
  refreshThemeCatalog: (id: string) => api.refresh(id),
  installMarketTheme: (s: string, t: string) => api.install(s, t),
}))

const pkg = {
  id: "p1", theme_id: "t1", name: "Corporate", version: "1.0.0", author: "acme",
  description: "A corporate theme", license: "MIT", runtime: "declarative",
  manifest_schema: 1, minimum_beat_version: "0.1.0", archive_digest: "a".repeat(64),
  archive_bytes: 10, content_bytes: 5, source_type: "upload", state: "installed",
  validated_at: "2026-01-01T00:00:00Z", creator_id: "owner", created_at: "2026-01-01T00:00:00Z",
  updated_at: "2026-01-01T00:00:00Z", is_active: true, health_state: "healthy",
}

describe("themes panel installed view", () => {
  beforeEach(() => {
    vi.resetAllMocks()
    api.list.mockResolvedValue([pkg])
    api.markets.mockResolvedValue([])
  })

  it("lists installed themes with active state", async () => {
    render(<LocaleProvider><ThemesPanel /></LocaleProvider>)
    expect(await screen.findByText("Corporate")).toBeInTheDocument()
    expect(screen.getByText("1.0.0")).toBeInTheDocument()
    expect(screen.getByText("Active")).toBeInTheDocument()
  })

  it("activates a theme", async () => {
    api.list.mockResolvedValue([{ ...pkg, is_active: false }])
    api.activate.mockResolvedValue({ id: "p1", is_active: true })
    render(<LocaleProvider><ThemesPanel /></LocaleProvider>)
    fireEvent.click(await screen.findByRole("button", { name: "Activate" }))
    expect(api.activate).not.toHaveBeenCalled()
    fireEvent.click(screen.getByRole("button", { name: "Confirm" }))
    await waitFor(() => expect(api.activate).toHaveBeenCalledWith("p1"))
  })

  it("rolls back to built-in", async () => {
    api.rollback.mockResolvedValue({ rollback_target: "built-in" })
    render(<LocaleProvider><ThemesPanel /></LocaleProvider>)
    fireEvent.click(await screen.findByRole("button", { name: "Roll back" }))
    fireEvent.click(screen.getByRole("button", { name: "Confirm" }))
    await waitFor(() => expect(api.rollback).toHaveBeenCalled())
  })

  it("shows the declared site-configurable fields in the package declaration", async () => {
    const palette = Object.fromEntries(THEME_PALETTE_TOKENS.map((key) => [key, "#172033"]))
    api.preview.mockResolvedValue({
      package_id: "p1", theme_id: "t1", name: "Corporate", version: "1.0.0", runtime: "declarative", built_in: false, protocol: 2,
      declarative: { schema_version: 1, overview: "cards", detail: "balanced", density: "comfortable", radius: 6, light: palette, dark: palette },
      configuration_schema: { type: "managed", items: [
        { key: "overview", type: "select", options: ["cards", "table"] },
        { key: "show_tags", type: "switch" },
      ] },
    })
    render(<LocaleProvider><ThemesPanel /></LocaleProvider>)
    fireEvent.click(await screen.findByRole("button", { name: "Package declaration · Corporate" }))
    expect(await screen.findByText(/Site-configurable/)).toBeInTheDocument()
    expect(screen.getByText(/Overview layout: Grouped cards \/ Fleet table · Node tags: on \/ off/)).toBeInTheDocument()
  })

  it("shows an error on load failure", async () => {
    api.list.mockRejectedValue(new Error("store down"))
    render(<LocaleProvider><ThemesPanel /></LocaleProvider>)
    expect(await screen.findByText("store down")).toBeInTheDocument()
  })
})

describe("themes panel market view", () => {
  beforeEach(() => {
    vi.resetAllMocks()
    api.list.mockResolvedValue([])
    api.markets.mockResolvedValue([{ id: "m1", name: "Market One", catalog_url: "https://m.example/c", enabled: true, sort_order: 0, key_fingerprint: "fp", last_status: "", created_at: "2026-01-01T00:00:00Z", updated_at: "2026-01-01T00:00:00Z" }])
  })

  it("lists market sources", async () => {
    render(<LocaleProvider><ThemesPanel /></LocaleProvider>)
    fireEvent.click(screen.getByRole("tab", { name: "Market" }))
    expect(await screen.findByText("Market One")).toBeInTheDocument()
  })

  it("creates a market source through the dialog", async () => {
    api.createMarket.mockResolvedValue({ id: "m2" })
    render(<LocaleProvider><ThemesPanel /></LocaleProvider>)
    fireEvent.click(screen.getByRole("tab", { name: "Market" }))
    fireEvent.click(await screen.findByRole("button", { name: "Add source" }))
    fireEvent.change(screen.getByLabelText("Source name"), { target: { value: "Market Two" } })
    fireEvent.change(screen.getByLabelText("Catalog URL"), { target: { value: "https://m2.example/c" } })
    fireEvent.change(screen.getByLabelText("Public key"), { target: { value: "k".repeat(44) } })
    fireEvent.change(screen.getByLabelText("Key fingerprint"), { target: { value: "fp" } })
    fireEvent.click(screen.getByRole("button", { name: "Add source" }))
    await waitFor(() => expect(api.createMarket).toHaveBeenCalled())
  })

  it("refreshes a catalog and installs a theme", async () => {
    api.refresh.mockResolvedValue({
      schema_version: 1, source_id: "m1",
      themes: [{ theme_id: "market-theme", name: "Market Theme", version: "2.0.0", author: "a", description: "d", runtime: "declarative", package_url: "https://m.example/t.zip", package_sha256: "a".repeat(64), manifest_sha256: "b".repeat(64), minimum_beat_version: "0.1.0" }],
    })
    api.install.mockResolvedValue({ id: "p9" })
    render(<LocaleProvider><ThemesPanel /></LocaleProvider>)
    fireEvent.click(screen.getByRole("tab", { name: "Market" }))
    fireEvent.click(await screen.findByRole("button", { name: "Refresh catalog" }))
    expect((await screen.findAllByText("Market Theme")).length).toBeGreaterThan(0)
    fireEvent.click(screen.getAllByRole("button", { name: "Install" })[0])
    await waitFor(() => expect(api.install).toHaveBeenCalledWith("m1", expect.objectContaining({ theme_id: "market-theme", version: "2.0.0", package_sha256: "a".repeat(64) })))
  })
})
describe("themes panel additional coverage", () => {
  beforeEach(() => {
    vi.resetAllMocks()
    api.list.mockResolvedValue([pkg])
    api.markets.mockResolvedValue([{ id: "m1", name: "Market One", catalog_url: "https://m.example/c", enabled: true, sort_order: 0, key_fingerprint: "fp", last_status: "", created_at: "2026-01-01T00:00:00Z", updated_at: "2026-01-01T00:00:00Z" }])
  })

  it("deletes a theme", async () => {
    api.list.mockResolvedValue([{ ...pkg, is_active: false }])
    api.remove.mockResolvedValue(undefined)
    render(<LocaleProvider><ThemesPanel /></LocaleProvider>)
    fireEvent.click(await screen.findByRole("button", { name: "Delete" }))
    expect(api.remove).not.toHaveBeenCalled()
    fireEvent.click(screen.getByRole("button", { name: "Confirm" }))
    await waitFor(() => expect(api.remove).toHaveBeenCalledWith("p1"))
  })

  it("exports a theme", async () => {
    api.exportTheme.mockResolvedValue(new Blob(["zip"]))
    Object.defineProperty(window, "URL", { value: { createObjectURL: vi.fn(() => "blob:x"), revokeObjectURL: vi.fn() }, writable: true })
    render(<LocaleProvider><ThemesPanel /></LocaleProvider>)
    fireEvent.click(await screen.findByRole("button", { name: "Export" }))
    await waitFor(() => expect(api.exportTheme).toHaveBeenCalledWith("p1"))
  })

  it("toggles a market source enabled state", async () => {
    api.updateMarket.mockResolvedValue({ id: "m1", enabled: false })
    render(<LocaleProvider><ThemesPanel /></LocaleProvider>)
    fireEvent.click(screen.getByRole("tab", { name: "Market" }))
    const toggle = (await screen.findAllByRole("switch"))[0]
    fireEvent.click(toggle)
    fireEvent.click(screen.getByRole("button", { name: "Confirm" }))
    await waitFor(() => expect(api.updateMarket).toHaveBeenCalled())
  })

  it("deletes a market source", async () => {
    api.deleteMarket.mockResolvedValue(undefined)
    render(<LocaleProvider><ThemesPanel /></LocaleProvider>)
    fireEvent.click(screen.getByRole("tab", { name: "Market" }))
    fireEvent.click(await screen.findByRole("button", { name: "Delete" }))
    fireEvent.click(screen.getByRole("button", { name: "Confirm" }))
    await waitFor(() => expect(api.deleteMarket).toHaveBeenCalledWith("m1"))
  })

  it("surfaces a market refresh failure", async () => {
    api.refresh.mockRejectedValue(new Error("market down"))
    render(<LocaleProvider><ThemesPanel /></LocaleProvider>)
    fireEvent.click(screen.getByRole("tab", { name: "Market" }))
    fireEvent.click(await screen.findByRole("button", { name: "Refresh catalog" }))
    expect(await screen.findByText("market down")).toBeInTheDocument()
  })

  it("shows empty installed state", async () => {
    api.list.mockResolvedValue([])
    render(<LocaleProvider><ThemesPanel /></LocaleProvider>)
    expect(await screen.findByText("No theme packages installed. The built-in theme remains active.")).toBeInTheDocument()
  })
})

describe("themes panel dialog paths", () => {
  beforeEach(() => {
    vi.resetAllMocks()
    api.list.mockResolvedValue([])
    api.markets.mockResolvedValue([])
  })

  it("opens the upload dialog and submits without a file", async () => {
    render(<LocaleProvider><ThemesPanel /></LocaleProvider>)
    fireEvent.click(await screen.findByRole("button", { name: "Upload theme" }))
    expect(await screen.findByText("Upload a theme package")).toBeInTheDocument()
    fireEvent.submit(screen.getByRole("button", { name: "Upload theme" }).closest("form")!)
    await waitFor(() => expect(api.upload).not.toHaveBeenCalled())
  })
})

describe("themes panel error branches", () => {
  beforeEach(() => {
    vi.resetAllMocks()
    api.list.mockResolvedValue([{ ...pkg, is_active: false }])
    api.markets.mockResolvedValue([])
  })

  it("surfaces rollback error", async () => {
    api.rollback.mockRejectedValue(new Error("rollback failed"))
    render(<LocaleProvider><ThemesPanel /></LocaleProvider>)
    fireEvent.click(await screen.findByRole("button", { name: "Roll back" }))
    fireEvent.click(screen.getByRole("button", { name: "Confirm" }))
    expect(await screen.findByText("rollback failed")).toBeInTheDocument()
  })

  it("surfaces delete error", async () => {
    api.remove.mockRejectedValue(new Error("delete failed"))
    render(<LocaleProvider><ThemesPanel /></LocaleProvider>)
    fireEvent.click(await screen.findByRole("button", { name: "Delete" }))
    fireEvent.click(screen.getByRole("button", { name: "Confirm" }))
    expect(await screen.findByText("delete failed")).toBeInTheDocument()
  })

  it("surfaces market install error", async () => {
    api.list.mockResolvedValue([])
    api.markets.mockResolvedValue([{ id: "m1", name: "M", catalog_url: "https://m/c", enabled: true, sort_order: 0, key_fingerprint: "", last_status: "", created_at: "2026-01-01T00:00:00Z", updated_at: "2026-01-01T00:00:00Z" }])
    api.refresh.mockResolvedValue({ schema_version: 1, source_id: "m1", themes: [{ theme_id: "t1", name: "T", version: "1.0.0", author: "a", description: "d", runtime: "declarative", package_url: "https://m/t.zip", package_sha256: "a".repeat(64), manifest_sha256: "b".repeat(64), minimum_beat_version: "0.1.0" }] })
    api.install.mockRejectedValue(new Error("install failed"))
    render(<LocaleProvider><ThemesPanel /></LocaleProvider>)
    fireEvent.click(screen.getByRole("tab", { name: "Market" }))
    fireEvent.click(await screen.findByRole("button", { name: "Refresh catalog" }))
    await screen.findAllByText("T")
    fireEvent.click(screen.getAllByRole("button", { name: "Install" })[0])
    expect(await screen.findByText("install failed")).toBeInTheDocument()
  })
})
