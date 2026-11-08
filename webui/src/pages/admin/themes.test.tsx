import { render, screen } from "@testing-library/react"
import { describe, expect, it, vi } from "vitest"
import { LocaleProvider } from "@/context/locale"
import Themes from "@/pages/admin/themes"

vi.mock("@/pages/admin/theme-panels", () => ({ ThemesPanel: () => <div>themes-panel</div> }))

describe("standalone theme management page", () => {
  it("renders the theme library outside site settings", () => {
    render(<LocaleProvider><Themes /></LocaleProvider>)
    expect(screen.getByRole("heading", { name: "Themes" })).toBeInTheDocument()
    expect(screen.getByText("Install, preview, configure and activate themes for the public dashboard and node pages.")).toBeInTheDocument()
    expect(screen.getByText("themes-panel")).toBeInTheDocument()
  })
})
