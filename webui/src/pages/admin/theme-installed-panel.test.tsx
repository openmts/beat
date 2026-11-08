import { fireEvent, render, screen, waitFor } from "@testing-library/react"
import { beforeEach, describe, expect, it, vi } from "vitest"
import { APIError } from "@/lib/api-error"
import { LocaleProvider } from "@/context/locale"
import { ThemesPanel } from "@/pages/admin/theme-panels"
import * as themes from "@/lib/theme-api"
import { readBundledTheme } from "@/lib/theme-bundled"
import { reauthenticateAdmin } from "@/lib/security-api"
import type { ThemePackage } from "@/types/theme"

vi.mock("@/lib/theme-api")
vi.mock("@/lib/theme-bundled", async (original) => ({ ...await original<object>(), readBundledTheme: vi.fn() }))
vi.mock("@/lib/security-api", () => ({ reauthenticateAdmin: vi.fn() }))
vi.mock("@/pages/admin/theme-preview", () => ({ ThemePreview: ({ theme }: { theme: ThemePackage }) => <p>Previewing {theme.name}</p> }))
const pkg = { id: "pkg", name: "Slate", version: "1.0.0", description: "Grouped public cards", author: "Beat", runtime: "declarative", preview_url: "/themes/assets/hash/preview.png" } as ThemePackage
function panel() { return render(<LocaleProvider><ThemesPanel /></LocaleProvider>) }

describe("installed theme operations", () => {
  beforeEach(() => {
    vi.resetAllMocks()
    vi.mocked(themes.listAdminThemes).mockResolvedValue([pkg])
  })
  it("shows the installed preview, bundled default and author guide", async () => {
    panel()
    await waitFor(() => expect(document.querySelector(`img[src="${pkg.preview_url}"]`)).not.toBeNull())
    expect(screen.getAllByRole("img", { name: "Slate preview" })).toHaveLength(1)
    expect(document.querySelector('img[src="/themes/default.preview.png"]')).not.toBeNull()
    expect(screen.getByRole("heading", { name: "Default" })).toBeInTheDocument()
    expect(screen.getByRole("link", { name: "Theme author guide" })).toHaveAttribute("href", expect.stringContaining("theme-author-guide.md"))
    expect(screen.getByRole("link", { name: "Download Default ZIP" })).toHaveAttribute("href", "/themes/default.theme.zip")
    fireEvent.click(screen.getByRole("button", { name: "Preview" }))
    expect(screen.getByText("Previewing Slate")).toBeInTheDocument()
    expect(themes.activateTheme).not.toHaveBeenCalled()
  })
  it("installs the bundled default without activating it", async () => {
    const archive = new Blob(["zip"])
    vi.mocked(readBundledTheme).mockResolvedValue(archive)
    panel()
    fireEvent.click((await screen.findAllByRole("button", { name: "Install" }))[0])
    await waitFor(() => expect(themes.uploadTheme).toHaveBeenCalledWith(archive))
    expect(readBundledTheme).toHaveBeenCalledWith("default")
    expect(themes.activateTheme).not.toHaveBeenCalled()
  })
  it("can cancel a destructive operation before any request", async () => {
    panel()
    fireEvent.click(await screen.findByRole("button", { name: "Delete" }))
    fireEvent.click(screen.getByRole("button", { name: "Cancel" }))
    expect(themes.deleteTheme).not.toHaveBeenCalled()
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument()
  })
  it("requests local proof only after 428 and retries the exact confirmed operation", async () => {
    vi.mocked(themes.activateTheme).mockRejectedValueOnce(new APIError("recent authentication is required", 428)).mockResolvedValue(pkg)
    panel()
    fireEvent.click(await screen.findByRole("button", { name: "Activate" }))
    fireEvent.click(screen.getByRole("button", { name: "Confirm" }))
    fireEvent.change(await screen.findByLabelText("Current password"), { target: { value: "owner proof" } })
    fireEvent.change(screen.getByLabelText("Authenticator code"), { target: { value: "123456" } })
    fireEvent.click(screen.getByRole("button", { name: "Verify and continue" }))
    await waitFor(() => expect(themes.activateTheme).toHaveBeenCalledTimes(2))
    expect(reauthenticateAdmin).toHaveBeenCalledWith("owner proof", "123456")
    await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument())
  })
  it("keeps a failed proof visible without repeating the mutation", async () => {
    vi.mocked(themes.deleteTheme).mockRejectedValueOnce(new APIError("recent authentication is required", 428))
    vi.mocked(reauthenticateAdmin).mockRejectedValue(new Error("Invalid credentials"))
    panel()
    fireEvent.click(await screen.findByRole("button", { name: "Delete" }))
    fireEvent.click(screen.getByRole("button", { name: "Confirm" }))
    fireEvent.change(await screen.findByLabelText("Current password"), { target: { value: "wrong proof" } })
    fireEvent.click(screen.getByRole("button", { name: "Verify and continue" }))
    expect(await screen.findByText("Invalid credentials")).toBeInTheDocument()
    expect(themes.deleteTheme).toHaveBeenCalledTimes(1)
  })
  it("offers explicit built-in recovery separately from rollback", async () => {
    vi.mocked(themes.listAdminThemes).mockResolvedValue([{ ...pkg, is_active: true }])
    panel()
    fireEvent.click(await screen.findByRole("button", { name: "Use built-in" }))
    fireEvent.click(screen.getByRole("button", { name: "Confirm" }))
    await waitFor(() => expect(themes.useBuiltInTheme).toHaveBeenCalledOnce())
    expect(themes.rollbackTheme).not.toHaveBeenCalled()
  })
  it("reports logical deletion with pending cleanup and supports a retry", async () => {
    vi.mocked(themes.deleteTheme).mockResolvedValueOnce({ package_id: pkg.id, state: "deleted", cleanup_pending: true }).mockResolvedValue(undefined)
    panel()
    fireEvent.click(await screen.findByRole("button", { name: "Delete" }))
    fireEvent.click(screen.getByRole("button", { name: "Confirm" }))
    expect(await screen.findByText(/The theme has been deleted and its assets are unavailable/)).toBeInTheDocument()
    fireEvent.click(screen.getByRole("button", { name: "Retry cleanup" }))
    await waitFor(() => expect(themes.deleteTheme).toHaveBeenCalledTimes(2))
    await waitFor(() => expect(screen.queryByRole("button", { name: "Retry cleanup" })).not.toBeInTheDocument())
  })

})
