import { fireEvent, render, screen, waitFor, within } from "@testing-library/react"
import { beforeEach, expect, it, vi } from "vitest"
import { LocaleProvider } from "@/context/locale"
import { EvaluationRuleEditor } from "@/pages/admin/evaluation-rule-editor"
import * as api from "@/lib/alert-evaluation-api"
import { evaluationRuleFixture, evaluationSourcesFixture } from "@/test/alert-evaluation-fixtures"

vi.mock("@/lib/alert-evaluation-api")
vi.mock("@/components/admin-select-field", () => ({ AdminSelectField: ({ id, label, value, options, disabled, onValueChange }: { id: string; label: string; value: string; options: Array<{ value: string; label: string }>; disabled?: boolean; onValueChange: (value: string) => void }) => <label>{label}<select id={id} value={value} disabled={disabled} onChange={event => onValueChange(event.target.value)}>{options.map(option => <option key={option.value} value={option.value}>{option.label}</option>)}</select></label> }))
beforeEach(() => { vi.resetAllMocks(); localStorage.setItem("locale", "en") })
const view = (rule = evaluationRuleFixture()) => { const onClose = vi.fn(), onSaved = vi.fn(); render(<LocaleProvider><EvaluationRuleEditor rule={rule} sources={evaluationSourcesFixture()} onClose={onClose} onSaved={onSaved} /></LocaleProvider>); return { onClose, onSaved } }

it("submits the complete policy and keeps edits on revision conflict", async () => {
  const { onClose, onSaved } = view()
  fireEvent.change(screen.getByLabelText("Name"), { target: { value: "Edited CPU" } })
  fireEvent.change(screen.getByLabelText("Description"), { target: { value: "Reviewed policy" } })
  fireEvent.change(screen.getByLabelText("Operator"), { target: { value: "gte" } })
  fireEvent.change(screen.getByLabelText("Window (seconds; 0 for current)"), { target: { value: "60" } })
  fireEvent.change(screen.getByLabelText("Aggregation"), { target: { value: "avg" } })
  fireEvent.change(screen.getByLabelText(/^Recovery threshold/), { target: { value: "70" } })
  fireEvent.change(screen.getByLabelText("Recovery confirmation (seconds)"), { target: { value: "15" } })
  fireEvent.change(screen.getByLabelText("Scope"), { target: { value: "nodes" } })
  fireEvent.click(screen.getByRole("checkbox", { name: "Alpha" }))
  fireEvent.change(screen.getByLabelText("NoData policy"), { target: { value: "notify" } })
  fireEvent.change(screen.getAllByLabelText("Notification delay (seconds)")[0], { target: { value: "120" } })
  fireEvent.change(screen.getByLabelText("Error policy"), { target: { value: "notify" } })
  fireEvent.change(screen.getAllByLabelText("Notification delay (seconds)")[1], { target: { value: "30" } })
  fireEvent.change(screen.getByLabelText("Severity"), { target: { value: "critical" } })
  vi.mocked(api.saveEvaluationRule).mockRejectedValueOnce(new Error("stale_revision"))
  fireEvent.click(screen.getByRole("button", { name: "Save" }))
  expect(await screen.findByText(/Your input is preserved/)).toBeInTheDocument()
  expect(screen.getByLabelText("Name")).toHaveValue("Edited CPU"); expect(onClose).not.toHaveBeenCalled()
  vi.mocked(api.saveEvaluationRule).mockResolvedValue(evaluationRuleFixture())
  fireEvent.click(screen.getByRole("button", { name: "Save" }))
  await waitFor(() => expect(onSaved).toHaveBeenCalled())
  expect(api.saveEvaluationRule).toHaveBeenLastCalledWith(expect.objectContaining({ name: "Edited CPU", operator: "gte", aggregation: "avg", window_seconds: 60, recovery_threshold: 70, recovery_duration: 15, scope: { kind: "nodes", node_ids: ["node-one"] }, no_data_policy: "notify", no_data_delay_seconds: 120, error_policy: "notify", error_delay_seconds: 30 }), expect.objectContaining({ revision: 3 }))
})
it("locks numeric policy while allowing independent scope and notification edits", () => {
  const { onClose } = view(evaluationRuleFixture({ numeric_policy_locked: true }))
  expect(screen.getByText(/locks numeric policy/)).toBeInTheDocument()
  for (const label of ["Metric", "Operator", "Trigger threshold", "Trigger confirmation (seconds)", "Window (seconds; 0 for current)", "Aggregation"]) expect(screen.getByLabelText(label)).toBeDisabled()
  expect(screen.getByLabelText("Scope")).toBeEnabled(); expect(screen.getByLabelText("NoData policy")).toBeEnabled()
  fireEvent.click(screen.getByRole("button", { name: "Cancel" })); expect(onClose).toHaveBeenCalled()
})
it("uses current-only operational metrics and supports group scopes without sending preview automatically", async () => {
  view()
  fireEvent.change(screen.getByLabelText("Metric"), { target: { value: "heartbeat_age_seconds" } })
  expect(screen.getByLabelText("Window (seconds; 0 for current)")).toBeDisabled()
  fireEvent.change(screen.getByLabelText("Scope"), { target: { value: "groups" } })
  fireEvent.click(screen.getByRole("checkbox", { name: "Production" }))
  const section = screen.getByText("Preview nodes (up to 8)").closest("section")!
  expect(within(section).queryByRole("checkbox", { name: "Beta" })).not.toBeInTheDocument()
  expect(api.previewEvaluation).not.toHaveBeenCalled()
  fireEvent.click(within(section).getByRole("checkbox", { name: "Alpha" }))
  vi.mocked(api.previewEvaluation).mockResolvedValue([{ node_id: "node-one", value: null, source_at: null, evaluation_status: "no_data", reason_code: "missing_value" }])
  fireEvent.click(screen.getByRole("button", { name: "Preview" }))
  expect(await screen.findByText("missing_value")).toBeInTheDocument(); expect(within(section).getByText("—")).toBeInTheDocument()
  expect(api.saveEvaluationRule).not.toHaveBeenCalled()
})
it("cancels an obsolete preview and reports read failures without publishing stale values", async () => {
  view()
  fireEvent.click(screen.getByRole("checkbox", { name: "Alpha" }))
  let finish!: (value: Awaited<ReturnType<typeof api.previewEvaluation>>) => void
  vi.mocked(api.previewEvaluation).mockImplementationOnce(() => new Promise(resolve => { finish = resolve }))
  fireEvent.click(screen.getByRole("button", { name: "Preview" }))
  const signal = vi.mocked(api.previewEvaluation).mock.calls[0][2]
  fireEvent.change(screen.getByLabelText("Name"), { target: { value: "New draft" } })
  expect(signal.aborted).toBe(true)
  finish([{ node_id: "node-one", value: 1234, source_at: null, evaluation_status: "ok", reason_code: "" }])
  await waitFor(() => expect(screen.queryByText("1234")).not.toBeInTheDocument())
  vi.mocked(api.previewEvaluation).mockRejectedValueOnce(new Error("capacity"))
  fireEvent.click(screen.getByRole("button", { name: "Preview" }))
  expect(await screen.findByText(/Capacity reached/)).toBeInTheDocument()
})
it("creates a rule with explicit submit and rejects empty numeric fields locally", async () => {
  const onSaved = vi.fn(), onClose = vi.fn()
  render(<LocaleProvider><EvaluationRuleEditor rule={null} sources={evaluationSourcesFixture()} onSaved={onSaved} onClose={onClose} /></LocaleProvider>)
  fireEvent.change(screen.getByLabelText("Name"), { target: { value: "New rule" } })
  fireEvent.change(screen.getByLabelText("Trigger threshold"), { target: { value: "" } })
  fireEvent.click(screen.getByRole("button", { name: "Save" }))
  expect(await screen.findByText(/Enter complete, valid/)).toBeInTheDocument(); expect(api.saveEvaluationRule).not.toHaveBeenCalled()
  fireEvent.change(screen.getByLabelText("Trigger threshold"), { target: { value: "90" } })
  fireEvent.change(screen.getByLabelText("Trigger confirmation (seconds)"), { target: { value: "0" } })
  fireEvent.click(screen.getByRole("switch"))
  vi.mocked(api.saveEvaluationRule).mockResolvedValue(evaluationRuleFixture())
  fireEvent.click(screen.getByRole("button", { name: "Save" }))
  await waitFor(() => expect(api.saveEvaluationRule).toHaveBeenCalledWith(expect.objectContaining({ enabled: false, threshold: 90, duration: 0 }), null))
})
