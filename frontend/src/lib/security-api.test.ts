import MockAdapter from "axios-mock-adapter"
import { afterEach, beforeEach, describe, expect, it } from "vitest"
import { api } from "@/lib/api-client"
import {
  beginTOTP, bootstrapAdmin, changeAdminPassword, createAdminUser, deleteAdminUser,
  disableTOTP, enableTOTP, getAdminSession, getAuthState, listAdminSessions,
  listAdminUsers, listAuditEvents, loginAdmin, logoutAdmin, reauthenticateAdmin,
  revokeAdminSession, revokeOtherAdminSessions, updateAdminUser,
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
    mock.onGet("/admin/audit-events").reply((config) => [200, {
      events: [], total: 0, limit: 50, offset: Number(config.params.offset),
    }])
    expect((await listAuditEvents(50)).offset).toBe(50)
  })
})
