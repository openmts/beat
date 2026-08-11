import { fireEvent, render, renderHook, screen, waitFor } from "@testing-library/react"
import { beforeEach, describe, expect, it, vi } from "vitest"
import {
  SiteSettingsProvider,
  defaultSiteSettings,
  useSiteSettings,
} from "@/context/site-settings"
import * as api from "@/lib/api"

vi.mock("@/lib/api", () => ({ getSiteSettings: vi.fn() }))

const customSettings = {
  ...defaultSiteSettings,
  site_title: "Infrastructure",
  site_description: "Public infrastructure status",
  favicon_url: "/custom.svg",
  default_theme: "dark" as const,
  show_ip_addresses: false,
  updated_at: "2026-07-30T00:00:00Z",
}

function Harness() {
  const { settings, loading, error, applySettings, reload } = useSiteSettings()
  return (
    <div>
      <span>{loading ? "loading" : settings.site_title}</span>
      <span>{error}</span>
      <button onClick={() => applySettings({ ...settings, site_title: "Applied", favicon_url: "" })}>
        apply
      </button>
      <button onClick={() => void reload()}>reload</button>
    </div>
  )
}

describe("site settings context", () => {
  beforeEach(() => {
    vi.resetAllMocks()
    document.head.querySelectorAll("meta[name='description'], link[rel~='icon']").forEach((node) => node.remove())
  })

  it("loads settings and applies document metadata", async () => {
    vi.mocked(api.getSiteSettings).mockResolvedValue(customSettings)
    render(<SiteSettingsProvider><Harness /></SiteSettingsProvider>)
    expect(screen.getByText("loading")).toBeInTheDocument()
    expect(await screen.findByText("Infrastructure")).toBeInTheDocument()
    await waitFor(() => expect(document.title).toBe("Infrastructure"))
    expect(document.querySelector("meta[name='description']")).toHaveAttribute(
      "content",
      "Public infrastructure status",
    )
    expect(document.querySelector("link[rel~='icon']")).toHaveAttribute("href", "/custom.svg")

    fireEvent.click(screen.getByText("apply"))
    await waitFor(() => expect(document.title).toBe("Applied"))
    expect(document.querySelector("link[rel~='icon']")).toHaveAttribute("href", "/favicon.svg")
  })

  it("reports load failures and can retry", async () => {
    vi.mocked(api.getSiteSettings)
      .mockRejectedValueOnce(new Error("settings failed"))
      .mockRejectedValueOnce("invalid failure")
    render(<SiteSettingsProvider><Harness /></SiteSettingsProvider>)
    expect(await screen.findByText("settings failed")).toBeInTheDocument()
    fireEvent.click(screen.getByText("reload"))
    await waitFor(() => expect(screen.getByText("Failed to load site settings")).toBeInTheDocument())
  })

  it("returns defaults outside a provider", () => {
    const { result } = renderHook(() => useSiteSettings())
    expect(result.current.settings).toEqual(defaultSiteSettings)
  })
})
