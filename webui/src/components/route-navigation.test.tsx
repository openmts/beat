import { act, render, screen } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { BrowserRouter, Link, Route, Routes } from "react-router"
import { afterEach, describe, expect, it, vi } from "vitest"
import { LazyRoute } from "./route-chunk"

vi.mock("@/context/locale", async () => {
  const { testLocaleValue } = await import("@/test/catalogs")
  return { useLocale: () => testLocaleValue("en"), useOptionalLocale: () => testLocaleValue("en") }
})
afterEach(() => { window.history.replaceState(null, "", "/"); vi.restoreAllMocks() })

function navigation(first: Parameters<typeof LazyRoute>[0]["load"], second: Parameters<typeof LazyRoute>[0]["load"]) {
  return <BrowserRouter><nav><Link to="/">First</Link><Link to="/second">Second</Link></nav><Routes>
    <Route path="/" element={<LazyRoute load={first} />} />
    <Route path="/second" element={<LazyRoute load={second} />} />
  </Routes></BrowserRouter>
}

describe("lazy route navigation", () => {
  it("renders linked pages and browser back/forward without a document reload", async () => {
    const first = vi.fn(async () => ({ default: () => <h1>First page</h1> }))
    const second = vi.fn(async () => ({ default: () => <h1>Second page</h1> }))
    render(navigation(first, second))
    expect(await screen.findByRole("heading", { name: "First page" })).toBeInTheDocument()
    await userEvent.click(screen.getByRole("link", { name: "Second" }))
    expect(await screen.findByRole("heading", { name: "Second page" })).toBeInTheDocument()
    expect(window.location.pathname).toBe("/second")
    await act(async () => { window.history.back() })
    expect(await screen.findByRole("heading", { name: "First page" })).toBeInTheDocument()
    await act(async () => { window.history.forward() })
    expect(await screen.findByRole("heading", { name: "Second page" })).toBeInTheDocument()
    expect(first).toHaveBeenCalledTimes(1)
    expect(second).toHaveBeenCalledTimes(1)
  })

  it("completes navigation after a delayed chunk resolves", async () => {
    let resolve!: (value: { default: () => React.JSX.Element }) => void
    const pending = new Promise<{ default: () => React.JSX.Element }>(done => { resolve = done })
    const second = vi.fn(() => pending)
    render(navigation(async () => ({ default: () => <h1>First page</h1> }), second))
    await screen.findByRole("heading", { name: "First page" })
    await userEvent.click(screen.getByRole("link", { name: "Second" }))
    expect(screen.queryByRole("heading", { name: "Second page" })).not.toBeInTheDocument()
    await act(async () => { resolve({ default: () => <h1>Second page</h1> }) })
    expect(await screen.findByRole("heading", { name: "Second page" })).toBeInTheDocument()
    expect(second).toHaveBeenCalledTimes(1)
  })

  it("leaves a failed page and allows an explicit failed-chunk retry", async () => {
    vi.spyOn(console, "error").mockImplementation(() => undefined)
    const first = vi.fn<Parameters<typeof LazyRoute>[0]["load"]>()
      .mockRejectedValueOnce(new Error("chunk unavailable"))
      .mockResolvedValue({ default: () => <h1>Recovered page</h1> })
    render(navigation(first, async () => ({ default: () => <h1>Second page</h1> })))
    await screen.findByRole("alert")
    await userEvent.click(screen.getByRole("link", { name: "Second" }))
    expect(await screen.findByRole("heading", { name: "Second page" })).toBeInTheDocument()
    await userEvent.click(screen.getByRole("link", { name: "First" }))
    await userEvent.click(await screen.findByRole("button", { name: "Retry loading" }))
    expect(await screen.findByRole("heading", { name: "Recovered page" })).toBeInTheDocument()
    expect(first).toHaveBeenCalledTimes(2)
  })
  it("renders the last clicked route while an earlier chunk is still pending", async () => {
    let resolveSecond!: (value: { default: () => React.JSX.Element }) => void
    const pending = new Promise<{ default: () => React.JSX.Element }>((done) => { resolveSecond = done })
    const second = vi.fn(() => pending)
    const navigation = <BrowserRouter><nav><Link to="/">First</Link><Link to="/second">Second</Link><Link to="/third">Third</Link></nav><Routes>
      <Route path="/" element={<LazyRoute load={async () => ({ default: () => <h1>First page</h1> })} />} />
      <Route path="/second" element={<LazyRoute load={second} />} />
      <Route path="/third" element={<LazyRoute load={async () => ({ default: () => <h1>Third page</h1> })} />} />
    </Routes></BrowserRouter>
    render(navigation)
    await screen.findByRole("heading", { name: "First page" })
    await userEvent.click(screen.getByRole("link", { name: "Second" }))
    await userEvent.click(screen.getByRole("link", { name: "Third" }))
    expect(await screen.findByRole("heading", { name: "Third page" })).toBeInTheDocument()
    expect(window.location.pathname).toBe("/third")
    await act(async () => { resolveSecond({ default: () => <h1>Second page</h1> }) })
  })
})
