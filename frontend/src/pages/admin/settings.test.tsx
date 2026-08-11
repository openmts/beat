import { fireEvent, render, screen, waitFor } from "@testing-library/react"
import { beforeEach, describe, expect, it, vi } from "vitest"
import { LocaleProvider } from "@/context/locale"
import Settings from "@/pages/admin/settings"
import * as api from "@/lib/api"
import type { MaintenanceOverview, SiteSettings } from "@/types"

const contextMocks = vi.hoisted(() => ({ applySettings: vi.fn() }))
const siteState = vi.hoisted(() => ({
  settings: {
    site_title: "Beat Monitor",
    site_description: "Status page",
    logo_url: "/logo.svg",
    favicon_url: "/favicon.svg",
    default_theme: "system",
    show_ip_addresses: true,
    show_network_quality: true,
    updated_at: "",
  } as SiteSettings,
  loading: false,
  error: null as string | null,
}))

vi.mock("@/context/site-settings", () => ({
  useSiteSettings: () => ({ ...siteState, applySettings: contextMocks.applySettings }),
}))
vi.mock("@/lib/api", () => ({
  updateSiteSettings: vi.fn(),
  getMaintenanceOverview: vi.fn(),
  updateMaintenanceSettings: vi.fn(),
  startMaintenance: vi.fn(),
}))
vi.mock("@/components/ui/select", async () => {
  const mocks = await vi.importActual<typeof import("@/test/admin-ui-mocks")>("@/test/admin-ui-mocks")
  return {
    Select: mocks.MockSelect,
    SelectContent: mocks.PassThrough,
    SelectGroup: mocks.PassThrough,
    SelectItem: mocks.MockSelectItem,
    SelectTrigger: () => null,
    SelectValue: () => null,
  }
})
vi.mock("@/components/ui/switch", () => ({
  Switch: ({ checked, onCheckedChange, ...props }: {
    checked: boolean
    onCheckedChange?: (checked: boolean) => void
  }) => (
    <button
      type="button"
      role="switch"
      aria-checked={checked}
      onClick={() => onCheckedChange?.(!checked)}
      {...props}
    >toggle</button>
  ),
}))

function view() {
  return render(<LocaleProvider><Settings /></LocaleProvider>)
}

const maintenanceOverview: MaintenanceOverview = {
  settings: {
    retention_days: 30,
    auto_cleanup_enabled: true,
    cleanup_hour_utc: 3,
    updated_at: "2026-07-30T00:00:00Z",
  },
  status: {
    running: false,
    last_started_at: null,
    last_completed_at: null,
    last_status: "never",
    last_error: "",
    last_cutoff_at: null,
    last_duration_ms: 0,
    last_trigger: "",
    sqlite_integrity: "",
  },
  storage: {
    mts_bytes: 10 * 1024 ** 2,
    sqlite_bytes: 256 * 1024,
    total_bytes: 10.25 * 1024 ** 2,
    mts_healthy: true,
    mts_health_reasons: [],
  },
}

describe("admin site settings", () => {
  beforeEach(() => {
    vi.resetAllMocks()
    siteState.loading = false
    siteState.error = null
    siteState.settings = {
      site_title: "Beat Monitor",
      site_description: "Status page",
      logo_url: "/logo.svg",
      favicon_url: "/favicon.svg",
      default_theme: "system",
      show_ip_addresses: true,
      show_network_quality: true,
      updated_at: "",
    }
    vi.mocked(api.getMaintenanceOverview).mockResolvedValue(maintenanceOverview)
    vi.mocked(api.updateMaintenanceSettings).mockImplementation(async (settings) => settings)
    vi.mocked(api.startMaintenance).mockResolvedValue()
    vi.spyOn(window, "confirm").mockReturnValue(true)
  })

  it("shows readable values and saves every setting", async () => {
    vi.mocked(api.updateSiteSettings).mockImplementation(async (settings) => ({
      ...settings,
      updated_at: "2026-07-30T00:00:00Z",
    }))
    view()
    expect(screen.getAllByTestId("select")[0]).toHaveAttribute("data-selected-label", "Follow system")
    expect(screen.queryByText("system")).not.toBeInTheDocument()
    expect(document.querySelector('img[src="/logo.svg"]')).toBeInTheDocument()

    fireEvent.change(screen.getByLabelText("Site title"), { target: { value: "Operations" } })
    fireEvent.change(screen.getByLabelText("Logo URL"), { target: { value: "/brand.svg" } })
    fireEvent.click(screen.getAllByRole("switch")[0])
    fireEvent.click(screen.getAllByRole("button", { name: "Save" })[0])

    await waitFor(() => expect(api.updateSiteSettings).toHaveBeenCalledWith(
      expect.objectContaining({
        site_title: "Operations",
        logo_url: "/brand.svg",
        show_ip_addresses: false,
        default_theme: "system",
      }),
    ))
    expect(contextMocks.applySettings).toHaveBeenCalled()
    expect(screen.getByText("Site settings saved")).toBeInTheDocument()
  })

  it("shows loading, load, and save errors with a text fallback preview", async () => {
    siteState.loading = true
    siteState.error = "load failed"
    siteState.settings = { ...siteState.settings, logo_url: "" }
    const rendered = view()
    expect(screen.getByText("load failed")).toBeInTheDocument()
    expect(screen.getByText("Beat Monitor", { selector: "span" })).toBeInTheDocument()
    expect(screen.getAllByRole("button", { name: "Save" })[0]).toBeDisabled()

    siteState.loading = false
    siteState.error = null
    vi.mocked(api.updateSiteSettings).mockRejectedValue(new Error("save failed"))
    rendered.rerender(<LocaleProvider><Settings /></LocaleProvider>)
    fireEvent.click(screen.getAllByRole("button", { name: "Save" })[0])
    expect(await screen.findByText("save failed")).toBeInTheDocument()
  })

  it("shows readable maintenance values and starts a protected cleanup", async () => {
    view()
    expect(await screen.findByText("10 MiB")).toBeInTheDocument()
    expect(screen.getByText("Healthy")).toBeInTheDocument()
    expect(screen.getAllByTestId("select")[1]).toHaveAttribute("data-selected-label", "03:00 UTC")
    expect(screen.queryByText("3")).not.toBeInTheDocument()

    fireEvent.change(screen.getByLabelText("Metric retention"), { target: { value: "60" } })
    fireEvent.click(screen.getAllByRole("button", { name: "Save" })[1])
    await waitFor(() => expect(api.updateMaintenanceSettings).toHaveBeenCalledWith(
      expect.objectContaining({ retention_days: 60, cleanup_hour_utc: 3 }),
    ))

    fireEvent.click(screen.getByRole("button", { name: "Run maintenance" }))
    await waitFor(() => expect(api.startMaintenance).toHaveBeenCalled())
    expect(window.confirm).toHaveBeenCalled()
  })
})
