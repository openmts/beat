import { render, screen, waitFor } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { afterEach, describe, expect, it, vi } from "vitest"
import { LazyRoute, RouteLoading } from "./route-chunk"
import { resetChunk, stableChunk } from "./route-chunk-cache"
vi.mock("@/context/locale", async () => {
  const { testLocaleValue } = await import("@/test/catalogs")
  return { useLocale: () => testLocaleValue("en"), useOptionalLocale: () => testLocaleValue("en") }
})
afterEach(() => vi.restoreAllMocks())
describe("lazy route error recovery", () => {
 it("loads routes and exposes accessible loading state", async () => {
  const first = render(<RouteLoading />); expect(screen.getByRole("status", { name: "Loading page" })).toBeInTheDocument(); first.unmount()
  render(<LazyRoute load={async () => ({ default: () => <main>Loaded route</main> })} />); expect(await screen.findByText("Loaded route")).toBeInTheDocument()
 })
 it("bounds retries without an automatic reload", async () => {
  vi.spyOn(console, "error").mockImplementation(() => undefined)
  const load = vi.fn(async () => { throw new Error("chunk failed") }); render(<LazyRoute load={load} />)
  await userEvent.click(await screen.findByRole("button", { name: "Retry loading" })); await userEvent.click(await screen.findByRole("button", { name: "Retry loading" })); expect(await screen.findByRole("button", { name: "Reload page" })).toBeInTheDocument(); expect(load).toHaveBeenCalledTimes(3)
 })
 it("recovers a loading state that never settles", async () => {
  const reload = vi.fn()
  render(<RouteLoading recovery={{ graceMs: 5, storage: null, reload, now: () => 150_000_000 }} />)
  expect(screen.getByRole("status", { name: "Loading page" })).toBeInTheDocument()
  await waitFor(() => expect(reload).toHaveBeenCalledTimes(1))
 })
 it("cancels the loading watchdog when the route commits", async () => {
  const reload = vi.fn()
  const view = render(<RouteLoading recovery={{ graceMs: 20, storage: null, reload, now: () => 160_000_000 }} />)
  await waitFor(() => expect(screen.getByRole("status", { name: "Loading page" })).toBeInTheDocument())
  view.unmount()
  await new Promise(resolve => setTimeout(resolve, 80))
  expect(reload).not.toHaveBeenCalled()
 })
 it("keeps one lazy identity per loader and replaces it only on explicit reset", () => {
  const load = vi.fn(async () => ({ default: () => <p>Stable page</p> }))
  const original = stableChunk(load)
  expect(stableChunk(load)).toBe(original)
  resetChunk(load)
  expect(stableChunk(load)).not.toBe(original)
 })
 it("does not re-import a chunk when its consumer remounts", async () => {
  const load = vi.fn(async () => ({ default: () => <p>Remount stable</p> }))
  const view = render(<LazyRoute load={load} />)
  expect(await screen.findByText("Remount stable")).toBeInTheDocument()
  view.unmount()
  render(<LazyRoute load={load} />)
  expect(await screen.findByText("Remount stable")).toBeInTheDocument()
  expect(load).toHaveBeenCalledTimes(1)
 })
 it("shares one lazy identity for inline loaders that import the same module", () => {
  const first = stableChunk(() => import("@/pages/not-found"))
  const second = stableChunk(() => import("@/pages/not-found"))
  expect(second).toBe(first)
 })
 it("keeps loaders that are not literal imports separate", () => {
  const first = stableChunk(() => Promise.resolve({ default: () => <p>Plain</p> }))
  const second = stableChunk(() => Promise.resolve({ default: () => <p>Plain</p> }))
  expect(second).not.toBe(first)
 })
 it("drops the shared module entry when a chunk is reset", () => {
  const inline = () => import("@/pages/not-found")
  const original = stableChunk(inline)
  expect(stableChunk(() => import("@/pages/not-found"))).toBe(original)
  resetChunk(inline)
  expect(stableChunk(() => import("@/pages/not-found"))).not.toBe(original)
 })
})
