import MockAdapter from "axios-mock-adapter"
import { afterEach, beforeEach, describe, expect, it } from "vitest"
import { api } from "@/lib/api-client"
import {
  beginOIDCLink, beginOIDCLogin, beginTOTP, bootstrapAdmin, changeAdminPassword,
  createAdminUser, createOIDCProvider, deleteAdminUser, deleteOIDCProvider,
  disableTOTP, enableTOTP, getAdminSession, getAuthState, listAdminOIDCProviders,
  listAdminSessions, listAdminUsers, listAuditEvents, listMyExternalIdentities,
  listPublicOIDCProviders, loginAdmin, logoutAdmin, reauthenticateAdmin,
  revokeAdminSession, revokeOtherAdminSessions, unlinkExternalIdentity,
  updateAdminUser, updateOIDCProvider,
} from "@/lib/security-api"

describe("security API", () => {
  let mock: MockAdapter

  beforeEach(() => { mock = new MockAdapter(api) })
  afterEach(() => mock.restore())

  it("maps every administrator security request", async () => {
    mock.onGet("/auth/state").reply(200, { setup_required: true })
    expect(await getAuthState()).toEqual({ setup_required: true })

    const principal = { user: { id: "owner" }, session: { id: "session" } }
    mock.onPost("/auth/bootstrap").reply((config) => [201, { ...principal, request: JSON.parse(config.data) }])
    const bootstrapped = await bootstrapAdmin({ bootstrap_token: "token", username: "owner",
      display_name: "Owner", password: "long-password" })
    expect(bootstrapped.user.id).toBe("owner")

    mock.onPost("/auth/login").reply(200, principal)
    expect((await loginAdmin({ username: "owner", password: "long-password" })).session.id).toBe("session")
    mock.onPost("/auth/logout").reply(200)
    await expect(logoutAdmin()).resolves.toBeUndefined()
    mock.onGet("/auth/session").reply(200, principal)
    expect((await getAdminSession()).user.id).toBe("owner")
    mock.onPost("/auth/reauthenticate").reply(200, { id: "session" })
    expect((await reauthenticateAdmin("password", "123456")).id).toBe("session")

    mock.onGet("/admin/users").reply(200, [{ id: "owner" }])
    expect(await listAdminUsers()).toHaveLength(1)
    mock.onPost("/admin/users").reply(201, { id: "admin" })
    expect((await createAdminUser({ username: "admin", display_name: "Admin", role: "admin",
      password: "long-password" })).id).toBe("admin")
    mock.onPut("/admin/users/admin").reply(200)
    await updateAdminUser("admin", { display_name: "Admin", role: "owner", enabled: true })
    mock.onDelete("/admin/users/admin").reply(200)
    await deleteAdminUser("admin")
    mock.onPut("/admin/users/me/password").reply(200)
    await changeAdminPassword({ current_password: "old", new_password: "new-password-long", totp_code: "123456" })

    mock.onPost("/admin/users/me/totp").replyOnce(200, { secret: "secret", uri: "uri" })
    expect((await beginTOTP()).secret).toBe("secret")
    mock.onPost("/admin/users/me/totp").replyOnce(200)
    await enableTOTP("123456")
    mock.onDelete("/admin/users/me/totp").reply(200)
    await disableTOTP()

    mock.onGet("/admin/sessions").reply(200, [{ id: "session" }])
    expect(await listAdminSessions()).toHaveLength(1)
    mock.onDelete("/admin/sessions/session").reply(200)
    await revokeAdminSession("session")
    mock.onDelete("/admin/sessions/others").reply(200, { revoked: 2 })
    expect(await revokeOtherAdminSessions()).toBe(2)
    mock.onGet("/admin/audit-events").reply((config) => {
      expect(config.params).toMatchObject({ limit: 50, cursor: "page-1", action: "backup.create" })
      return [200, { events: [{ id: "event" }], next_cursor: "page-2" }]
    })
    const page = await listAuditEvents({ limit: 50, cursor: "page-1", action: "backup.create" })
    expect(page.next_cursor).toBe("page-2")
    expect(page.events).toHaveLength(1)
  })
})

describe("OIDC security API", () => {
  let mock: MockAdapter
  beforeEach(() => { mock = new MockAdapter(api) })
  afterEach(() => mock.restore())

  it("maps every OIDC request", async () => {
    mock.onGet("/auth/oidc/providers").reply(200, [{ id: "p1", slug: "issuer", display_name: "IdP" }])
    expect(await listPublicOIDCProviders()).toEqual([{ id: "p1", slug: "issuer", display_name: "IdP" }])

    mock.onPost("/auth/oidc/providers/p1/begin").reply(200, { authorization_url: "https://idp/auth" })
    expect(await beginOIDCLogin("p1", "/admin")).toBe("https://idp/auth")

    mock.onGet("/admin/oidc/identities/me").reply(200, [{ id: "id-1", provider_name: "IdP" }])
    expect((await listMyExternalIdentities())[0].provider_name).toBe("IdP")

    mock.onPost("/admin/oidc/providers/p1/link").reply(200, { authorization_url: "https://idp/link" })
    expect(await beginOIDCLink("p1", "/admin/security")).toBe("https://idp/link")

    mock.onDelete("/admin/oidc/identities/id-1").reply(204)
    await unlinkExternalIdentity("id-1")

    mock.onGet("/admin/oidc/providers").reply(200, [{ id: "p1" }])
    expect(await listAdminOIDCProviders()).toHaveLength(1)

    mock.onPost("/admin/oidc/providers").reply(201, { id: "p1", client_secret_configured: true })
    expect((await createOIDCProvider({
      slug: "issuer", display_name: "IdP", issuer_url: "https://idp",
      client_id: "c", client_secret: "s", scopes: ["openid"], enabled: true, sort_order: 0,
    })).client_secret_configured).toBe(true)

    mock.onPut("/admin/oidc/providers/p1").reply(200, { id: "p1", enabled: false })
    expect((await updateOIDCProvider("p1", {
      slug: "issuer", display_name: "IdP", issuer_url: "https://idp",
      client_id: "c", client_secret: "s", scopes: ["openid"], enabled: false, sort_order: 0,
    })).enabled).toBe(false)

    mock.onDelete("/admin/oidc/providers/p1").reply(204)
    await deleteOIDCProvider("p1")
  })
})
