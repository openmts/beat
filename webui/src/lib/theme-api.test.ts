import MockAdapter from "axios-mock-adapter"
import { afterEach, beforeEach, describe, expect, it } from "vitest"
import { api } from "@/lib/api-client"
import {
  activateTheme, createThemeMarket, deleteTheme, deleteThemeMarket, exportTheme,
  getActiveTheme, installMarketTheme, listAdminThemes, listThemeMarkets,
  refreshThemeCatalog, rollbackTheme, updateThemeMarket, uploadTheme,
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

    mock.onPost("/admin/theme-markets/m1/themes/t1/install").reply(201, { id: "p2" })
    expect((await installMarketTheme("m1", "t1")).id).toBe("p2")
  })
})