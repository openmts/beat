import { render, screen } from "@testing-library/react"
import { MemoryRouter, Route, Routes } from "react-router"
import { afterEach, beforeEach, expect, it, vi } from "vitest"
import { LocaleProvider } from "@/context/locale"
import PublicFallback from "@/pages/not-found"

const payload = { data: { name: "Freeze", definition: { title: "Node overview", description: "", panels: [
  { id: "cpu", title: "CPU usage", type: "timeseries", size: "medium", series: [
    { ref_id: "A", node_id: "node-one", metric: "cpu", aggregation: "avg", label: "CPU" }] }] } } }

function view(path: string) {
  return render(<LocaleProvider><MemoryRouter initialEntries={[path]}><Routes>
    <Route path="/metrics/snapshots/:token" element={<PublicFallback />} />
    <Route path="*" element={<PublicFallback />} />
  </Routes></MemoryRouter></LocaleProvider>)
}

beforeEach(() => localStorage.setItem("locale", "en"))
afterEach(() => vi.unstubAllGlobals())

it("renders the frozen dashboard definition for a share token", async () => {
  const fetchMock = vi.fn().mockResolvedValue({ ok: true, json: () => Promise.resolve(payload) })
  vi.stubGlobal("fetch", fetchMock)
  view("/metrics/snapshots/token-value")
  expect(await screen.findByText("Freeze")).toBeInTheDocument()
  expect(screen.getByText("CPU usage")).toBeInTheDocument()
  expect(screen.getByText(/frozen read-only snapshot/)).toBeInTheDocument()
  expect(fetchMock).toHaveBeenCalledWith("/api/v1/metrics/snapshots/token-value", expect.objectContaining({ credentials: "same-origin" }))
})

it("explains that a revoked or expired snapshot is unavailable", async () => {
  vi.stubGlobal("fetch", vi.fn().mockResolvedValue({ ok: false, json: () => Promise.resolve({}) }))
  view("/metrics/snapshots/gone")
  expect(await screen.findByRole("alert")).toHaveTextContent("missing, expired or revoked")
})

it("still renders the 404 page for unknown paths", async () => {
  vi.stubGlobal("fetch", vi.fn())
  view("/nope")
  expect(await screen.findByText("404")).toBeInTheDocument()
})
