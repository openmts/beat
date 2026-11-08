import { render, screen } from "@testing-library/react"
import { describe, expect, it } from "vitest"
import { CountryBadge } from "@/components/country-badge"
import { LocaleProvider } from "@/context/locale"

describe("CountryBadge", () => {
  it("marks a valid public country without changing accessible names", () => {
    render(<LocaleProvider><CountryBadge code="JP" /></LocaleProvider>)
    const badge = screen.getByTitle("Japan")
    expect(badge).toHaveAttribute("data-country-code", "JP")
    expect(badge).toHaveAttribute("aria-hidden", "true")
    expect(badge).toHaveTextContent("JP")
  })

  it("renders nothing for missing or invalid codes", () => {
    const view = render(<LocaleProvider><CountryBadge code="jp" className="custom" /></LocaleProvider>)
    expect(view.container).toBeEmptyDOMElement()
  })
})
