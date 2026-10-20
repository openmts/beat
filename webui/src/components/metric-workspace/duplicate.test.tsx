import { render, screen } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { describe, expect, it, vi } from "vitest"
import { WorkspaceDuplicate } from "./duplicate"
import { duplicateDashboardName } from "@/lib/workspace-duplicate"

vi.mock("@/context/locale", async () => {
  const { testLocaleValue } = await import("@/test/catalogs")
  return { useLocale: () => testLocaleValue("en"), useOptionalLocale: () => testLocaleValue("en") }
})

describe("dashboard duplicate dialog", () => {
  it("prefills a bounded copy name and confirms the trimmed value", async () => {
    const user = userEvent.setup(), confirm = vi.fn(), close = vi.fn()
    render(<WorkspaceDuplicate title="Fleet" close={close} confirm={confirm} />)
    const field = screen.getByLabelText("New dashboard name")
    expect(field).toHaveValue("Fleet copy")
    await user.clear(field)
    await user.type(field, "  Edge fleet  ")
    await user.click(screen.getByRole("button", { name: "Continue" }))
    expect(confirm).toHaveBeenCalledWith("Edge fleet")
    expect(close).toHaveBeenCalled()
  })

  it("blocks an empty name and closes without confirming", async () => {
    const user = userEvent.setup(), confirm = vi.fn(), close = vi.fn()
    render(<WorkspaceDuplicate title="Fleet" close={close} confirm={confirm} />)
    await user.clear(screen.getByLabelText("New dashboard name"))
    expect(screen.getByRole("button", { name: "Continue" })).toBeDisabled()
    await user.click(screen.getByRole("button", { name: "Cancel" }))
    expect(confirm).not.toHaveBeenCalled()
    expect(close).toHaveBeenCalled()
  })

  it("keeps the generated name inside the server title limit", () => {
    expect(duplicateDashboardName("", "copy")).toBe("copy")
    expect(duplicateDashboardName("x".repeat(200), "copy")).toHaveLength(120)
  })
})
