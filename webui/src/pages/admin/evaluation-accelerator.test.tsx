import { fireEvent, render, screen, waitFor } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { beforeEach, describe, expect, it, vi } from "vitest"
import { EvaluationAcceleratorScope } from "./evaluation-accelerator-scope"
import { EvaluationPreview } from "./evaluation-preview"
import { EvaluationDetailDialog } from "./evaluation-detail"
import { evaluationDraft, evaluationWrite } from "@/lib/alert-evaluation-form"
import { acceleratorAlertInputs, evaluationMetricChange, isAcceleratorAlertMetric } from "@/lib/alert-accelerator"
import { acceleratorCatalog, acceleratorDevice, acceleratorInventory } from "@/test/accelerator-fixtures"
import { workspaceNode } from "@/test/metric-workspace-fixtures"
import * as gpu from "@/lib/accelerator-api"
import * as evaluation from "@/lib/alert-evaluation-api"
vi.mock("@/context/auth", () => ({ useAuth: () => ({ principal: { user: { id: "owner" }, session: { id: "session" } } }) }))
vi.mock("@/context/locale", async () => {
  const { testLocaleValue } = await import("@/test/catalogs")
  return { useLocale: () => testLocaleValue("en"), useOptionalLocale: () => testLocaleValue("en") }
})
vi.mock("@/lib/accelerator-api", () => ({ getAcceleratorInventory: vi.fn() }))
vi.mock("@/lib/alert-evaluation-api", () => ({ previewEvaluation: vi.fn(), evaluationDetail: vi.fn(), evaluationDeviceDetail: vi.fn() }))
vi.mock("@/components/admin-select-field", () => ({ AdminSelectField: ({ id, label, value, options, onValueChange, disabled }: { id: string; label: string; value: string; options: { value: string; label: string }[]; onValueChange: (value: string) => void; disabled?: boolean }) => <label>{label}<select id={id} value={value} disabled={disabled} onChange={event => onValueChange(event.target.value)}>{options.map(option => <option key={option.value} value={option.value}>{option.label}</option>)}</select></label> }))
beforeEach(() => { vi.resetAllMocks(); vi.mocked(gpu.getAcceleratorInventory).mockResolvedValue(acceleratorInventory()); vi.mocked(evaluation.previewEvaluation).mockResolvedValue([]) })
describe("GPU alert device scope", () => {
 it("validates exact or any GPU scope and removes device binding for scalar metrics", () => {
  const draft = evaluationMetricChange({ ...evaluationDraft(), name: "GPU", scope: { kind: "nodes", node_ids: ["node-one", "node-two"] } }, "gpu_memory_percent")
  expect(draft.scope).toEqual({ kind: "nodes", node_ids: ["node-one"], device_id: "*" }); expect(evaluationWrite(draft).metric).toBe("gpu_memory_percent")
  expect(isAcceleratorAlertMetric("gpu_power_limit_percent")).toBe(true); expect(acceleratorAlertInputs("gpu_memory_percent")).toEqual(["gpu_memory_used_bytes", "gpu_memory_total_bytes"]); expect(acceleratorAlertInputs("gpu_power_limit_percent")).toHaveLength(2)
  expect(evaluationMetricChange(draft, "cpu").scope).not.toHaveProperty("device_id")
  expect(() => evaluationWrite({ ...draft, scope: { kind: "all" } })).toThrow("invalid"); expect(() => evaluationWrite({ ...draft, metric: "cpu" })).toThrow("invalid")
 })
 it("displays model labels while selecting device IDs and supports independent any-GPU evaluation", async () => {
  const draft = evaluationMetricChange({ ...evaluationDraft(), name: "GPU", scope: { kind: "nodes", node_ids: ["node-one"] } }, "gpu_utilization_percent"), change = vi.fn()
  render(<EvaluationAcceleratorScope draft={draft} sources={{ metrics: acceleratorCatalog(), nodes: [workspaceNode()], groups: [] }} locked={false} onChange={change} />)
  await waitFor(() => expect(screen.getByLabelText("GPU device")).not.toBeDisabled()); expect(screen.getByLabelText("GPU device")).toHaveDisplayValue("Any GPU (evaluated separately)")
  fireEvent.change(screen.getByLabelText("GPU device"), { target: { value: acceleratorDevice().device_id } }); expect(change).toHaveBeenCalledWith(expect.objectContaining({ scope: { kind: "nodes", node_ids: ["node-one"], device_id: acceleratorDevice().device_id } }))
  expect(screen.getByRole("option", { name: "GPU 1 · NVIDIA Test GPU" })).toBeInTheDocument()
 })
 it("shows an explicit zero-target preview instead of a normal evaluation", async () => {
  const draft = evaluationMetricChange({ ...evaluationDraft(), name: "GPU", scope: { kind: "nodes", node_ids: ["node-one"] } }, "gpu_utilization_percent")
  render(<EvaluationPreview draft={draft} nodes={[workspaceNode()]} />); await userEvent.click(screen.getByRole("checkbox", { name: "Production host" })); await userEvent.click(screen.getByRole("button", { name: "Preview" }))
  expect(await screen.findByText("No current evaluation targets")).toBeInTheDocument(); expect(evaluation.previewEvaluation).toHaveBeenCalledWith(expect.objectContaining({ scope: { kind: "nodes", node_ids: ["node-one"], device_id: "*" } }), ["node-one"], expect.any(AbortSignal))
 })
 it("binds the detail request to the selected device", async () => {
  vi.mocked(evaluation.evaluationDeviceDetail).mockRejectedValue(new Error("detail unavailable"))
  render(<EvaluationDetailDialog ruleID="rule" nodeID="node" deviceID={acceleratorDevice().device_id} onClose={vi.fn()} />)
  expect(await screen.findByText("detail unavailable")).toBeInTheDocument(); expect(evaluation.evaluationDetail).not.toHaveBeenCalled(); expect(evaluation.evaluationDeviceDetail).toHaveBeenCalledWith({ ruleID: "rule", nodeID: "node", deviceID: acceleratorDevice().device_id }, expect.any(AbortSignal))
 })
})
