import { act, fireEvent, render, screen, waitFor, within } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { MemoryRouter } from "react-router"
import { afterAll, beforeEach, describe, expect, it, vi } from "vitest"
import AxiosMockAdapter from "axios-mock-adapter"
import { api as client } from "@/lib/api-client"
import AcceleratorsPage from "./accelerators"
import { AcceleratorPolicyEditor } from "./accelerator-policy-editor"
import { AcceleratorEnrollmentPolicy } from "./accelerator-enrollment-policy"
import { AcceleratorDeviceField } from "@/components/metric-workspace/accelerator-device-field"
import { acceleratorDevice, acceleratorInventory, acceleratorPolicy } from "@/test/accelerator-fixtures"
import { workspaceNode } from "@/test/metric-workspace-fixtures"
import * as api from "@/lib/accelerator-api"
import type { NodeAcceleratorsAdmin } from "@/types/accelerator"
const auth = vi.hoisted(() => ({ principal: { user: { id: "owner", role: "owner" }, session: { id: "session", reauthenticated_until: "2099-01-01T00:00:00Z" } } }))
const siteState = vi.hoisted(() => ({ showAccelerators: true }))
vi.mock("@/context/auth", () => ({ useAuth: () => auth }))
vi.mock("@/context/site-settings", () => ({ useSiteSettings: () => ({ settings: { show_accelerators: siteState.showAccelerators } }) }))
vi.mock("@/context/locale", async () => {
  const { testLocaleValue } = await import("@/test/catalogs")
  return { useLocale: () => testLocaleValue("en"), useOptionalLocale: () => testLocaleValue("en") }
})
vi.mock("@/components/ui/switch", () => ({
  Switch: ({ checked, onCheckedChange, ...props }: {
    checked: boolean
    onCheckedChange?: (checked: boolean) => void
  }) => (
    <button
      type="button"
      role="switch"
      aria-checked={checked}
      onClick={() => onCheckedChange?.(!checked)}
      {...props}
    >toggle</button>
  ),
}))
const nodeAdapter = new AxiosMockAdapter(client)
afterAll(() => nodeAdapter.restore())
vi.mock("@/lib/accelerator-api", () => ({ getAcceleratorInventory: vi.fn(), getAcceleratorPolicy: vi.fn(), updateAcceleratorPolicy: vi.fn(), updateAcceleratorVisibility: vi.fn() }))
beforeEach(() => {
 nodeAdapter.reset()
 nodeAdapter.onGet("/nodes").reply(200, { code: 0, data: [workspaceNode("one")] })
 nodeAdapter.onGet("/nodes/manage").reply(200, { code: 0, data: [workspaceNode("one"), { ...workspaceNode("two"), name: "Second host", is_public: false }] })
 vi.resetAllMocks(); auth.principal.user.role = "owner"; auth.principal.session.id = "session"; siteState.showAccelerators = true; vi.mocked(api.getAcceleratorInventory).mockResolvedValue(acceleratorInventory()); vi.mocked(api.getAcceleratorPolicy).mockResolvedValue(acceleratorPolicy()); vi.mocked(api.updateAcceleratorPolicy).mockResolvedValue({ ...acceleratorPolicy(), configured: true, revision: 1 }); vi.mocked(api.updateAcceleratorVisibility).mockResolvedValue({ node_id: "node-one", visible: false }) })
async function selectNode(name = "Production host") { await userEvent.click(screen.getByRole("combobox", { name: "Node" })); await userEvent.click(await screen.findByRole("option", { name })) }
describe("GPU administrative collection and inventory", () => {
 it("selects a private node from the management API when the public API excludes it", async () => {
  render(<MemoryRouter><AcceleratorsPage /></MemoryRouter>)
  await selectNode("Second host")
  expect(await screen.findByText("Collection ready")).toBeInTheDocument()
  expect(screen.getByText("Private node")).toBeInTheDocument()
  expect(api.getAcceleratorInventory).toHaveBeenCalledWith("two", expect.any(AbortSignal))
  expect(nodeAdapter.history.get.map(request => request.url)).toEqual(["/nodes/manage"])
 })
 it("shows named inventory and its state while keeping opaque fingerprints out of labels", async () => {
  render(<MemoryRouter><AcceleratorsPage /></MemoryRouter>); await selectNode()
  expect(await screen.findByText("Collection ready")).toBeInTheDocument(); expect(screen.getByText("GPU 1 · NVIDIA Test GPU")).toBeInTheDocument(); expect(screen.queryByText("private fingerprint")).not.toBeInTheDocument(); expect(screen.getByText("Public node")).toBeInTheDocument()
  expect(api.getAcceleratorInventory).toHaveBeenCalledWith("one", expect.any(AbortSignal))
  await userEvent.click(screen.getByRole("tab", { name: "New-node defaults" })); expect(await screen.findByText("Default GPU policy for new nodes")).toBeInTheDocument(); expect(api.getAcceleratorPolicy).toHaveBeenCalledWith({ kind: "enrollment_policy" }, expect.any(AbortSignal))
 })
 it("saves explicit opt-in or disablement with recent confirmation and CAS", async () => {
  const saved = vi.fn(); render(<AcceleratorPolicyEditor value={{ ...acceleratorPolicy(), enabled: true, effective_source: "installation" }} scope={{ kind: "node", id: "node-one" }} saved={saved} />)
  await userEvent.click(screen.getByRole("switch", { name: "Enable GPU collection" })); fireEvent.change(screen.getByLabelText("Sample interval (seconds)"), { target: { value: "60" } }); fireEvent.change(screen.getByLabelText("Device limit (1–32)"), { target: { value: "2" } }); await userEvent.click(screen.getByRole("checkbox", { name: "GPU power limit" }))
  await userEvent.click(screen.getByRole("button", { name: "Save collection policy" })); expect(api.updateAcceleratorPolicy).not.toHaveBeenCalled(); await userEvent.click(screen.getByRole("button", { name: "Confirm" })); await waitFor(() => expect(saved).toHaveBeenCalledOnce())
  expect(api.updateAcceleratorPolicy).toHaveBeenCalledWith({ kind: "node", id: "node-one" }, expect.objectContaining({ enabled: false, sample_interval_seconds: 60, maximum_devices: 2, expected_revision: 0 }))
  expect(vi.mocked(api.updateAcceleratorPolicy).mock.calls[0][1].metrics).not.toContain("gpu_power_limit_watts")
 })
 it("shows conflicts and rejects invalid numeric policies before a request", async () => {
  vi.mocked(api.updateAcceleratorPolicy).mockRejectedValueOnce(new Error("policy conflict")); render(<AcceleratorPolicyEditor value={acceleratorPolicy()} scope={{ kind: "node", id: "node-one" }} saved={vi.fn()} />)
  fireEvent.change(screen.getByLabelText("Sample interval (seconds)"), { target: { value: "4" } }); fireEvent.submit(screen.getByRole("button", { name: "Save collection policy" }).closest("form")!); expect(screen.getByRole("alert")).toHaveTextContent("5 and 3600")
  fireEvent.change(screen.getByLabelText("Sample interval (seconds)"), { target: { value: "5" } }); await userEvent.click(screen.getByRole("button", { name: "Save collection policy" })); await userEvent.click(screen.getByRole("button", { name: "Confirm" })); expect(await screen.findByRole("alert")).toHaveTextContent("policy conflict")
 })
 it("saves node visibility while retaining it behind the global override", async () => {
  const view = render(<MemoryRouter><AcceleratorsPage /></MemoryRouter>); await selectNode()
  const visibility = await screen.findByRole("switch", { name: "Show this node in metric pages" })
  fireEvent.click(visibility)
  await waitFor(() => expect(api.updateAcceleratorVisibility).toHaveBeenCalledWith("one", false))
  siteState.showAccelerators = false
  view.rerender(<MemoryRouter><AcceleratorsPage /></MemoryRouter>)
  expect(await screen.findByText(/global switch is off/i)).toBeInTheDocument()
 })
 it("reports unavailable inventory and does not let a stale request cross node selection", async () => {
  let resolve!: (value: NodeAcceleratorsAdmin) => void; vi.mocked(api.getAcceleratorInventory).mockImplementationOnce(() => new Promise(done => { resolve = done })).mockResolvedValueOnce({ ...acceleratorInventory(), state: "unsupported", devices: [] })
  render(<MemoryRouter><AcceleratorsPage /></MemoryRouter>); await selectNode(); const firstSignal = vi.mocked(api.getAcceleratorInventory).mock.calls[0][1]; await selectNode("Second host"); expect(await screen.findByText("Unsupported hardware or platform")).toBeInTheDocument(); expect(firstSignal.aborted).toBe(true)
  await act(async () => resolve(acceleratorInventory())); expect(screen.queryByText("GPU 1 · NVIDIA Test GPU")).not.toBeInTheDocument(); expect(screen.getByText("Private node")).toBeInTheDocument()
  vi.mocked(api.getAcceleratorInventory).mockRejectedValueOnce(new Error("inventory unavailable")); await userEvent.click(screen.getByRole("button", { name: "Refresh" })); expect(await screen.findByRole("alert")).toHaveTextContent("inventory unavailable")
 })
 it("hides default-enrollment authority from ordinary admins and retries owner load failures", async () => {
  auth.principal.user.role = "admin"; const view = render(<AcceleratorEnrollmentPolicy />); expect(view.container).toBeEmptyDOMElement(); expect(api.getAcceleratorPolicy).not.toHaveBeenCalled()
  auth.principal.user.role = "owner"; vi.mocked(api.getAcceleratorPolicy).mockRejectedValueOnce(new Error("unavailable")); view.rerender(<AcceleratorEnrollmentPolicy />); expect(await screen.findByRole("alert")).toHaveTextContent("unavailable"); await userEvent.click(screen.getByRole("button", { name: "Retry" })); expect(await screen.findByRole("button", { name: "Save collection policy" })).toBeInTheDocument()
 })
 it("selects GPU devices by readable labels and requires a concrete node", async () => {
  const change = vi.fn(), device = acceleratorDevice(), series = { ref_id: "A", node_id: "one", metric: "gpu_utilization_percent", aggregation: "avg" as const, label: "", color: "blue" as const }
  const view = render(<AcceleratorDeviceField series={series} change={change} />); const selector = await screen.findByRole("combobox", { name: "GPU device A" }); await waitFor(() => expect(selector).not.toBeDisabled()); fireEvent.keyDown(selector, { key: "ArrowDown" }); await userEvent.click(await screen.findByRole("option", { name: "GPU 1 · NVIDIA Test GPU" })); expect(change).toHaveBeenCalledWith(device.device_id)
  view.rerender(<AcceleratorDeviceField series={{ ...series, node_id: "$node", device_id: device.device_id }} change={change} />); expect(screen.getByRole("status")).toHaveTextContent("Choose a specific node")
  expect(within(view.container).queryByText(device.device_id)).not.toBeInTheDocument()
 })
})
