import { fireEvent, render, screen, within } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { MemoryRouter } from "react-router"
import { beforeEach, describe, expect, it, vi } from "vitest"
import { FleetResults } from "./fleet-results"
import { FleetToolbar } from "./fleet-toolbar"
import { FleetSummary, FleetConnection } from "./fleet-summary"
import { FleetMultiFilter } from "./fleet-filters"
import { PublicNodeMetadata } from "@/components/public-node-metadata"
import { defaultFleetControls } from "@/lib/fleet-state"
import { workspaceNode } from "@/test/metric-workspace-fixtures"
import type { PublicFleetSnapshot } from "@/lib/public-fleet-live"
import type { Group } from "@/types"
vi.mock("@/context/locale", async () => {
  const { testLocaleValue } = await import("@/test/catalogs")
  return { useLocale: () => testLocaleValue("en"), useOptionalLocale: () => testLocaleValue("en") }
})
vi.mock("@/lib/theme-presentation", () => ({ usePublicTheme: () => null }))
vi.mock("@/components/node-card", () => ({ NodeCard: ({ node }: { node: { name: string } }) => <article>{node.name}</article> }))
vi.mock("@/components/telemetry-status", () => ({ TelemetryStatus: () => <span>Fresh</span> }))
const node = { ...workspaceNode("one"), name: "Visible host", tags: ["edge"], telemetry_state: "fresh" as const, metrics: { cpu: 10, cpu_used: 1, cpu_total: 4, memory: 20, memory_used: 1024, memory_total: 4096, disk: 30, disk_used: 2048, disk_total: 8192, net_sent: 100, net_recv: 200 }, location: { scope: "public", country_code: "JP" }, lifecycle: { status: "expired", expires_on: "2026-01-01", price_micros: "1234567", currency: "USD", interval_unit: "month", interval_count: 1 } }
const groups = [{ id: "group-one", name: "Production", sort_order: 0 }] as Group[]
beforeEach(() => vi.clearAllMocks())
describe("public fleet UI", () => {
 it("renders groups in both layouts and pauses ordering during pointer/focus interaction", () => {
  const interaction = vi.fn(), view = render(<MemoryRouter><FleetResults nodes={[node]} allNodes={[node]} groups={groups} layout="cards" onInteraction={interaction} /></MemoryRouter>)
  expect(screen.getByRole("heading", { name: "Production" })).toBeInTheDocument(); expect(screen.getByRole("article")).toHaveTextContent("Visible host")
  const results = document.getElementById("fleet-results")!; fireEvent.pointerOver(results); fireEvent.pointerLeave(results); fireEvent.focus(results); fireEvent.blur(results, { relatedTarget: document.body }); expect(interaction.mock.calls.map(call => call[0])).toEqual([true, false, true, false])
  view.rerender(<MemoryRouter><FleetResults nodes={[node, { ...workspaceNode("two"), status: "offline" }]} allNodes={[node]} groups={groups} layout="table" onInteraction={interaction} /></MemoryRouter>)
  expect(screen.getByRole("table")).toBeInTheDocument(); expect(screen.getByRole("link", { name: "Visible host" })).toHaveAttribute("href", "/node/one"); expect(screen.getByRole("link", { name: "Visible host" })).toHaveAttribute("title", "Visible host"); expect(screen.getAllByText("linux / amd64")[0]).toHaveAttribute("title", "linux / amd64"); expect(screen.getByText("Japan")).toBeInTheDocument(); expect(screen.getByText(/\$1.234567/)).toBeInTheDocument()
  view.rerender(<MemoryRouter><FleetResults nodes={[]} allNodes={[]} groups={[]} layout="table" onInteraction={interaction} /></MemoryRouter>); expect(screen.getByText(/No matching public nodes/)).toBeInTheDocument()
 })
 it("exposes bounded filters, clear and layout without a wallboard entry", async () => {
  const change = vi.fn(), view = render(<MemoryRouter><FleetToolbar value={{ ...defaultFleetControls, search: "query" }} change={change} nodes={[node]} groups={groups} count={1} /></MemoryRouter>)
  expect(screen.queryByLabelText("Status")).not.toBeInTheDocument()
  await userEvent.click(screen.getByRole("button", { name: "More filters" })); expect(screen.getByLabelText("Status")).toBeInTheDocument()
  await userEvent.click(screen.getByRole("button", { name: "Hide filters" })); expect(screen.queryByLabelText("Status")).not.toBeInTheDocument()
  fireEvent.change(screen.getByRole("textbox"), { target: { value: "edge" } }); expect(change).toHaveBeenCalledWith({ search: "edge" })
  await userEvent.click(screen.getByRole("button", { name: "Clear" })); expect(change).toHaveBeenCalledWith({ search: "" })
  await userEvent.click(screen.getByRole("button", { name: "Table" })); expect(change).toHaveBeenCalledWith({ layout: "table" }); await userEvent.click(screen.getByRole("button", { name: "Cards" })); expect(change).toHaveBeenCalledWith({ layout: "cards" })
  expect(screen.queryByRole("link", { name: "Wallboard" })).not.toBeInTheDocument()
  await userEvent.click(screen.getByRole("button", { name: /Groups: All/ })); await userEvent.click(screen.getByRole("checkbox", { name: "Production" })); expect(change).toHaveBeenCalledWith({ groups: ["group-one"] }); await userEvent.click(screen.getByRole("button", { name: "Done" }))
  view.rerender(<MemoryRouter><FleetToolbar value={{ ...defaultFleetControls, status: "online" }} change={change} nodes={[node]} groups={groups} count={1} /></MemoryRouter>); expect(screen.getByRole("button", { name: /More filters.*1 active/ })).toBeInTheDocument()
 })
 it("caps displayed options and selection while allowing refinement and reset", async () => {
  const change = vi.fn(), options = Array.from({ length: 110 }, (_, i) => ({ value: String(i), label: `Group ${i}` }))
  render(<FleetMultiFilter label="Groups" options={options} value={Array.from({ length: 16 }, (_, i) => String(i))} change={change} />)
  await userEvent.click(screen.getByRole("button")); expect(screen.getAllByRole("checkbox")).toHaveLength(100); expect(screen.getByRole("checkbox", { name: "Group 99" })).toHaveAttribute("aria-disabled", "true")
  fireEvent.change(screen.getByRole("textbox"), { target: { value: "Group 109" } }); expect(screen.getAllByRole("checkbox")).toHaveLength(1)
  await userEvent.click(screen.getByRole("button", { name: "Clear" })); expect(change).toHaveBeenCalledWith([])
 })
 it("labels partial rates and connection/offline/error states", async () => {
  const state: PublicFleetSnapshot = { nodes: [node], groups, loading: false, error: "", connected: true, offline: false, receivedAt: Date.now(), serverTime: Date.now(), stale: false }, retry = vi.fn()
  const view = render(<><FleetSummary nodes={[node, workspaceNode("missing")]} /><FleetConnection state={state} retry={retry} /></>)
  expect(screen.getAllByText(/Partial data \(1\/2\)/)).toHaveLength(2)
  for (const patch of [{ offline: true }, { error: "capacity" }, { error: "denied" }, { stale: true }, { nodes: [], error: "unavailable" }]) { view.rerender(<FleetConnection state={{ ...state, ...patch }} retry={retry} />); expect(screen.getByRole("alert")).toBeInTheDocument() }
  await userEvent.click(screen.getByRole("button", { name: "Retry" })); expect(retry).toHaveBeenCalledOnce()
  view.rerender(<FleetSummary nodes={[]} unavailable />); expect(screen.getAllByText("—")).toHaveLength(6)
 })
 it("keeps unknown countries and hidden lifecycle values neutral", () => {
  const view = render(<PublicNodeMetadata node={workspaceNode()} />); expect(view.container).toBeEmptyDOMElement()
  view.rerender(<PublicNodeMetadata node={{ ...workspaceNode(), location: { scope: "private" } }} />); expect(within(view.container).getByText(/Local/)).toBeInTheDocument()
 })
})
