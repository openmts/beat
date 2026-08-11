import type { ReactNode } from "react"
import { render, screen } from "@testing-library/react"
import { MemoryRouter } from "react-router"
import { describe, expect, it, vi } from "vitest"
import { PublicFooter } from "@/components/public-footer"
import { LocaleProvider } from "@/context/locale"

vi.mock("@/context/site-settings", () => ({
  useSiteSettings: () => ({
    settings: {
      site_title: "Beat Monitor",
      site_description: "Server monitoring and operations dashboard.",
      logo_url: "",
      show_network_quality: true,
    },
  }),
}))

function Providers({ children }: { children: ReactNode }) {
  return <MemoryRouter><LocaleProvider>{children}</LocaleProvider></MemoryRouter>
}

describe("PublicFooter", () => {
  it("renders site identity and admin link", () => {
    render(<Providers><PublicFooter /></Providers>)
    expect(screen.getByText(/Beat Monitor/)).toBeInTheDocument()
    expect(screen.getByRole("link", { name: "Admin" })).toHaveAttribute("href", "/admin")
  })
})
