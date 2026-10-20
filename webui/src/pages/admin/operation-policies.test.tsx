import MockAdapter from "axios-mock-adapter"
import { fireEvent, render, screen, waitFor, within } from "@testing-library/react"
import { afterEach, beforeEach, expect, it, vi } from "vitest"
import { LocaleProvider } from "@/context/locale"
import { api } from "@/lib/api-client"
import { operationNode, operationPolicy } from "@/test/operations-fixtures"
import { OperationPolicies } from "./operation-policies"
const auth = vi.hoisted(() => ({ principal: { user: { id: "owner", role: "owner", totp_enabled_at: null }, session: { reauthenticated_until: "2099-01-01T00:00:00Z" } } }))
vi.mock("@/context/auth", () => ({ useAuth: () => auth }))
const mock = new MockAdapter(api)
beforeEach(() => {
 localStorage.setItem("locale", "en"); auth.principal.user.role = "owner"; auth.principal.user.id = "owner"
 mock.onGet("/admin/operations/policies").reply(200, [operationPolicy()])
 mock.onGet("/nodes/manage").reply(200, [{ ...operationNode(), alias: "Named server" }])
 mock.onGet("/groups").reply(200, [{ id: "group-one", name: "Production group" }])
 mock.onGet("/admin/operations/status").reply(200, { enabled: true, scanner_configured: false, spool_configured: true, maximum_file_bytes: 2147483648, chunk_bytes: 524288 })
})
afterEach(() => mock.reset())
const view = () => render(<LocaleProvider><OperationPolicies /></LocaleProvider>)
const confirm = () => fireEvent.click(within(screen.getByRole("dialog")).getByRole("button", { name: "Confirm" }))
it("hides owner-only policy data and actions from admins", async () => {
 auth.principal.user.role = "admin"; const page = view()
 expect(screen.getByText("Only owners can manage execution policies.")).toBeInTheDocument(); expect(mock.history.get).toHaveLength(0)
 auth.principal.user.role = "owner"; page.rerender(<LocaleProvider><OperationPolicies /></LocaleProvider>); await screen.findByText("Production policy")
 fireEvent.click(screen.getByRole("button", { name: "Edit" })); expect(screen.getByLabelText("Name")).toHaveValue("Production policy")
 auth.principal.user.role = "admin"; page.rerender(<LocaleProvider><OperationPolicies /></LocaleProvider>); expect(screen.queryByLabelText("Name")).not.toBeInTheDocument()
})
it("saves the confirmed version and named scope with validated limits", async () => {
 view(); await screen.findByText("Production policy"); fireEvent.click(screen.getByRole("button", { name: "Edit" }))
 fireEvent.change(screen.getByLabelText("Name"), { target: { value: "Reviewed policy" } })
 for (const [name, value] of [["Maximum targets", "5"], ["Active task limit", "30"], ["Concurrency", "2"], ["Timeout seconds", "300"], ["Output bytes", "4096"]]) fireEvent.change(screen.getByLabelText(name), { target: { value } })
 fireEvent.click(screen.getByRole("checkbox", { name: "Named server" })); fireEvent.click(screen.getByRole("checkbox", { name: "Production group" }))
 fireEvent.click(screen.getByRole("checkbox", { name: "sh" })); fireEvent.click(screen.getByRole("checkbox", { name: "powershell" }))
 fireEvent.click(screen.getByRole("checkbox", { name: "Allow root requests" })); fireEvent.click(screen.getByRole("checkbox", { name: "Require file scanning" }))
 expect(screen.getByText(/No file scanner/)).toBeInTheDocument()
 fireEvent.change(screen.getByLabelText("Task records (days)"), { target: { value: "2" } }); fireEvent.change(screen.getByLabelText("Output (days)"), { target: { value: "0" } })
 fireEvent.click(screen.getByRole("button", { name: "Save" })); expect(mock.history.put).toHaveLength(0)
 mock.onPut("/admin/operations/policies/policy-one").reply(200, { id: "policy-one" }); confirm()
 await waitFor(() => expect(mock.history.put).toHaveLength(1))
 expect(JSON.parse(mock.history.put[0].data)).toMatchObject({ id: "policy-one", version: 1, name: "Reviewed policy", node_ids: [], group_ids: ["group-one"], policy: { maximum_targets: 5, maximum_active_tasks: 30, concurrency: 2, timeout_seconds: 300, output_bytes: 4096, allow_root: false, require_scan: true, supported_shells: ["sh", "powershell"], retention: { task_days: 2, output_days: 0 } } })
 await waitFor(() => expect(screen.queryByLabelText("Name")).not.toBeInTheDocument())
})
it("retains policy input on conflict and handles load/disable failures", async () => {
 view(); await screen.findByText("Production policy"); fireEvent.click(screen.getByRole("button", { name: "Edit" }))
 fireEvent.change(screen.getByLabelText("Name"), { target: { value: "Keep draft" } }); fireEvent.click(screen.getByRole("button", { name: "Save" }))
 mock.onPut("/admin/operations/policies/policy-one").reply(409, { message: "stale policy" }); confirm()
 expect(await screen.findByText(/Policy version changed/)).toBeInTheDocument(); expect(screen.getByLabelText("Name")).toHaveValue("Keep draft")
 fireEvent.click(within(screen.getByRole("dialog")).getByRole("button", { name: "Cancel" })); fireEvent.click(screen.getByRole("button", { name: "Cancel" }))
 fireEvent.click(screen.getByRole("button", { name: "Disable" })); mock.onPost("/admin/operations/policies/policy-one/disable").replyOnce(503, { message: "storage unavailable" }); confirm()
 expect(await screen.findByText("storage unavailable")).toBeInTheDocument(); mock.onPost("/admin/operations/policies/policy-one/disable").reply(200, {}); confirm(); await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument())
 await waitFor(() => expect(screen.getByRole("button", { name: "Refresh" })).not.toBeDisabled())
 mock.onGet("/admin/operations/policies").reply(503, { message: "read failed" }); fireEvent.click(screen.getByRole("button", { name: "Refresh" })); expect(await screen.findByText("read failed")).toBeInTheDocument()
})
it("does not confirm invalid numeric, root, retention or scope values", async () => {
 view(); await screen.findByText("Production policy"); fireEvent.click(screen.getByRole("button", { name: "New policy" }))
 fireEvent.change(screen.getByLabelText("Name"), { target: { value: "New owner policy" } }); fireEvent.click(screen.getByRole("checkbox", { name: "Named server" }))
 fireEvent.change(screen.getByLabelText("Concurrency"), { target: { value: "5" } }); expect(screen.getByRole("button", { name: "Save" })).toBeDisabled()
 fireEvent.change(screen.getByLabelText("Concurrency"), { target: { value: "" } }); expect(screen.getByLabelText("Concurrency")).toHaveValue(null)
 fireEvent.change(screen.getByLabelText("Concurrency"), { target: { value: "1" } }); fireEvent.click(screen.getByRole("button", { name: "Add root" })); expect(screen.getByRole("button", { name: "Save" })).toBeDisabled()
 fireEvent.change(screen.getByLabelText("Root ID"), { target: { value: "files" } }); fireEvent.change(screen.getByLabelText("Output (days)"), { target: { value: "31" } }); expect(screen.getByRole("button", { name: "Save" })).toBeDisabled()
 fireEvent.change(screen.getByLabelText("Output (days)"), { target: { value: "7" } }); mock.onPost("/admin/operations/policies").reply(200, { id: "new" }); fireEvent.click(screen.getByRole("button", { name: "Save" })); confirm()
 await waitFor(() => expect(mock.history.post).toHaveLength(1)); expect(JSON.parse(mock.history.post[0].data).version).toBe(0)
})
it("bounds combined scopes, searches by names and clears old-owner drafts", async () => {
 mock.onGet("/nodes/manage").reply(200, Array.from({ length: 201 }, (_, index) => ({ ...operationNode(), id: `node-${index}`, name: `Server ${index}` })))
 mock.onGet("/admin/operations/policies").reply(200, [{ ...operationPolicy(), node_ids: Array.from({ length: 200 }, (_, index) => `node-${index}`) }])
 const page = view(); await screen.findByText("Production policy"); fireEvent.click(screen.getByRole("button", { name: "Edit" }))
 expect(screen.getByText(/Showing the first 100/)).toBeInTheDocument(); expect(screen.getByRole("checkbox", { name: "Production group" })).toHaveAttribute("aria-disabled", "true")
 fireEvent.click(screen.getByRole("checkbox", { name: "Production group" })); expect(screen.getByText("Groups (0/0)")).toBeInTheDocument(); expect(screen.getByRole("checkbox", { name: "Production group" })).toHaveAttribute("aria-checked", "false")
 fireEvent.change(screen.getByLabelText("Nodes search"), { target: { value: "Server 200" } }); expect(screen.getByRole("checkbox", { name: "Server 200" })).toHaveAttribute("aria-disabled", "true")
 fireEvent.click(screen.getByRole("checkbox", { name: "Server 200" })); expect(screen.getByText("Nodes (200/200)")).toBeInTheDocument()
 fireEvent.change(screen.getByLabelText("Name"), { target: { value: "Old owner draft" } }); auth.principal.user.id = "new-owner"; page.rerender(<LocaleProvider><OperationPolicies /></LocaleProvider>)
 expect(screen.queryByLabelText("Name")).not.toBeInTheDocument(); auth.principal.user.id = "owner"
})
it("supports restrictive kind changes, disabled policies, null root metadata and empty states", async () => {
 mock.onGet("/admin/operations/policies").reply(200, [{ ...operationPolicy(), policy: { ...operationPolicy().policy, roots: null, kinds: ["shell", "snippet"], supported_shells: ["sh"] } }])
 view(); await screen.findByText("Production policy"); fireEvent.click(screen.getByRole("button", { name: "Edit" }))
 fireEvent.click(screen.getByRole("checkbox", { name: "Command" })); fireEvent.click(screen.getByRole("checkbox", { name: "sh" })); fireEvent.click(screen.getByRole("checkbox", { name: "Enabled" }))
 mock.onPut("/admin/operations/policies/policy-one").reply(200, { id: "policy-one" }); fireEvent.click(screen.getByRole("button", { name: "Save" })); confirm()
 await waitFor(() => expect(mock.history.put).toHaveLength(1)); expect(JSON.parse(mock.history.put[0].data).policy).toMatchObject({ kinds: ["snippet"], supported_shells: [], enabled: false, roots: [] })
 await screen.findByText("Production policy"); mock.onGet("/admin/operations/policies").reply(200, []); await waitFor(() => expect(screen.getByRole("button", { name: "Refresh" })).toBeEnabled()); fireEvent.click(screen.getByRole("button", { name: "Refresh" })); expect(await screen.findByText("No policies")).toBeInTheDocument()
})
