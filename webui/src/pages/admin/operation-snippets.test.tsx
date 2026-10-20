import MockAdapter from "axios-mock-adapter"
import { act, fireEvent, render, screen, waitFor, within } from "@testing-library/react"
import { afterEach, beforeEach, expect, it, vi } from "vitest"
import { LocaleProvider } from "@/context/locale"
import { api } from "@/lib/api-client"
import { operationSnippet } from "@/test/operations-fixtures"
import { OperationSnippets } from "./operation-snippets"
const auth = vi.hoisted(() => ({ principal: { user: { id: "owner", role: "owner", totp_enabled_at: null }, session: { reauthenticated_until: "2099-01-01T00:00:00Z" } } }))
vi.mock("@/context/auth", () => ({ useAuth: () => auth }))
vi.mock("@/components/ui/select", async () => { const m = await vi.importActual<typeof import("@/test/admin-ui-mocks")>("@/test/admin-ui-mocks"); return { Select: m.MockSelect, SelectContent: m.PassThrough, SelectItem: m.MockSelectItem, SelectTrigger: () => null, SelectValue: () => null } })
const mock = new MockAdapter(api)
beforeEach(() => { localStorage.setItem("locale", "en"); auth.principal.user.id = "owner"; auth.principal.user.role = "owner"; auth.principal.session.reauthenticated_until = "2099-01-01T00:00:00Z"; mock.onGet("/admin/operations/snippets").reply(200, [operationSnippet()]); mock.onGet("/admin/operations/snippets/snippet-one").reply(200, { content: "printf private-content" }) })
afterEach(() => mock.reset())
const view = () => { const execute = vi.fn(); return { ...render(<LocaleProvider><OperationSnippets execute={execute} /></LocaleProvider>), execute } }
const confirm = () => fireEvent.click(within(screen.getByRole("dialog")).getByRole("button", { name: "Confirm" }))
const read = async () => { const previous = mock.history.get.filter(request => request.url?.endsWith("/snippet-one")).length; await screen.findByText("Inspect host"); fireEvent.click(screen.getByRole("button", { name: "View / edit" })); expect(mock.history.get.filter(request => request.url?.endsWith("/snippet-one"))).toHaveLength(previous); confirm(); await screen.findByLabelText("Content") }
it("loads exact-version content only on confirmation and preserves drafts on CAS failure", async () => {
 view(); await read()
 expect(mock.history.get.find(request => request.url?.endsWith("/snippet-one"))?.params).toEqual({ version: 2 })
 fireEvent.change(screen.getByLabelText("Snippet name"), { target: { value: "Edited snippet" } }); fireEvent.change(screen.getByLabelText("Description"), { target: { value: "New description" } })
 fireEvent.change(screen.getByTestId("select"), { target: { value: "powershell" } }); fireEvent.click(screen.getByRole("checkbox", { name: "Share with administrators" }))
 fireEvent.change(screen.getByLabelText("Content"), { target: { value: "Write-Output edited" } }); fireEvent.click(screen.getByRole("button", { name: "Save version" }))
 mock.onPut("/admin/operations/snippets/snippet-one").reply(409, { message: "snippet version changed" }); confirm()
 expect(await screen.findByText(/Snippet version changed.*draft is preserved/)).toBeInTheDocument(); expect(screen.getByLabelText("Content")).toHaveValue("Write-Output edited")
 expect(JSON.parse(mock.history.put[0].data)).toEqual({ id: "snippet-one", name: "Edited snippet", description: "New description", visibility: "private", shell_family: "powershell", expected_version: 2, content: btoa("Write-Output edited") })
 fireEvent.click(within(screen.getByRole("dialog")).getByRole("button", { name: "Cancel" })); fireEvent.click(screen.getByRole("button", { name: "Cancel and clear content" }))
 expect(screen.queryByLabelText("Content")).not.toBeInTheDocument(); expect(localStorage.getItem("content")).toBeNull()
})
it("uses author metadata even for global owners and approves only shared current versions", async () => {
 const snippet = { ...operationSnippet(), owner_id: "someone-else", approved: false }
 mock.onGet("/admin/operations/snippets").reply(200, [snippet]); view(); await screen.findByText("Inspect host")
 expect(screen.queryByRole("button", { name: "Delete" })).not.toBeInTheDocument(); expect(screen.getByRole("button", { name: "Dispatch" })).toBeDisabled()
 fireEvent.click(screen.getByRole("button", { name: "View" })); confirm(); await screen.findByLabelText("Content")
 expect(screen.getByLabelText("Content")).toHaveAttribute("readonly"); expect(screen.getByLabelText("Snippet name")).toHaveAttribute("readonly"); expect(screen.queryByRole("button", { name: "Save version" })).not.toBeInTheDocument(); expect(screen.getByTestId("select")).toBeDisabled()
 fireEvent.click(screen.getByRole("button", { name: "Cancel and clear content" })); fireEvent.click(screen.getByRole("button", { name: "Approve" }))
 mock.onPost("/admin/operations/snippets/snippet-one/approve").reply(409, { message: "approval version changed" }); confirm()
 expect(await screen.findByText(/Reload and review the current shared version/)).toBeInTheDocument(); expect(JSON.parse(mock.history.post[0].data)).toEqual({ version: 2 })
 fireEvent.click(within(screen.getByRole("dialog")).getByRole("button", { name: "Cancel" }))
 mock.onGet("/admin/operations/snippets").reply(200, [{ ...snippet, owner_id: "owner", visibility: "private" }]); fireEvent.click(screen.getByRole("button", { name: "Refresh" }))
 await waitFor(() => expect(screen.queryByRole("button", { name: "Approve" })).not.toBeInTheDocument())
})
it("lets admins create owned drafts, validates byte limits and never auto-dispatches", async () => {
 auth.principal.user.id = "admin"; auth.principal.user.role = "admin"; const page = view(); await screen.findByText("Inspect host")
 expect(screen.queryByRole("button", { name: "Approve" })).not.toBeInTheDocument(); expect(screen.queryByRole("button", { name: "Delete" })).not.toBeInTheDocument()
 fireEvent.click(screen.getByRole("button", { name: "New snippet" })); fireEvent.change(screen.getByLabelText("Content"), { target: { value: "printf hello" } }); fireEvent.change(screen.getByLabelText("Snippet name"), { target: { value: "字".repeat(67) } })
 expect(screen.getByRole("button", { name: "Save version" })).toBeDisabled()
 fireEvent.change(screen.getByLabelText("Snippet name"), { target: { value: "My draft" } }); fireEvent.change(screen.getByLabelText("Content"), { target: { value: "bad\0script" } }); expect(screen.getByRole("button", { name: "Save version" })).toBeDisabled()
 fireEvent.change(screen.getByLabelText("Content"), { target: { value: "x".repeat(65537) } }); expect(screen.getByRole("button", { name: "Save version" })).toBeDisabled()
 fireEvent.change(screen.getByLabelText("Content"), { target: { value: "printf hello" } }); mock.onPost("/admin/operations/snippets").reply(200, { ...operationSnippet(), owner_id: "admin", version: 1, approved: false }); fireEvent.click(screen.getByRole("button", { name: "Save version" })); confirm()
 await waitFor(() => expect(screen.queryByLabelText("Content")).not.toBeInTheDocument()); expect(JSON.parse(mock.history.post[0].data)).toMatchObject({ expected_version: 0, visibility: "private", content: btoa("printf hello") }); expect(page.execute).not.toHaveBeenCalled()
})
it("dispatches the approved immutable content after explicit read confirmation", async () => {
 const page = view(); await screen.findByText("Inspect host"); fireEvent.click(screen.getByRole("button", { name: "Dispatch" }))
 expect(page.execute).not.toHaveBeenCalled(); confirm()
 await waitFor(() => expect(page.execute).toHaveBeenCalledWith({ snippet: operationSnippet(), content: "printf private-content" }))
 expect(mock.history.post).toHaveLength(0)
})
it("discards late sensitive responses after unmount or account changes", async () => {
 let finish!: (response: [number, { content: string }]) => void
 mock.onGet("/admin/operations/snippets/snippet-one").reply(() => new Promise(resolve => { finish = resolve }))
 const page = view(); await screen.findByText("Inspect host"); fireEvent.click(screen.getByRole("button", { name: "Dispatch" })); confirm()
 await waitFor(() => expect(finish).toBeDefined()); const signal = mock.history.get.find(request => request.url?.endsWith("/snippet-one"))?.signal; page.unmount(); expect(signal?.aborted).toBe(true); await act(async () => { finish([200, { content: "late private script" }]) })
 expect(page.execute).not.toHaveBeenCalled(); expect(screen.queryByText("late private script")).not.toBeInTheDocument()
})
it("clears loaded sensitive drafts on account changes and preserves content on delete errors", async () => {
 const page = view(); await read(); fireEvent.click(screen.getByRole("button", { name: "Delete" }))
 mock.onDelete("/admin/operations/snippets/snippet-one").replyOnce(503, { message: "delete unavailable" }); confirm(); expect(await screen.findByText("delete unavailable")).toBeInTheDocument(); expect(screen.getByLabelText("Content")).toHaveValue("printf private-content")
 mock.onDelete("/admin/operations/snippets/snippet-one").reply(200, {}); confirm(); await waitFor(() => expect(screen.queryByLabelText("Content")).not.toBeInTheDocument())
 await read(); auth.principal.user.id = "different-admin"; auth.principal.user.role = "admin"; page.rerender(<LocaleProvider><OperationSnippets execute={page.execute} /></LocaleProvider>)
 expect(screen.queryByLabelText("Content")).not.toBeInTheDocument()
})
it("does not require recent proof to save/delete drafts but still protects full content reads", async () => {
 auth.principal.session.reauthenticated_until = "2000-01-01T00:00:00Z"
 view(); await screen.findByText("Inspect host"); fireEvent.click(screen.getByRole("button", { name: "New snippet" }))
 fireEvent.change(screen.getByLabelText("Snippet name"), { target: { value: "Private draft" } }); fireEvent.change(screen.getByLabelText("Content"), { target: { value: "printf draft" } })
 mock.onPost("/admin/operations/snippets").reply(200, { ...operationSnippet(), approved: false }); fireEvent.click(screen.getByRole("button", { name: "Save version" }))
 expect(screen.queryByLabelText("Current password")).not.toBeInTheDocument(); confirm(); await waitFor(() => expect(screen.queryByLabelText("Content")).not.toBeInTheDocument())
 expect(mock.history.post.filter(request => request.url === "/auth/reauthenticate")).toHaveLength(0)
 fireEvent.click(await screen.findByRole("button", { name: "Delete" })); expect(screen.queryByLabelText("Current password")).not.toBeInTheDocument(); fireEvent.click(within(screen.getByRole("dialog")).getByRole("button", { name: "Cancel" }))
 fireEvent.click(screen.getByRole("button", { name: "View / edit" })); expect(screen.getByLabelText("Current password")).toBeInTheDocument()
 mock.onPost("/auth/reauthenticate").reply(200, { reauthenticated_until: "2099-01-01T00:00:00Z" }); fireEvent.change(screen.getByLabelText("Current password"), { target: { value: "fixture password" } }); confirm()
 await screen.findByLabelText("Content"); expect(mock.history.post.filter(request => request.url === "/auth/reauthenticate")).toHaveLength(1)
})
it("refreshes approved metadata and recovers from read/list failures without caching full content", async () => {
 mock.onGet("/admin/operations/snippets").reply(503, { message: "metadata unavailable" }); view(); expect(await screen.findByText("metadata unavailable")).toBeInTheDocument()
 mock.onGet("/admin/operations/snippets").reply(200, [{ ...operationSnippet(), approved: false }]); fireEvent.click(screen.getByRole("button", { name: "Refresh" })); await screen.findByText("Inspect host")
 mock.onGet("/admin/operations/snippets/snippet-one").reply(403, { message: "content access denied" }); fireEvent.click(screen.getByRole("button", { name: "View / edit" })); confirm()
 expect(await screen.findByText("content access denied")).toBeInTheDocument(); expect(screen.queryByLabelText("Content")).not.toBeInTheDocument()
 fireEvent.click(within(screen.getByRole("dialog")).getByRole("button", { name: "Cancel" })); fireEvent.click(screen.getByRole("button", { name: "Approve" }))
 mock.onPost("/admin/operations/snippets/snippet-one/approve").reply(200, {}); mock.onGet("/admin/operations/snippets").reply(200, [operationSnippet()]); confirm()
 await waitFor(() => expect(screen.getByRole("button", { name: "Dispatch" })).toBeEnabled()); expect(screen.queryByRole("button", { name: "Approve" })).not.toBeInTheDocument()
 expect(JSON.parse(mock.history.post[0].data)).toEqual({ version: 2 })
})
it("clears content when the library loses its authenticated identity", async () => {
 const page = view(); await read(); auth.principal.user.id = ""; page.rerender(<LocaleProvider><OperationSnippets execute={page.execute} /></LocaleProvider>)
 expect(screen.getByText("Sign in with an administrator account.")).toBeInTheDocument(); expect(screen.queryByLabelText("Content")).not.toBeInTheDocument()
})
it("does not issue a content request if authentication completes after the library unmounts", async () => {
 auth.principal.session.reauthenticated_until = "2000-01-01T00:00:00Z"
 let finish!: (response: [number, { reauthenticated_until: string }]) => void
 mock.onPost("/auth/reauthenticate").reply(() => new Promise(resolve => { finish = resolve }))
 const page = view(); await screen.findByText("Inspect host"); fireEvent.click(screen.getByRole("button", { name: "View / edit" })); fireEvent.change(screen.getByLabelText("Current password"), { target: { value: "fixture password" } }); confirm()
 await waitFor(() => expect(finish).toBeDefined()); page.unmount(); await act(async () => { finish([200, { reauthenticated_until: "2099-01-01T00:00:00Z" }]) })
 expect(mock.history.get.filter(request => request.url?.endsWith("/snippet-one"))).toHaveLength(0)
})
