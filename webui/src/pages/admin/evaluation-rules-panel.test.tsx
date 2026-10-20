import { fireEvent, render, screen, waitFor, within } from "@testing-library/react"
import { MemoryRouter } from "react-router"
import { beforeEach, expect, it, vi } from "vitest"
import { LocaleProvider } from "@/context/locale"
import EvaluationRulesPanel from "@/pages/admin/evaluation-rules-panel"
import * as api from "@/lib/alert-evaluation-api"
import { evaluationRuleFixture, evaluationSourcesFixture } from "@/test/alert-evaluation-fixtures"
vi.mock("@/lib/alert-evaluation-api")
beforeEach(() => { vi.resetAllMocks(); localStorage.setItem("locale", "en"); vi.mocked(api.evaluationRules).mockResolvedValue([evaluationRuleFixture()]); vi.mocked(api.evaluationSources).mockResolvedValue(evaluationSourcesFixture()) })
const view = () => render(<MemoryRouter><LocaleProvider><EvaluationRulesPanel /></LocaleProvider></MemoryRouter>)
it("toggles with the original revision and keeps delete confirmation on conflict", async () => {
  view(); await screen.findByText("CPU high")
  vi.mocked(api.saveEvaluationRule).mockRejectedValueOnce(new Error("stale_revision"))
  fireEvent.click(screen.getByRole("switch", { name: "CPU high Enabled" }))
  expect(await screen.findByText(/Your input is preserved/)).toBeInTheDocument()
  expect(api.saveEvaluationRule).toHaveBeenCalledWith(expect.objectContaining({ enabled: false }), expect.objectContaining({ revision: 3 }))
  fireEvent.click(screen.getByRole("button", { name: "Delete CPU high" }))
  vi.mocked(api.deleteEvaluationRule).mockRejectedValueOnce(new Error("stale_revision"))
  fireEvent.click(within(screen.getByRole("dialog")).getByRole("button", { name: "Delete" }))
  await waitFor(() => expect(api.deleteEvaluationRule).toHaveBeenCalledWith(expect.objectContaining({ revision: 3 })))
  expect(screen.getByRole("dialog")).toBeInTheDocument()
  vi.mocked(api.deleteEvaluationRule).mockResolvedValue()
  fireEvent.click(within(screen.getByRole("dialog")).getByRole("button", { name: "Delete" }))
  await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument())
})
it("opens create/edit, preserves locked state and displays load errors", async () => {
  vi.mocked(api.evaluationRules).mockResolvedValue([evaluationRuleFixture({ numeric_policy_locked: true })])
  view(); await screen.findByText("CPU high")
  fireEvent.click(screen.getByRole("button", { name: "Edit rule CPU high" }))
  expect(screen.getByText(/locks numeric policy/)).toBeInTheDocument()
  fireEvent.click(within(screen.getByRole("dialog")).getByRole("button", { name: "Cancel" }))
  fireEvent.click(screen.getByRole("button", { name: "Create rule" }))
  expect(screen.getByLabelText("Name")).toHaveValue("")
  fireEvent.click(within(screen.getByRole("dialog")).getByRole("button", { name: "Cancel" }))
  vi.mocked(api.evaluationRules).mockRejectedValueOnce(new Error("storage offline"))
  fireEvent.click(screen.getByRole("button", { name: "Refresh" }))
  expect(await screen.findByText("storage offline")).toBeInTheDocument()
})
it("opens a prefilled rule from a panel link and ignores unknown metrics", async () => {
  window.history.replaceState(null, "", "/admin/alerts?tab=rules&metric=cpu&aggregation=avg&scope=nodes&window=900&node_id=node-one&threshold=75&name=Node%20CPU%20high")
  try {
    view()
    const dialog = await screen.findByRole("dialog")
    expect(within(dialog).getByLabelText("Name")).toHaveValue("Node CPU high")
    expect(within(dialog).getByLabelText("Trigger threshold")).toHaveValue(75)
    expect(within(dialog).getByLabelText("Window (seconds; 0 for current)")).toHaveValue(900)
    expect(within(dialog).getByLabelText("Scope")).toHaveTextContent("Selected nodes")
    expect(within(dialog).getByText("Selected: 1 / 1000")).toBeInTheDocument()
    expect(api.saveEvaluationRule).not.toHaveBeenCalled()
  } finally { window.history.replaceState(null, "", "/") }
})

it("does not prefill a rule the catalog rejects", async () => {
  window.history.replaceState(null, "", "/admin/alerts?tab=rules&metric=not_in_catalog&aggregation=avg&scope=all")
  try {
    view()
    await screen.findByText("CPU high")
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument()
  } finally { window.history.replaceState(null, "", "/") }
})

it("shows empty data and supports a successful enable change", async () => {
  vi.mocked(api.evaluationRules).mockResolvedValueOnce([])
  view(); expect(await screen.findByText("No records")).toBeInTheDocument()
  fireEvent.click(screen.getByRole("button", { name: "Refresh" })); await screen.findByText("CPU high")
  vi.mocked(api.saveEvaluationRule).mockResolvedValue(evaluationRuleFixture({ enabled: false }))
  fireEvent.click(screen.getByRole("switch", { name: "CPU high Enabled" }))
  await waitFor(() => expect(api.saveEvaluationRule).toHaveBeenCalled())
})

it("duplicates a rule as a disabled copy", async () => {
  view(); await screen.findByText("CPU high")
  fireEvent.click(screen.getByRole("button", { name: "Duplicate rule CPU high" }))
  const dialog = await screen.findByRole("dialog")
  expect(within(dialog).getByLabelText("Name")).toHaveValue("CPU high (copy)")
  expect(within(dialog).getByLabelText("Trigger threshold")).toHaveValue(80)
  vi.mocked(api.saveEvaluationRule).mockResolvedValue(evaluationRuleFixture({ name: "CPU high (copy)", enabled: false }))
  fireEvent.click(within(dialog).getByRole("button", { name: "Save" }))
  await waitFor(() => expect(api.saveEvaluationRule).toHaveBeenCalledWith(
    expect.objectContaining({ name: "CPU high (copy)", enabled: false, metric: "cpu", threshold: 80 }), null))
})
