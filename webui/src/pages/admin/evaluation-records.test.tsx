import { fireEvent, render, screen, waitFor, within } from "@testing-library/react"
import { MemoryRouter } from "react-router"
import { beforeEach, expect, it, vi } from "vitest"
import { LocaleProvider } from "@/context/locale"
import { EvaluationRecords } from "@/pages/admin/evaluation-records"
import { EvaluationDetailDialog } from "@/pages/admin/evaluation-detail"
import { evaluationTime } from "@/lib/alert-evaluation-format"
import * as api from "@/lib/alert-evaluation-api"
import { evaluationDetailFixture, evaluationRuleFixture, evaluationSourcesFixture, evaluationStateFixture } from "@/test/alert-evaluation-fixtures"
import { notificationReceipt } from "@/test/notification-fixtures"
vi.mock("@/lib/alert-evaluation-api")
vi.mock("@/pages/admin/notification-delivery-detail", () => ({ NotificationDeliveryDetail: ({ id, close }: { id: string; close: () => void }) => <div role="dialog"><p>{id}</p><button onClick={close}>Back to evaluation</button></div> }))
vi.mock("@/components/admin-select-field", () => ({ AdminSelectField: ({ id, label, value, options, onValueChange }: { id: string; label: string; value: string; options: Array<{ value: string; label: string }>; onValueChange: (value: string) => void }) => <label>{label}<select id={id} value={value} onChange={event => onValueChange(event.target.value)}>{options.map(option => <option key={option.value} value={option.value}>{option.label}</option>)}</select></label> }))
beforeEach(() => { vi.resetAllMocks(); localStorage.setItem("locale", "en"); vi.mocked(api.evaluationRules).mockResolvedValue([evaluationRuleFixture()]); vi.mocked(api.evaluationSources).mockResolvedValue(evaluationSourcesFixture()); vi.mocked(api.evaluationDetail).mockResolvedValue(evaluationDetailFixture()) })
const view = (mode: "states" | "history" = "states") => render(<MemoryRouter><LocaleProvider><EvaluationRecords mode={mode} /></LocaleProvider></MemoryRouter>)
it("keeps threshold phase and evaluation health independent and resets cursor when filters change", async () => {
  vi.mocked(api.evaluationStates).mockResolvedValue({ items: [evaluationStateFixture()], next_cursor: "page-two" })
  view(); await screen.findByText("stale_value")
  const row = screen.getByRole("cell", { name: "Alpha" }).closest("tr")!
  expect(within(row).getByText("Firing")).toBeInTheDocument(); expect(within(row).getByText("NoData")).toBeInTheDocument(); expect(within(row).getByText("Trigger: 15s")).toBeInTheDocument()
  fireEvent.click(screen.getByRole("button", { name: "Next" }))
  await waitFor(() => expect(api.evaluationStates).toHaveBeenLastCalledWith(expect.stringContaining("cursor=page-two"), expect.any(AbortSignal)))
  fireEvent.click(screen.getByRole("button", { name: "Previous" }))
  await waitFor(() => expect(api.evaluationStates).toHaveBeenLastCalledWith(expect.stringContaining("cursor=&"), expect.any(AbortSignal)))
  fireEvent.change(screen.getByLabelText("Evaluation status"), { target: { value: "error" } })
  await waitFor(() => expect(api.evaluationStates).toHaveBeenLastCalledWith(expect.stringContaining("status=error"), expect.any(AbortSignal)))
  expect(screen.getByRole("button", { name: "Previous" })).toBeDisabled()
  fireEvent.click(screen.getByRole("button", { name: "Details" }))
  expect(await screen.findByText("Acknowledged incidents can only be resolved manually.")).toBeInTheDocument()
  expect(await screen.findByText("Notification history unavailable")).toBeInTheDocument()
})
it("reads health history with explicit kind filters and keeps failures visible", async () => {
  vi.mocked(api.evaluationEvents).mockResolvedValue({ items: [{ id: "health-one", rule_id: "rule-one", node_id: "node-one", rule_revision: 3, epoch_id: "epoch", kind: "error", reason_code: "query_timeout", occurred_at: "2026-10-07T12:00:00Z", notified_at: null, closed_at: null, source_key: "source" }] })
  view("history"); await screen.findByText("query_timeout")
  fireEvent.change(screen.getByLabelText("Kind"), { target: { value: "recovered" } })
  await waitFor(() => expect(api.evaluationEvents).toHaveBeenLastCalledWith(expect.stringContaining("kind=recovered"), expect.any(AbortSignal)))
  vi.mocked(api.evaluationEvents).mockRejectedValueOnce(new Error("history offline"))
  fireEvent.click(screen.getByRole("button", { name: "Refresh" }))
  expect(await screen.findByText("history offline")).toBeInTheDocument()
})
it("shows empty pages and missing references without using zero as evidence", async () => {
  vi.mocked(api.evaluationStates).mockResolvedValue({ items: [] })
  view(); expect(await screen.findByText("No records")).toBeInTheDocument(); expect(screen.getByRole("button", { name: "Next" })).toBeDisabled()
  vi.mocked(api.evaluationStates).mockResolvedValue({ items: [evaluationStateFixture({ rule_id: "removed", node_id: "removed-node", evaluation_status: "error", last_good_at: null })] })
  fireEvent.click(screen.getByRole("button", { name: "Refresh" }))
  expect(await screen.findByText("removed-node")).toBeInTheDocument(); expect(screen.getByText("removed")).toBeInTheDocument()
  expect(evaluationTime(null)).toBe("—"); expect(evaluationTime("invalid")).toBe("—")
})
it("renders detailed delivered and health event links, reload errors and absent history", async () => {
  const detail = evaluationDetailFixture()
  detail.notifications = [{ source_key: "source", status: "not_routed", receipt: notificationReceipt({ deliveries: [{ id: "delivery", channel_id: "channel", channel_revision: 1, state: "pending", attempt_count: 1 }] }) }]
  detail.recent_health_events = [{ id: "health", rule_id: "rule-one", node_id: "node-one", rule_revision: 3, epoch_id: "epoch", kind: "recovered", reason_code: "usable_evidence", occurred_at: "2026-10-07T12:00:00Z", notified_at: null, closed_at: null, source_key: "health-source" }]
  vi.mocked(api.evaluationDetail).mockResolvedValue(detail)
  const close = vi.fn(), incident = vi.fn(); render(<LocaleProvider><EvaluationDetailDialog ruleID="rule-one" nodeID="node-one" onClose={close} onIncident={incident} /></LocaleProvider>)
  expect(await screen.findByText("No subscribed channel")).toBeInTheDocument(); expect(screen.getByText(/usable_evidence/)).toBeInTheDocument(); expect(screen.getByText(/channel · Queued/)).toBeInTheDocument()
  fireEvent.click(screen.getByRole("button", { name: "View incident history" })); expect(incident).toHaveBeenCalled()
  fireEvent.click(screen.getByRole("button", { name: "Details" })); expect(screen.getByText("delivery")).toBeInTheDocument()
  fireEvent.click(screen.getByRole("button", { name: "Back to evaluation" })); expect(screen.queryByText("delivery")).not.toBeInTheDocument()
  vi.mocked(api.evaluationDetail).mockRejectedValueOnce(new Error("detail offline"))
  fireEvent.click(screen.getByRole("button", { name: "Refresh" })); expect(await screen.findByText("detail offline")).toBeInTheDocument()
  vi.mocked(api.evaluationDetail).mockResolvedValue({ ...detail, rule: null, window: null, notifications: [], state: evaluationStateFixture({ active_event_status: "", evidence_id: "", reason_code: "", health: { ...detail.state.health, epoch_id: "" } }) })
  fireEvent.click(screen.getByRole("button", { name: "Refresh" })); expect(await screen.findByText("No records")).toBeInTheDocument()
  fireEvent.click(screen.getByRole("button", { name: "Close" })); expect(close).toHaveBeenCalled()
})
