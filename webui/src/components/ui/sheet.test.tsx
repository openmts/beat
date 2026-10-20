import { render, screen } from "@testing-library/react"
import { describe, expect, it } from "vitest"
import { LocaleProvider } from "@/context/locale"
import { Sheet, SheetContent, SheetHeader, SheetTitle } from "@/components/ui/sheet"

function content(closeLabel?: string) {
  return (
    <Sheet open>
      <SheetContent closeLabel={closeLabel}>
        <SheetHeader><SheetTitle>Title</SheetTitle></SheetHeader>
      </SheetContent>
    </Sheet>
  )
}

describe("sheet close label", () => {
  it("takes the built-in close button label from the locale catalog", () => {
    render(<LocaleProvider>{content()}</LocaleProvider>)
    expect(screen.getByRole("button", { name: "Close" })).toBeInTheDocument()
  })

  it("prefers an explicit label and still renders without a locale provider", () => {
    const { unmount } = render(content("Dismiss"))
    expect(screen.getByRole("button", { name: "Dismiss" })).toBeInTheDocument()
    unmount()
    render(content())
    expect(screen.getByRole("button")).toBeInTheDocument()
  })
})
