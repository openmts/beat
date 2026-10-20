import MockAdapter from "axios-mock-adapter"
import { afterEach, beforeEach, describe, expect, it } from "vitest"
import { api } from "@/lib/api-client"
import {
  activateTheme, createThemeMarket, deleteTheme, deleteThemeMarket, exportTheme,
  getActiveTheme, installMarketTheme, listAdminThemes, listThemeMarkets,
  refreshThemeCatalog, rollbackTheme, updateThemeMarket, uploadTheme, previewTheme, configureTheme, useBuiltInTheme,
} from "@/lib/theme-api"

describe("theme API", () => {
  let mock: MockAdapter
  beforeEach(() => { mock = new MockAdapter(api) })
  afterEach(() => mock.restore())

  it("maps every theme request", async () => {
    mock.onGet("/themes/active").reply(200, { built_in: true, theme_id: "built-in", name: "Built-in", version: "1.0.0", runtime: "declarative", protocol: 1 })
    expect((await getActiveTheme()).built_in).toBe(true)

    mock.onGet("/admin/themes").reply(200, [{ id: "p1", theme_id: "t1" }])
    expect(await listAdminThemes()).toHaveLength(1)

    mock.onPost("/admin/themes/upload").reply(201, { id: "p1" })
    expect((await uploadTheme(new Blob(["x"]))).id).toBe("p1")

    mock.onGet("/admin/themes/p1/export").reply(200, new Blob(["zip"]))
    expect(await exportTheme("p1")).toBeInstanceOf(Blob)

    mock.onPost("/admin/themes/p1/activate").reply(200, { id: "p1", is_active: true })
    expect((await activateTheme("p1")).is_active).toBe(true)

    mock.onPost("/admin/themes/rollback").reply(200, { rollback_target: "built-in" })
    expect((await rollbackTheme()).rollback_target).toBe("built-in")

    mock.onGet("/admin/themes/p1/preview").reply(200, { package_id: "p1", runtime: "declarative" })
    expect((await previewTheme("p1")).package_id).toBe("p1")
    mock.onPut("/admin/themes/p1/configuration", { radius: 0 }).reply(200, { package_id: "p1", configuration: '{"radius":0}' })
    expect((await configureTheme("p1", { radius: 0 })).configuration).toBe('{"radius":0}')
    mock.onPost("/admin/themes/built-in").reply(200, { active_theme: "built-in" })
    await useBuiltInTheme()

    mock.onDelete("/admin/themes/p1").reply(204)
    await deleteTheme("p1")

    mock.onGet("/admin/theme-markets").reply(200, [{ id: "m1" }])
    expect(await listThemeMarkets()).toHaveLength(1)

    mock.onPost("/admin/theme-markets").reply(201, { id: "m1" })
    expect((await createThemeMarket({ name: "M", catalog_url: "https://x", public_key: "k", key_fingerprint: "", enabled: true, sort_order: 0 })).id).toBe("m1")

    mock.onPut("/admin/theme-markets/m1").reply(200, { id: "m1", enabled: false })
    expect((await updateThemeMarket("m1", { name: "M", catalog_url: "https://x", public_key: "k", key_fingerprint: "", enabled: false, sort_order: 0 })).enabled).toBe(false)

    mock.onDelete("/admin/theme-markets/m1").reply(204)
    await deleteThemeMarket("m1")

    mock.onGet("/admin/theme-markets/m1/catalog").reply(200, { schema_version: 1, source_id: "m1", themes: [] })
    expect((await refreshThemeCatalog("m1"))).toHaveProperty("themes")

    mock.onPost("/admin/theme-markets/m1/themes/t1/install", { version: "1.0.0", package_sha256: "a".repeat(64) }).reply(201, { id: "p2" })
    expect((await installMarketTheme("m1", { theme_id: "t1", version: "1.0.0", package_sha256: "a".repeat(64) })).id).toBe("p2")
  })
})

it("uploads the archive body directly without multipart wrapping", async () => {
  const mock = new MockAdapter(api)
  const archive = new Blob([new Uint8Array([0x50, 0x4b, 3, 4])], { type: "application/zip" })
  mock.onPost("/admin/themes/upload").reply((request) => {
    expect(request.data).toBe(archive)
    expect(request.headers?.["Content-Type"]).toBe("application/zip")
    return [201, { id: "uploaded" }]
  })
  try { expect(await uploadTheme(archive)).toEqual({ id: "uploaded" }) } finally { mock.restore() }
})


it("preserves the explicit partial-cleanup response", async () => {
  const mock = new MockAdapter(api)
  const pending = { package_id: "p1", state: "deleted", cleanup_pending: true }
  mock.onDelete("/admin/themes/p1").reply(202, pending)
  try { expect(await deleteTheme("p1")).toEqual(pending) } finally { mock.restore() }
})
