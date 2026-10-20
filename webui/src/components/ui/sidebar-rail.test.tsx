import { render, screen } from "@testing-library/react"
import { describe, expect, it } from "vitest"
import { LocaleProvider } from "@/context/locale"
import { SidebarProvider, SidebarRail, SidebarTrigger } from "@/components/ui/sidebar"

function rail(label?: string) {
  return (
    <SidebarProvider>
      <SidebarRail label={label} />
    </SidebarProvider>
  )
}

describe("sidebar rail label", () => {
  it("localizes the trigger label instead of hardcoding English", () => {
    render(<LocaleProvider><SidebarProvider><SidebarTrigger /></SidebarProvider></LocaleProvider>)
    expect(screen.getByRole("button", { name: "Toggle sidebar" })).toHaveAttribute("title", "Toggle sidebar")
  })

  it("takes the rail label from the locale catalog", () => {
    render(<LocaleProvider>{rail()}</LocaleProvider>)
    expect(screen.getByRole("button", { name: "Toggle sidebar" })).toHaveAttribute("title", "Toggle sidebar")
  })

  it("prefers an explicit label and still renders without a locale provider", () => {
    const { unmount } = render(rail("Collapse navigation"))
    expect(screen.getByRole("button", { name: "Collapse navigation" })).toHaveAttribute("title", "Collapse navigation")
    unmount()
    render(rail())
    expect(screen.getByRole("button")).toBeInTheDocument()
  })
})
