import { fireEvent, render, screen, waitFor } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { beforeEach, expect, it, vi } from "vitest"
import { LocaleProvider } from "@/context/locale"
import SSHKeys from "./ssh-keys"
import * as api from "@/lib/api"
import { reauthenticateAdmin } from "@/lib/security-api"

const auth = vi.hoisted(() => ({ principal: { user: { id: "owner", role: "owner", totp_enabled_at: null }, session: { id: "session", reauthenticated_until: "2099-01-01T00:00:00Z" as string | null } } }))
const key = { id: "key", name: "Production", key_type: "ed25519", public_key: "fixture-public", fingerprint: "fixture-fingerprint", created_at: "" }
vi.mock("@/context/auth", () => ({ useAuth: () => auth }))
vi.mock("@/hooks/use-api", () => ({ useSSHKeys: () => ({ data: [key], loading: false, error: null, refresh: async () => undefined }) }))
vi.mock("@/lib/api", () => ({ getSSHKey: vi.fn(), createSSHKey: vi.fn(), generateSSHKey: vi.fn(), deleteSSHKey: vi.fn() }))
vi.mock("@/lib/security-api", () => ({ reauthenticateAdmin: vi.fn() }))
beforeEach(() => { vi.resetAllMocks(); localStorage.setItem("locale", "en"); auth.principal.session.id = "session"; auth.principal.session.reauthenticated_until = "2099-01-01T00:00:00Z"; vi.mocked(api.getSSHKey).mockResolvedValue({ ...key, private_key: "fixture-private" }) })
const page = () => <LocaleProvider><SSHKeys /></LocaleProvider>

it("requires confirmation to reveal and clears secrets on identity change", async () => {
 const view = render(page())
 await userEvent.click(screen.getByRole("button", { name: /Private Key.*Production/ }))
 expect(api.getSSHKey).not.toHaveBeenCalled()
 await userEvent.click(screen.getByRole("button", { name: "Confirm" }))
 expect(await screen.findByText("fixture-private")).toBeInTheDocument()
 auth.principal.session.id = "replacement-session"; view.rerender(page())
 await waitFor(() => expect(screen.queryByText("fixture-private")).not.toBeInTheDocument())
})

it("blocks reveal after failed identity proof and supports an explicit retry", async () => {
 auth.principal.session.reauthenticated_until = null
 vi.mocked(reauthenticateAdmin).mockRejectedValueOnce(new Error("Identity proof rejected")).mockResolvedValueOnce({ ...auth.principal.session, reauthenticated_until: "2099-01-01T00:00:00Z" } as Awaited<ReturnType<typeof reauthenticateAdmin>>)
 render(page()); await userEvent.click(screen.getByRole("button", { name: /Private Key.*Production/ }))
 fireEvent.change(screen.getByLabelText("Current password"), { target: { value: "wrong-proof" } })
 await userEvent.click(screen.getByRole("button", { name: "Confirm" }))
 expect(await screen.findByRole("alert")).toHaveTextContent("Identity proof rejected"); expect(api.getSSHKey).not.toHaveBeenCalled()
 fireEvent.change(screen.getByLabelText("Current password"), { target: { value: "correct-proof" } })
 await userEvent.click(screen.getByRole("button", { name: "Confirm" }))
 expect(await screen.findByText("fixture-private")).toBeInTheDocument(); expect(api.getSSHKey).toHaveBeenCalledOnce()
})

it("drops imported private-key drafts when confirmation is cancelled", async () => {
 render(page()); await userEvent.click(screen.getByRole("button", { name: "Create" }))
 fireEvent.change(screen.getByLabelText("Name"), { target: { value: "New key" } })
 fireEvent.change(screen.getByLabelText("Public Key"), { target: { value: "fixture-public" } })
 fireEvent.change(screen.getByLabelText(/Private Key/, { selector: "textarea" }), { target: { value: "fixture-private" } })
 await userEvent.click(screen.getByRole("button", { name: "Confirm" }))
 expect(api.createSSHKey).not.toHaveBeenCalled()
 await userEvent.click(screen.getByRole("button", { name: "Cancel" }))
 await userEvent.click(screen.getByRole("button", { name: "Create" }))
 expect(screen.getByLabelText(/Private Key/, { selector: "textarea" })).toHaveValue(""); expect(api.createSSHKey).not.toHaveBeenCalled()
})

it("keeps a failed deletion visible for retry", async () => {
 vi.mocked(api.deleteSSHKey).mockRejectedValueOnce(new Error("Audit unavailable")).mockResolvedValueOnce(undefined)
 render(page()); await userEvent.click(screen.getByRole("button", { name: "Delete · Production" }))
 await userEvent.click(screen.getByRole("button", { name: "Confirm" }))
 expect(await screen.findByRole("alert")).toHaveTextContent("Audit unavailable")
 await userEvent.click(screen.getByRole("button", { name: "Confirm" }))
 await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument())
 expect(api.deleteSSHKey).toHaveBeenCalledTimes(2)
})
