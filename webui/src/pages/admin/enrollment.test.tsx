import { render, screen, waitFor, within } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { MemoryRouter } from "react-router"
import { beforeEach, describe, expect, it, vi } from "vitest"
import { LocaleProvider } from "@/context/locale"
import EnrollmentPage from "@/pages/admin/enrollment"
import * as api from "@/lib/enrollment-discovery-api"
import type { EnrollmentGrant, EnrollmentRequest } from "@/types/enrollment"

const auth = vi.hoisted(() => ({ principal: { user: { id: "owner", role: "owner" }, session: { id: "session", reauthenticated_until: "2099-01-01T00:00:00Z" } } }))
vi.mock("@/context/auth", () => ({ useAuth: () => auth }))
vi.mock("@/lib/enrollment-discovery-api")
vi.mock("@/hooks/use-api", () => ({ useGroups: () => ({ data: [group], loading: false, error: null }) }))

const group = { id: "group-1", name: "Production", sort_order: 0, is_default: true, created_at: "", updated_at: "" }

const grant: EnrollmentGrant = {
  id: "grant-1", name: "Datacenter rollout", secret_prefix: "beat_enroll_v1_abcd", mode: "manual", enabled: true,
  expires_at: "2027-01-01T00:00:00Z", default_group_id: "group-1", default_group_name: "Production",
  node_name_prefix: "web", tags: ["edge"], remarks: "", public: false, ssh_port: 22, source_ip_as_host: false,
  source_cidrs: ["10.0.0.0/8"], hostname_pattern: "^web-.*$", claim_recovery: true, maximum_enrollments: 10,
  consumed_enrollments: 2, created_at: "2026-10-01T00:00:00Z", updated_at: "2026-10-01T00:00:00Z",
}

const request: EnrollmentRequest = {
  id: "request-1", grant_id: "grant-1", grant_name: "Datacenter rollout", status: "pending", node_name: "web-9",
  hostname: "web-9.example.com", advertised_host: "10.0.0.9", os: "linux", architecture: "amd64",
  agent_version: "1.4.0", virtualization: "kvm", ssh_port: 2222, source_ip: "10.0.0.9", installation_nonce: "nonce-1",
  first_request_at: "2026-10-15T01:00:00Z", last_request_at: "2026-10-15T01:05:00Z", expires_at: "2026-10-15T02:00:00Z",
}

function view() {
  return render(<LocaleProvider><MemoryRouter><EnrollmentPage /></MemoryRouter></LocaleProvider>)
}

async function openTab(name: string) {
  await userEvent.click(screen.getByRole("tab", { name }))
}

/** 确认弹窗总是渲染在表单弹窗之后，取最后一个对话框即可避免角色歧义。 */
function lastDialog(): HTMLElement {
  const dialogs = screen.getAllByRole("dialog")
  return dialogs[dialogs.length - 1]
}

async function confirmDialog(): Promise<HTMLElement> {
  const dialog = await screen.findByRole("dialog", { name: /(Grant|grant|enrollment)/ })
  await waitFor(() => expect(within(dialog).getByRole("button", { name: "Confirm" })).toBeInTheDocument())
  return dialog
}

describe("node enrollment workspace", () => {
  beforeEach(() => {
    vi.resetAllMocks()
    auth.principal.user.role = "owner"
    vi.mocked(api.listEnrollmentRequests).mockResolvedValue({ requests: [request], next_cursor: "" })
    vi.mocked(api.listEnrollmentGrants).mockResolvedValue([grant])
    vi.mocked(api.approveEnrollmentRequest).mockResolvedValue({ ...request, status: "approved", node_name: "web-9" })
    vi.mocked(api.rejectEnrollmentRequest).mockResolvedValue({ ...request, status: "rejected", reason: "policy-conflict" })
    vi.mocked(api.createEnrollmentGrant).mockResolvedValue({ grant, secret: "beat_enroll_v1_created" })
    vi.mocked(api.updateEnrollmentGrant).mockResolvedValue(grant)
    vi.mocked(api.rotateEnrollmentGrantSecret).mockResolvedValue({ grant, secret: "beat_enroll_v1_rotated" })
    vi.mocked(api.revokeEnrollmentGrant).mockResolvedValue(grant)
  })

  it("opens the approval queue first and shows the reported machine details", async () => {
    view()
    expect(screen.getByRole("tab", { name: "Pending approval" })).toHaveAttribute("aria-selected", "true")
    expect(await screen.findByText("web-9.example.com")).toBeInTheDocument()
    expect(screen.getByText(/Datacenter rollout/)).toBeInTheDocument()
    expect(screen.getByText("linux · amd64")).toBeInTheDocument()
    expect(screen.getAllByText("10.0.0.9")).toHaveLength(2)
    expect(api.listEnrollmentRequests).toHaveBeenCalledWith(
      expect.objectContaining({ status: "pending", limit: 25 }), expect.any(AbortSignal))
  })

  it("hides grant management from ordinary administrators and does not read grants", async () => {
    auth.principal.user.role = "admin"
    view()
    expect(screen.queryByRole("tab", { name: "Enrollment grants" })).not.toBeInTheDocument()
    await screen.findByText("web-9.example.com")
    expect(api.listEnrollmentGrants).not.toHaveBeenCalled()
  })

  it("approves a request with the final node fields chosen in the sheet", async () => {
    view()
    await userEvent.click(await screen.findByRole("button", { name: "Approve" }))
    const dialog = within(await screen.findByRole("dialog"))
    expect(dialog.getByLabelText("Node name")).toHaveValue("web-9")
    expect(dialog.getByText("Production")).toBeInTheDocument()
    expect(dialog.queryByText("group-1")).not.toBeInTheDocument()
    await userEvent.clear(dialog.getByLabelText("Node name"))
    await userEvent.type(dialog.getByLabelText("Node name"), "web-9-final")
    await userEvent.click(dialog.getByRole("button", { name: "Approve" }))
    await waitFor(() => expect(api.approveEnrollmentRequest).toHaveBeenCalledWith("request-1", {
      node_name: "web-9-final", group_id: "group-1", tags: ["edge"], remarks: "", public: false,
      host: "10.0.0.9", ssh_port: 2222,
    }))
    expect(await screen.findByRole("status")).toHaveTextContent("Approved.")
  })

  it("blocks an approval without a node name and keeps the dialog open", async () => {
    view()
    await userEvent.click(await screen.findByRole("button", { name: "Approve" }))
    const dialog = within(await screen.findByRole("dialog"))
    await userEvent.clear(dialog.getByLabelText("Node name"))
    await userEvent.click(dialog.getByRole("button", { name: "Approve" }))
    expect(await dialog.findByText("Enter the final node name.")).toBeInTheDocument()
    expect(api.approveEnrollmentRequest).not.toHaveBeenCalled()
  })

  it("rejects a machine with a bounded reason code", async () => {
    view()
    await userEvent.click(await screen.findByRole("button", { name: "Reject" }))
    const dialog = within(await screen.findByRole("dialog"))
    await userEvent.type(dialog.getByLabelText("Reason code"), "policy-conflict")
    await userEvent.click(dialog.getByRole("button", { name: "Reject" }))
    await waitFor(() => expect(api.rejectEnrollmentRequest).toHaveBeenCalledWith("request-1", "policy-conflict"))
    expect(await screen.findByRole("status")).toHaveTextContent("Request rejected.")
  })

  it("lists grants and reveals the one-time secret after creation", async () => {
    view()
    await openTab("Enrollment grants")
    expect(await screen.findByText("Datacenter rollout")).toBeInTheDocument()
    expect(screen.getByText(/2\/10/)).toBeInTheDocument()
    expect(screen.getByText("10.0.0.0/8 · ^web-.*$")).toBeInTheDocument()
    await userEvent.click(screen.getByRole("button", { name: "Create grant" }))
    const dialog = within(await screen.findByRole("dialog"))
    await userEvent.type(dialog.getByLabelText("Grant name"), "Edge rollout")
    await userEvent.click(dialog.getByRole("combobox", { name: "Approval" }))
    await userEvent.click(await screen.findByRole("option", { name: "Automatic approval" }))
    await userEvent.clear(dialog.getByLabelText("Maximum enrollments"))
    await userEvent.type(dialog.getByLabelText("Maximum enrollments"), "5")
    await userEvent.type(dialog.getByLabelText("Trusted source CIDRs"), "10.0.0.0/8")
    await userEvent.type(dialog.getByLabelText("Hostname pattern"), "^web-.*$")
    await userEvent.click(dialog.getByRole("checkbox", { name: "Accept new requests" }))
    await userEvent.click(dialog.getByRole("button", { name: "Save" }))
    await userEvent.click(within(await confirmDialog()).getByRole("button", { name: "Confirm" }))
    await waitFor(() => expect(api.createEnrollmentGrant).toHaveBeenCalledWith(expect.objectContaining({
      name: "Edge rollout", mode: "automatic", default_group_id: "group-1", maximum_enrollments: 5,
      enabled: false, source_cidrs: ["10.0.0.0/8"], hostname_pattern: "^web-.*$",
    })))
    const secret = within(lastDialog())
    expect(secret.getByText("beat_enroll_v1_created")).toBeInTheDocument()
    const command = secret.getByText(/beat-agent enroll --server/)
    expect(command.textContent).toContain("--server http://localhost:3000 --allow-http --grant-stdin <<'BEAT_ENROLL'")
    expect(command.textContent).toContain("\nbeat_enroll_v1_created\nBEAT_ENROLL")
    expect(secret.getByText(/--grant-file instead of --grant-stdin/)).toBeInTheDocument()
    expect({ ...localStorage }).toEqual({})
    expect(window.location.search).toBe("")
    expect(api.createEnrollmentGrant).toHaveBeenCalledTimes(1)
  })

  it("routes revocation through the owner confirmation flow", async () => {
    view()
    await openTab("Enrollment grants")
    await userEvent.click(await screen.findByRole("button", { name: "Revoke" }))
    const dialog = within(lastDialog())
    expect(dialog.getByText(/Consumed slots are not returned/)).toBeInTheDocument()
    expect(api.revokeEnrollmentGrant).not.toHaveBeenCalled()
    await userEvent.click(dialog.getByRole("button", { name: "Confirm" }))
    await waitFor(() => expect(api.revokeEnrollmentGrant).toHaveBeenCalledWith("grant-1"))
    expect(screen.getByRole("status")).toHaveTextContent("Grant revoked.")
  })

  it("rotates the grant secret and shows it exactly once", async () => {
    view()
    await openTab("Enrollment grants")
    await userEvent.click(await screen.findByRole("button", { name: "Rotate secret" }))
    await userEvent.click(within(lastDialog()).getByRole("button", { name: "Confirm" }))
    await waitFor(() => expect(api.rotateEnrollmentGrantSecret).toHaveBeenCalledWith("grant-1"))
    const rotated = within(lastDialog())
    expect(rotated.getByText("beat_enroll_v1_rotated")).toBeInTheDocument()
    expect(rotated.getByText(/beat-agent enroll --server/).textContent).toContain("beat_enroll_v1_rotated")
  })

  it("clears the one-time secret once the dialog closes", async () => {
    view()
    await openTab("Enrollment grants")
    await userEvent.click(await screen.findByRole("button", { name: "Rotate secret" }))
    await userEvent.click(within(lastDialog()).getByRole("button", { name: "Confirm" }))
    expect(within(lastDialog()).getByText("beat_enroll_v1_rotated")).toBeInTheDocument()
    await userEvent.keyboard("{Escape}")
    await waitFor(() => expect(screen.queryByText(/beat_enroll_v1_rotated/)).not.toBeInTheDocument())
    expect(screen.queryByText(/beat-agent enroll --server/)).not.toBeInTheDocument()
  })

  it("reports a failed queue load and retries on demand", async () => {
    vi.mocked(api.listEnrollmentRequests).mockRejectedValueOnce(new Error("queue unavailable"))
    view()
    expect(await screen.findByRole("alert")).toHaveTextContent("queue unavailable")
    vi.mocked(api.listEnrollmentRequests).mockResolvedValue({ requests: [], next_cursor: "" })
    await userEvent.click(screen.getByRole("button", { name: "Retry" }))
    expect(await screen.findByText("No machine is waiting for approval.")).toBeInTheDocument()
  })
})
