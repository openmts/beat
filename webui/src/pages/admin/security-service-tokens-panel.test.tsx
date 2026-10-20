import { render, screen, waitFor } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { beforeEach, describe, expect, it, vi } from "vitest"
import { LocaleProvider } from "@/context/locale"
import * as operationsAPI from "@/lib/operations-api"
import * as securityAPI from "@/lib/security-api"
import * as tokensAPI from "@/lib/service-tokens-api"
import { SecurityServiceTokensPanel } from "@/pages/admin/security-service-tokens-panel"
import type { ServiceToken } from "@/types/service-tokens"

vi.mock("@/context/auth", () => ({ useAuth: () => ({ principal: {
  user: { id: "owner", role: "owner", totp_enabled_at: "2026-01-01T00:00:00Z" },
  session: { id: "session-one", reauthenticated_until: null },
} }) }))
vi.mock("@/lib/service-tokens-api")
vi.mock("@/lib/operations-api")
vi.mock("@/lib/security-api")

const now = Date.now()
const activeToken: ServiceToken = {
  id: "token-one", name: "nightly backup", purpose: "ci", prefix: "beat_service_v1_abcdefgh",
  visibility: "private", source_cidrs: ["192.0.2.0/24"],
  scopes: ["backups:create", "health:read"],
  resources: [{ kind: "node", id: "node-one" }],
  expires_at: new Date(now + 7 * 86_400_000).toISOString(), rotation_overlap_seconds: 0,
  revision: 1, created_by: "owner", created_at: new Date(now - 86_400_000).toISOString(),
}
const credential = { token: "beat_service_v1_onetimeplaintext", principal: activeToken }

beforeEach(() => {
  vi.resetAllMocks()
  vi.mocked(tokensAPI.listServiceTokens).mockResolvedValue([activeToken])
  vi.mocked(tokensAPI.createServiceToken).mockResolvedValue(credential)
  vi.mocked(tokensAPI.rotateServiceToken).mockResolvedValue(credential)
  vi.mocked(tokensAPI.revokeServiceToken).mockResolvedValue({ revoked: true })
  vi.mocked(operationsAPI.getOperationNodes).mockResolvedValue([])
  vi.mocked(operationsAPI.getOperationGroups).mockResolvedValue([])
  vi.mocked(securityAPI.reauthenticateAdmin).mockResolvedValue({ id: "session-one", reauthenticated_until: new Date(now + 600_000).toISOString() } as never)
})

async function proveIdentity(user: ReturnType<typeof userEvent.setup>) {
  await user.type(await screen.findByLabelText("Current password"), "correct horse")
  await user.type(screen.getByLabelText("Authenticator code"), "123456")
  await user.click(screen.getByRole("button", { name: "Confirm identity" }))
}

describe("security service tokens panel", () => {
  it("lists localized metadata and revokes only after a reason and confirmation", async () => {
    const user = userEvent.setup()
    render(<LocaleProvider><SecurityServiceTokensPanel /></LocaleProvider>)
    expect(await screen.findByText("nightly backup")).toBeInTheDocument()
    expect(screen.getByText("beat_service_v1_abcdefgh")).toBeInTheDocument()
    expect(screen.getByText("Create backups")).toBeInTheDocument()
    expect(screen.getByText("Node")).toBeInTheDocument()
    expect(screen.getByText("192.0.2.0/24")).toBeInTheDocument()

    await user.click(screen.getByRole("button", { name: "Revoke nightly backup" }))
    const revokeButton = await screen.findByRole("button", { name: "Revoke" })
    expect(revokeButton).toBeDisabled()
    await user.type(screen.getByLabelText("Reason"), "rotated out of use")
    await user.click(revokeButton)
    await proveIdentity(user)
    await waitFor(() => expect(tokensAPI.revokeServiceToken).toHaveBeenCalledWith("token-one", "rotated out of use"))
  })

  it("creates a token behind recent proof and clears the one-time plaintext", async () => {
    const user = userEvent.setup()
    render(<LocaleProvider><SecurityServiceTokensPanel /></LocaleProvider>)
    await screen.findByText("nightly backup")
    await user.click(screen.getByRole("button", { name: "Create token" }))
    await user.type(screen.getByLabelText("Name"), "ci runner")
    await user.click(screen.getByRole("checkbox", { name: /Read extended health/ }))
    await user.click(screen.getByRole("button", { name: "Confirm" }))
    await proveIdentity(user)
    expect(await screen.findByText("beat_service_v1_onetimeplaintext")).toBeInTheDocument()
    await waitFor(() => expect(tokensAPI.createServiceToken).toHaveBeenCalledWith(expect.objectContaining({
      name: "ci runner", scopes: ["health:read"], visibility: "private", source_cidrs: [],
    })))
    await user.click(screen.getByRole("button", { name: "I have stored it" }))
    await waitFor(() => expect(screen.queryByText("beat_service_v1_onetimeplaintext")).not.toBeInTheDocument())
  })

  it("rotates an existing token with the selected overlap", async () => {
    const user = userEvent.setup()
    render(<LocaleProvider><SecurityServiceTokensPanel /></LocaleProvider>)
    await screen.findByText("nightly backup")
    await user.click(screen.getByRole("button", { name: "Rotate nightly backup" }))
    await user.click(await screen.findByRole("button", { name: "Confirm" }))
    await proveIdentity(user)
    await waitFor(() => expect(tokensAPI.rotateServiceToken).toHaveBeenCalledWith("token-one", expect.objectContaining({
      name: "nightly backup", scopes: ["backups:create", "health:read"], overlap_seconds: 0,
    })))
  })

  it("surfaces load failures and offers a retry", async () => {
    vi.mocked(tokensAPI.listServiceTokens).mockRejectedValueOnce(new Error("network down"))
    const user = userEvent.setup()
    render(<LocaleProvider><SecurityServiceTokensPanel /></LocaleProvider>)
    expect(await screen.findByText("network down")).toBeInTheDocument()
    vi.mocked(tokensAPI.listServiceTokens).mockResolvedValue([activeToken])
    await user.click(screen.getByRole("button", { name: "Refresh" }))
    expect(await screen.findByText("nightly backup")).toBeInTheDocument()
  })
})
