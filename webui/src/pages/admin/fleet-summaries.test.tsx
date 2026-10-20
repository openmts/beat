import { emptyFleetSummary, validateFleetSummary } from "@/lib/fleet-summary-state"
import { fireEvent, render, screen, waitFor } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { beforeEach, describe, expect, it, vi } from "vitest"
import FleetSummariesPanel from "./fleet-summaries-panel"
import { FleetSummaryEditor } from "./fleet-summary-editor"
import { APIError } from "@/lib/api-error"
import * as api from "@/lib/fleet-summary-api"
import { workspaceNode } from "@/test/metric-workspace-fixtures"
import type { FleetSummarySchedule } from "@/types/fleet-summary"
vi.mock("@/context/locale", async () => {
  const { testLocaleValue } = await import("@/test/catalogs")
  return { useLocale: () => testLocaleValue("en"), useOptionalLocale: () => testLocaleValue("en") }
})
vi.mock("@/context/auth", () => ({ useAuth: () => ({ principal: { user: { id: "owner", role: "owner" }, session: { id: "session", reauthenticated_until: "2099-01-01T00:00:00Z" } } }) }))
vi.mock("@/hooks/use-api", () => ({ useNodes: () => ({ data: [workspaceNode()], loading: false }), useGroups: () => ({ data: [{ id: "group", name: "Production group" }], loading: false }), useAlertChannels: () => ({ data: [{ id: "channel", name: "Operations channel" }], loading: false }) }))
vi.mock("@/lib/fleet-summary-api", () => ({ listFleetSummaries: vi.fn(), createFleetSummary: vi.fn(), updateFleetSummary: vi.fn(), deleteFleetSummary: vi.fn(), testFleetSummary: vi.fn() }))
const schedule = (): FleetSummarySchedule => ({ ...emptyFleetSummary, id: "schedule", revision: 3, name: "Morning summary", channel_ids: ["channel"], next_run_at: "2026-10-02T08:00:00Z", last_result: "success", delivery_state: "delivered" })
beforeEach(() => { vi.clearAllMocks(); vi.mocked(api.listFleetSummaries).mockResolvedValue([]); vi.mocked(api.testFleetSummary).mockResolvedValue({ run_id: "run", status: "queued" }) })
describe("independent v23 status summaries", () => {
 it("shows explicit unavailable service instead of inventing fleet summary values", async () => {
  vi.mocked(api.listFleetSummaries).mockRejectedValueOnce(new APIError("unavailable", 404)); render(<FleetSummariesPanel onHistory={vi.fn()} />)
  expect(await screen.findByText(/Status summaries are not enabled/)).toBeInTheDocument(); expect(screen.queryByRole("button", { name: "Add status summary" })).not.toBeInTheDocument()
  await userEvent.click(screen.getByRole("button", { name: "Retry" })); expect(await screen.findByText("No summary schedules")).toBeInTheDocument(); expect(api.testFleetSummary).not.toHaveBeenCalled()
 })
 it("edits a schedule, sends only on confirmation and links queued delivery history", async () => {
  vi.mocked(api.listFleetSummaries).mockResolvedValue([schedule()]); const history = vi.fn(); render(<FleetSummariesPanel onHistory={history} />)
  await userEvent.click(await screen.findByRole("button", { name: "Edit" })); fireEvent.change(screen.getByLabelText("Name"), { target: { value: "Evening summary" } }); await userEvent.click(screen.getByRole("button", { name: "Save schedule" }))
  await waitFor(() => expect(api.updateFleetSummary).toHaveBeenCalledWith("schedule", expect.objectContaining({ name: "Evening summary", expected_revision: 3 }))); expect(vi.mocked(api.updateFleetSummary).mock.calls[0][1]).not.toHaveProperty("id")
  await userEvent.click(await screen.findByRole("button", { name: "Test send" })); expect(api.testFleetSummary).not.toHaveBeenCalled(); await userEvent.click(screen.getByRole("button", { name: "Confirm" })); expect(await screen.findByText("queued")).toBeInTheDocument(); await userEvent.click(screen.getByRole("button", { name: "View delivery history" })); expect(history).toHaveBeenCalledOnce()
  await userEvent.click(screen.getByRole("button", { name: "Delete" })); await userEvent.click(screen.getByRole("button", { name: "Confirm" })); await waitFor(() => expect(api.deleteFleetSummary).toHaveBeenCalledWith("schedule", 3))
 })
 it("creates daily/weekly schedules with names for scope selectors and bounded thresholds", async () => {
  const close = vi.fn(), saved = vi.fn(); render(<FleetSummaryEditor value={null} close={close} saved={saved} />)
  fireEvent.change(screen.getByLabelText("Name"), { target: { value: "Weekly summary" } }); fireEvent.change(screen.getByLabelText("Timezone"), { target: { value: "Asia/Shanghai" } }); fireEvent.change(screen.getByLabelText("Cadence"), { target: { value: "weekly" } }); fireEvent.change(screen.getByLabelText("Weekday"), { target: { value: "5" } }); fireEvent.change(screen.getByLabelText("Local time"), { target: { value: "20:00" } }); fireEvent.change(screen.getByLabelText("Name list threshold (0–50)"), { target: { value: "0" } })
  for (const label of ["Production host", "Production group", "Operations channel", "Enabled"]) await userEvent.click(screen.getByRole("checkbox", { name: label }))
  await userEvent.click(screen.getByRole("button", { name: "Save schedule" })); await waitFor(() => expect(saved).toHaveBeenCalledOnce()); expect(close).toHaveBeenCalledOnce()
  expect(api.createFleetSummary).toHaveBeenCalledWith(expect.objectContaining({ weekday: 5, cadence: "weekly", name_threshold: 0, node_ids: ["node-one"], group_ids: ["group"], channel_ids: ["channel"], enabled: false }))
  for (const input of [{ name: "" }, { local_time: "25:00" }, { name_threshold: 51 }, { cadence: "weekly" as const, weekday: null }, { timezone: "not-a-zone" }]) expect(() => validateFleetSummary({ ...schedule(), ...input })).toThrow()
 })
 it("surfaces API failures while preserving the edited draft", async () => {
  vi.mocked(api.createFleetSummary).mockRejectedValueOnce(new Error("conflict")); render(<FleetSummaryEditor value={null} close={vi.fn()} saved={vi.fn()} />)
  fireEvent.change(screen.getByLabelText("Name"), { target: { value: "Daily" } }); await userEvent.click(screen.getByRole("checkbox", { name: "Operations channel" })); await userEvent.click(screen.getByRole("button", { name: "Save schedule" })); expect(await screen.findByRole("alert")).toHaveTextContent("conflict"); expect(screen.getByLabelText("Name")).toHaveValue("Daily")
 })
})
