import { fireEvent, render, screen, waitFor } from "@testing-library/react"
import { beforeEach, describe, expect, it, vi } from "vitest"
import { LocaleProvider } from "@/context/locale"
import { SecurityOIDCIdentitiesPanel } from "@/pages/admin/security-oidc-identities-panel"
import { SecurityOIDCProvidersPanel } from "@/pages/admin/security-oidc-providers-panel"

const api = vi.hoisted(() => ({
  identities: vi.fn(),
  providers: vi.fn(),
  link: vi.fn(),
  unlink: vi.fn(),
  create: vi.fn(),
  update: vi.fn(),
  remove: vi.fn(),
  reauth: vi.fn(),
  listAdmin: vi.fn(),
}))
vi.mock("@/lib/security-api", () => ({
  listMyExternalIdentities: () => api.identities(),
  listPublicOIDCProviders: () => api.providers(),
  beginOIDCLink: () => api.link(),
  unlinkExternalIdentity: (id: string) => api.unlink(id),
  createOIDCProvider: () => api.create(),
  updateOIDCProvider: () => api.update(),
  deleteOIDCProvider: (id: string) => api.remove(id),
  reauthenticateAdmin: () => api.reauth(),
  listAdminOIDCProviders: () => api.listAdmin(),
}))

describe("OIDC identities panel", () => {
  beforeEach(() => {
    vi.resetAllMocks()
    api.identities.mockResolvedValue([
      {
        id: "id-1", provider_id: "p1", provider_name: "Corporate IdP", email: "u@example.com",
        email_verified: true, preferred_username: "user", last_login_at: "2026-08-01T00:00:00Z",
        created_at: "2026-07-01T00:00:00Z", updated_at: "2026-08-01T00:00:00Z",
      },
    ])
    api.providers.mockResolvedValue([{ id: "p2", slug: "issuer2", display_name: "Second IdP" }])
    api.link.mockResolvedValue("https://idp.example/link")
  })

  it("lists identities and provider names", async () => {
    render(<LocaleProvider><SecurityOIDCIdentitiesPanel /></LocaleProvider>)
    expect(await screen.findByText("Corporate IdP")).toBeInTheDocument()
    expect(screen.getByText("user")).toBeInTheDocument()
  })

  it("unlinks an identity", async () => {
    api.unlink.mockResolvedValue(undefined)
    render(<LocaleProvider><SecurityOIDCIdentitiesPanel /></LocaleProvider>)
    const unlink = await screen.findByRole("button", { name: "Unlink" })
    fireEvent.click(unlink)
    await waitFor(() => expect(api.unlink).toHaveBeenCalledWith("id-1"))
  })

  it("starts a link flow and navigates", async () => {
    const assign = vi.fn()
    Object.defineProperty(window, "location", { value: { assign }, writable: true })
    render(<LocaleProvider><SecurityOIDCIdentitiesPanel /></LocaleProvider>)
    const link = await screen.findByRole("button", { name: "Link identity · Second IdP" })
    fireEvent.click(link)
    await waitFor(() => expect(assign).toHaveBeenCalledWith("https://idp.example/link"))
  })

  it("shows empty state", async () => {
    api.identities.mockResolvedValue([])
    render(<LocaleProvider><SecurityOIDCIdentitiesPanel /></LocaleProvider>)
    expect(await screen.findByText("No external identities linked yet.")).toBeInTheDocument()
  })

  it("surfaces a load failure", async () => {
    api.identities.mockRejectedValue(new Error("store down"))
    render(<LocaleProvider><SecurityOIDCIdentitiesPanel /></LocaleProvider>)
    expect(await screen.findByText("store down")).toBeInTheDocument()
  })

  it("surfaces an unlink failure", async () => {
    api.unlink.mockRejectedValue(new Error("unlink failed"))
    render(<LocaleProvider><SecurityOIDCIdentitiesPanel /></LocaleProvider>)
    fireEvent.click(await screen.findByRole("button", { name: "Unlink" }))
    expect(await screen.findByText("unlink failed")).toBeInTheDocument()
  })

  it("surfaces a link failure", async () => {
    api.link.mockRejectedValue(new Error("link failed"))
    render(<LocaleProvider><SecurityOIDCIdentitiesPanel /></LocaleProvider>)
    fireEvent.click(await screen.findByRole("button", { name: "Link identity · Second IdP" }))
    expect(await screen.findByText("link failed")).toBeInTheDocument()
  })
})

describe("OIDC providers panel", () => {
  beforeEach(() => {
    vi.resetAllMocks()
    api.listAdmin.mockResolvedValue([
      {
        id: "p1", slug: "issuer", display_name: "Corporate IdP", issuer_url: "https://idp.example",
        client_id: "c1", client_secret_configured: true, scopes: ["openid"],
        enabled: true, configuration_version: 1, sort_order: 0,
        created_at: "2026-07-01T00:00:00Z", updated_at: "2026-07-01T00:00:00Z",
      },
    ])
    api.reauth.mockResolvedValue({ id: "s" })
  })

  it("lists providers with secret and enabled state", async () => {
    render(<LocaleProvider><SecurityOIDCProvidersPanel /></LocaleProvider>)
    expect(await screen.findByText("Corporate IdP")).toBeInTheDocument()
    expect(screen.getByText("Secret configured")).toBeInTheDocument()
    expect(screen.getAllByText("Enabled").length).toBeGreaterThan(0)
  })

  it("creates a provider through the dialog", async () => {
    api.create.mockResolvedValue({ id: "p3" })
    render(<LocaleProvider><SecurityOIDCProvidersPanel /></LocaleProvider>)
    fireEvent.click(await screen.findByRole("button", { name: "Add provider" }))
    fireEvent.change(screen.getByLabelText("Slug"), { target: { value: "new-idp" } })
    fireEvent.change(screen.getByLabelText("Display name"), { target: { value: "New IdP" } })
    fireEvent.change(screen.getByLabelText("Issuer URL"), { target: { value: "https://new-idp.example" } })
    fireEvent.change(screen.getByLabelText("Client ID"), { target: { value: "client" } })
    fireEvent.change(screen.getByLabelText("Client secret"), { target: { value: "secret" } })
    fireEvent.click(screen.getByRole("button", { name: "Create provider" }))
    await waitFor(() => expect(api.create).toHaveBeenCalled())
  })

  it("deletes a provider", async () => {
    api.remove.mockResolvedValue(undefined)
    render(<LocaleProvider><SecurityOIDCProvidersPanel /></LocaleProvider>)
    fireEvent.click(await screen.findByRole("button", { name: "Delete provider" }))
    await waitFor(() => expect(api.remove).toHaveBeenCalledWith("p1"))
  })

  it("prompts for recent authentication when the server requires it", async () => {
    api.update.mockRejectedValue(new Error("recent authentication is required"))
    render(<LocaleProvider><SecurityOIDCProvidersPanel /></LocaleProvider>)
    const toggle = (await screen.findAllByRole("switch"))[0]
    fireEvent.click(toggle)
    await waitFor(() => expect(screen.getByRole("button", { name: "Confirm identity" })).toBeInTheDocument())
    fireEvent.change(screen.getByLabelText("Current password"), { target: { value: "password" } })
    fireEvent.click(screen.getByRole("button", { name: "Confirm identity" }))
    await waitFor(() => expect(api.reauth).toHaveBeenCalled())
  })

  it("toggles a provider and reloads", async () => {
    api.update.mockResolvedValue({ id: "p1", enabled: false })
    render(<LocaleProvider><SecurityOIDCProvidersPanel /></LocaleProvider>)
    const toggle = (await screen.findAllByRole("switch"))[0]
    fireEvent.click(toggle)
    await waitFor(() => expect(api.update).toHaveBeenCalled())
  })

  it("opens the edit dialog with provider values", async () => {
    render(<LocaleProvider><SecurityOIDCProvidersPanel /></LocaleProvider>)
    fireEvent.click(await screen.findByRole("button", { name: "Edit provider" }))
    expect(await screen.findByLabelText("Slug")).toHaveValue("issuer")
    expect(screen.getByLabelText("Issuer URL")).toHaveValue("https://idp.example")
  })

  it("shows a non-reauth error on toggle", async () => {
    api.update.mockRejectedValue(new Error("configuration invalid"))
    render(<LocaleProvider><SecurityOIDCProvidersPanel /></LocaleProvider>)
    fireEvent.click((await screen.findAllByRole("switch"))[0])
    expect(await screen.findByText("configuration invalid")).toBeInTheDocument()
  })

  it("surfaces a load failure", async () => {
    api.listAdmin.mockRejectedValue(new Error("providers unavailable"))
    render(<LocaleProvider><SecurityOIDCProvidersPanel /></LocaleProvider>)
    expect(await screen.findByText("providers unavailable")).toBeInTheDocument()
  })
})