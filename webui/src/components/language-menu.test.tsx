import { render, screen, waitFor } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { beforeEach, describe, expect, it, vi } from "vitest"
import { LanguageMenu } from "./language-menu"
import { LocaleProvider } from "@/context/locale"

function renderMenu() {
  return render(
    <LocaleProvider>
      <LanguageMenu />
    </LocaleProvider>,
  )
}

describe("language menu", () => {
  beforeEach(() => {
    localStorage.clear()
  })

  it("lists every shipped locale with its native name and short code", async () => {
    const user = userEvent.setup()
    renderMenu()
    await user.click(screen.getByRole("button", { name: "Language · English" }))
    const english = await screen.findByRole("menuitem", { name: /EnglishEN/ })
    expect(english).toHaveAttribute("aria-current", "true")
    expect(screen.getByRole("menuitem", { name: /简体中文简/ })).toBeInTheDocument()
    expect(screen.getByRole("menuitem", { name: /繁體中文繁/ })).toBeInTheDocument()
    expect(screen.getByRole("menuitem", { name: /日本語JA/ })).toBeInTheDocument()
    expect(screen.getByRole("menuitem", { name: /Bahasa IndonesiaID/ })).toBeInTheDocument()
  })

  it("switches the visible locale without reloading the document", async () => {
    const user = userEvent.setup()
    renderMenu()
    await user.click(screen.getByRole("button", { name: "Language · English" }))
    await user.click(await screen.findByRole("menuitem", { name: /简体中文简/ }))
    await waitFor(() => expect(localStorage.getItem("locale")).toBe("zh-CN"))
    const switched = await screen.findByRole("button", { name: "语言 · 简体中文" })
    expect(switched).toBeInTheDocument()
    await user.click(switched)
    expect(await screen.findByRole("menuitem", { name: /简体中文简/ })).toHaveAttribute("aria-current", "true")
  })

  it("renders the locale stored by a previous visit", async () => {
    localStorage.setItem("locale", "zh-CN")
    renderMenu()
    expect(await screen.findByRole("button", { name: "语言 · 简体中文" })).toBeInTheDocument()
  })

  it("keeps rendering when storage is denied", async () => {
    const getItem = vi.spyOn(Storage.prototype, "getItem").mockImplementation(() => { throw new Error("denied") })
    const setItem = vi.spyOn(Storage.prototype, "setItem").mockImplementation(() => { throw new Error("denied") })
    const user = userEvent.setup()
    renderMenu()
    await user.click(screen.getByRole("button", { name: "Language · English" }))
    await user.click(await screen.findByRole("menuitem", { name: /简体中文简/ }))
    expect(await screen.findByRole("button", { name: "语言 · 简体中文" })).toBeInTheDocument()
    getItem.mockRestore()
    setItem.mockRestore()
  })
})
