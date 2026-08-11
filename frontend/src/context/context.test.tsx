import { fireEvent, render, renderHook, screen, waitFor } from "@testing-library/react"
import { beforeEach, describe, expect, it, vi } from "vitest"
import { LocaleProvider, useLocale } from "@/context/locale"
import { ThemeProvider, useTheme } from "@/context/theme"
import { AuthProvider, useAuth } from "@/context/auth"
import * as security from "@/lib/security-api"

vi.mock("@/lib/security-api")

const principal = {
  user: { id: "user", username: "owner", display_name: "Owner", role: "owner" as const,
    enabled: true, password_changed_at: "", last_login_at: null, totp_enabled_at: null,
    created_at: "", updated_at: "" },
  session: { id: "session", user_id: "user", token_prefix: "prefix", created_at: "",
    last_activity_at: "", idle_expires_at: "", absolute_expires_at: "",
    reauthenticated_until: null, ip_address: "", user_agent: "", revoked_at: null },
}

function LocaleHarness() {
  const { locale, setLocale, t } = useLocale()
  return <button onClick={() => setLocale(locale === "en" ? "zh-CN" : "en")}>{t("app.dashboard")}</button>
}

function ThemeHarness() {
  const { theme, toggleTheme } = useTheme()
  return <button onClick={toggleTheme}>{theme}</button>
}

function AuthHarness() {
  const { authenticated, login, logout } = useAuth()
  return <div><span>{authenticated ? "yes" : "no"}</span>
    <button onClick={() => void login({ username: "owner", password: "password" }).catch(() => undefined)}>login</button>
    <button onClick={() => void logout()}>logout</button></div>
}

describe("application contexts", () => {
  beforeEach(() => {
    vi.resetAllMocks()
    vi.mocked(security.getAuthState).mockResolvedValue({ setup_required: false })
    vi.mocked(security.getAdminSession).mockRejectedValue(new Error("unauthorized"))
    vi.mocked(security.logoutAdmin).mockResolvedValue()
  })

  it("persists and translates locale", () => {
    render(<LocaleProvider><LocaleHarness /></LocaleProvider>)
    expect(screen.getByRole("button")).toHaveTextContent("Dashboard")
    fireEvent.click(screen.getByRole("button"))
    expect(screen.getByRole("button")).toHaveTextContent("仪表盘")
    expect(localStorage.getItem("locale")).toBe("zh-CN")
  })

  it("restores locale and rejects use outside provider", () => {
    localStorage.setItem("locale", "zh-CN")
    render(<LocaleProvider><LocaleHarness /></LocaleProvider>)
    expect(screen.getByRole("button")).toHaveTextContent("仪表盘")
    expect(() => renderHook(() => useLocale())).toThrow("LocaleProvider")
  })

  it("persists and applies theme", () => {
    localStorage.setItem("theme", "light")
    render(<ThemeProvider><ThemeHarness /></ThemeProvider>)
    expect(screen.getByRole("button")).toHaveTextContent("light")
    fireEvent.click(screen.getByRole("button"))
    expect(screen.getByRole("button")).toHaveTextContent("dark")
    expect(document.documentElement).toHaveClass("dark")
  })

  it("uses system theme and rejects use outside provider", () => {
    vi.spyOn(window, "matchMedia").mockReturnValue({ matches: true } as MediaQueryList)
    render(<ThemeProvider><ThemeHarness /></ThemeProvider>)
    expect(screen.getByRole("button")).toHaveTextContent("dark")
    expect(() => renderHook(() => useTheme())).toThrow("ThemeProvider")
  })

  it("uses and updates the server default until a visitor chooses", () => {
    const rendered = render(<ThemeProvider defaultTheme="dark"><ThemeHarness /></ThemeProvider>)
    expect(screen.getByRole("button")).toHaveTextContent("dark")
    rendered.rerender(<ThemeProvider defaultTheme="light"><ThemeHarness /></ThemeProvider>)
    expect(screen.getByRole("button")).toHaveTextContent("light")
  })

  it("logs in and out with a server session", async () => {
    vi.mocked(security.loginAdmin).mockResolvedValue(principal)
    render(<AuthProvider><AuthHarness /></AuthProvider>)
    await waitFor(() => expect(screen.getByText("no")).toBeInTheDocument())
    fireEvent.click(screen.getByText("login"))
    await waitFor(() => expect(screen.getByText("yes")).toBeInTheDocument())
    expect(security.loginAdmin).toHaveBeenCalledWith(expect.objectContaining({ username: "owner" }))
    fireEvent.click(screen.getByText("logout"))
    await waitFor(() => expect(screen.getByText("no")).toBeInTheDocument())
  })

  it("rejects auth use outside provider", () => {
    expect(() => renderHook(() => useAuth())).toThrow("AuthProvider")
  })
})
