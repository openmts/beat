import { act, fireEvent, render, screen, waitFor, within } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { beforeEach, describe, expect, it, vi } from "vitest"
import { LocaleProvider } from "@/context/locale"
import { SecurityAdminsPanel } from "./security-admins-panel"
import * as security from "@/lib/security-api"
import type { AdminPrincipal, AdminUser } from "@/types/security"
const auth = vi.hoisted(() => ({ principal: { user: { id: "owner", username: "owner", display_name: "Owner", role: "owner", enabled: true }, session: { id: "session", user_id: "owner", reauthenticated_until: "2099-01-01T00:00:00Z" } } as AdminPrincipal }))
vi.mock("@/context/auth", () => ({ useAuth: () => auth }))
vi.mock("@/lib/security-api")
const other = (): AdminUser => ({ ...auth.principal.user, id: "other", username: "other", display_name: "Other", role: "admin" })
const mount = () => render(<LocaleProvider><SecurityAdminsPanel /></LocaleProvider>)
beforeEach(() => { vi.resetAllMocks(); auth.principal.user.role = "owner"; auth.principal.session.id = "session"; auth.principal.session.reauthenticated_until = "2099-01-01T00:00:00Z"; vi.mocked(security.listAdminUsers).mockResolvedValue([auth.principal.user, other()]); vi.mocked(security.reauthenticateAdmin).mockResolvedValue(auth.principal.session) })
function fillCreate() { for (const [label, value] of [["Username", "new-user"], ["Display name", "New user"], ["Password", "new-user-password"]]) fireEvent.change(screen.getByLabelText(label), { target: { value } }) }
describe("administrator account permissions and confirmation", () => {
 it("does not fetch or expose accounts to non-owners", () => { auth.principal.user.role = "admin"; const view = mount(); expect(view.container).toBeEmptyDOMElement(); expect(security.listAdminUsers).not.toHaveBeenCalled() })
 it("requires recent proof before creation and never offers self-delete", async () => {
  auth.principal.session.reauthenticated_until = null; mount(); await screen.findByText("@other · Administrator"); expect(screen.getAllByRole("button", { name: "Delete" })[0]).toBeDisabled()
  fillCreate(); await userEvent.click(screen.getByRole("button", { name: "Create administrator" })); expect(security.createAdminUser).not.toHaveBeenCalled(); expect(screen.getByLabelText("Password")).toHaveValue("")
  fireEvent.change(screen.getByLabelText("Current password"), { target: { value: "owner-proof" } }); await userEvent.click(screen.getByRole("button", { name: "Confirm" })); await waitFor(() => expect(security.createAdminUser).toHaveBeenCalledWith({ username: "new-user", display_name: "New user", password: "new-user-password", role: "admin" })); expect(security.reauthenticateAdmin).toHaveBeenCalledWith("owner-proof", "")
 })
 it("clears password on cancel and failed creation, requiring fresh entry on retry", async () => {
  vi.mocked(security.createAdminUser).mockRejectedValue(new Error("conflict")); mount(); fillCreate(); await userEvent.click(screen.getByRole("button", { name: "Create administrator" })); await userEvent.click(screen.getByRole("button", { name: "Cancel" })); expect(screen.getByLabelText("Password")).toHaveValue(""); expect(security.createAdminUser).not.toHaveBeenCalled()
  fillCreate(); await userEvent.click(screen.getByRole("button", { name: "Create administrator" })); await userEvent.click(screen.getByRole("button", { name: "Confirm" })); expect(await screen.findByRole("alert")).toHaveTextContent("conflict"); expect(screen.getByLabelText("Password")).toHaveValue("")
  await userEvent.click(screen.getByRole("button", { name: "Confirm" })); expect(await screen.findByRole("alert")).toHaveTextContent(/enter the new account password again/); expect(security.createAdminUser).toHaveBeenCalledTimes(1)
 })
 it("edits role, display name and enabled state through a separate confirmation", async () => {
  mount(); await screen.findByText("@other · Administrator"); await userEvent.click(screen.getAllByRole("button", { name: "Edit" })[1]); const dialog = screen.getByRole("dialog")
  fireEvent.change(within(dialog).getByLabelText("Display name"), { target: { value: "New display" } }); await userEvent.click(within(dialog).getByRole("combobox")); await userEvent.click(await screen.findByRole("option", { name: "Owner" })); await userEvent.click(within(dialog).getByRole("checkbox", { name: "Enabled" })); await userEvent.click(within(dialog).getByRole("button", { name: "Save" })); expect(security.updateAdminUser).not.toHaveBeenCalled()
  await userEvent.click(screen.getByRole("button", { name: "Confirm" })); await waitFor(() => expect(security.updateAdminUser).toHaveBeenCalledWith("other", { display_name: "New display", role: "owner", enabled: false }))
 })
 it("clears edits when session identity changes and rejects abandoned list results", async () => {
  let resolve!: (users: AdminUser[]) => void; vi.mocked(security.listAdminUsers).mockImplementationOnce(() => new Promise(done => { resolve = done })); const view = mount(); fillCreate()
  auth.principal.session.id = "new-session"; view.rerender(<LocaleProvider><SecurityAdminsPanel /></LocaleProvider>); await screen.findByText("@other · Administrator"); expect(screen.getByLabelText("Password")).toHaveValue(""); expect(screen.getByLabelText("Username")).toHaveValue("")
  await act(async () => resolve([{ ...other(), display_name: "Abandoned" }])); expect(screen.queryByText("Abandoned")).not.toBeInTheDocument()
 })
})
