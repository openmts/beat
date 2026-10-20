import MockAdapter from "axios-mock-adapter"
import { afterEach, describe, expect, it } from "vitest"
import { api } from "./api-client"
import * as lifecycle from "./lifecycle-api"
import * as summary from "./fleet-summary-api"
import type { LifecycleWrite, LocationWrite, GeoIPSettingsWrite, LifecycleSettings } from "@/types/node-lifecycle"
import type { FleetSummaryWrite } from "@/types/fleet-summary"
const mock = new MockAdapter(api)
afterEach(() => mock.reset())
describe("lifecycle API contracts", () => {
 it("uses encoded node paths and abortable reads", async () => {
  const signal = new AbortController().signal
  for (const [suffix, read] of [["location", lifecycle.getLocation], ["lifecycle", lifecycle.getLifecycle], ["lifecycle/events", lifecycle.getLifecycleEvents]] as const) { mock.onGet(`/admin/nodes/a%2Fb/${suffix}`).reply(200, { result: suffix }); expect(await read("a/b", signal)).toEqual({ result: suffix }); expect(mock.history.get.at(-1)?.signal).toBe(signal) }
  mock.onPut("/admin/nodes/a%2Fb/location").reply(200, { ok: true }); expect(await lifecycle.saveLocation("a/b", {} as LocationWrite)).toEqual({ ok: true })
  mock.onPost("/admin/nodes/a%2Fb/location/refresh").reply(200, { queued: true }); expect(await lifecycle.refreshLocation("a/b")).toEqual({ queued: true })
  mock.onPut("/admin/nodes/a%2Fb/lifecycle").reply(config => { expect(JSON.parse(config.data).price_micros).toBe("9223372036854775807"); return [200, { ok: true }] }); await lifecycle.saveLifecycle("a/b", { price_micros: "9223372036854775807" } as LifecycleWrite)
 })
 it("preserves settings CAS and handles upload bounds", async () => {
  mock.onGet("/admin/settings/geoip").reply(200, { revision: 7 }); expect(await lifecycle.getGeoIPSettings()).toEqual({ revision: 7 })
  mock.onPut("/admin/settings/geoip").reply(200, { revision: 8 }); expect(await lifecycle.saveGeoIPSettings({ expected_revision: 7 } as GeoIPSettingsWrite)).toEqual({ revision: 8 })
  mock.onGet("/admin/settings/lifecycle").reply(200, { timezone: "UTC" }); expect(await lifecycle.getLifecycleSettings()).toEqual({ timezone: "UTC" })
  mock.onPut("/admin/settings/lifecycle").reply(200, { timezone: "UTC" }); await lifecycle.saveLifecycleSettings({ timezone: "UTC" } as LifecycleSettings)
  mock.onPost("/admin/settings/geoip/mmdb").reply(config => { expect(config.headers?.["If-Match"]).toBe("7"); expect(config.headers?.["Content-Type"]).toBe("application/octet-stream"); return [200, { revision: 8 }] })
  expect(await lifecycle.uploadMMDB(new File(["db"], "db.mmdb"), 7)).toEqual({ revision: 8 })
  await expect(lifecycle.uploadMMDB(new File([], "empty"), 7)).rejects.toThrow("128 MiB")
  await expect(lifecycle.uploadMMDB({ size: 128 * 1024 * 1024 + 1 } as File, 7)).rejects.toThrow("128 MiB")
  mock.onPost("/admin/settings/geoip/mmdb/update").reply(200, { revision: 9 }); await lifecycle.updateMMDB({ account_id: "a", license_key: "l", edition: "GeoLite2-Country", expected_revision: 8 })
  mock.onPost("/admin/settings/geoip/mmdb/rollback").reply(200, { revision: 10 }); await lifecycle.rollbackMMDB(9)
 })
 it("keeps v23 adapters independent and reports unavailable backend errors", async () => {
  const base = "/alerts/fleet-summaries"
  mock.onGet(base).reply(404, { error: "Unavailable" }); await expect(summary.listFleetSummaries()).rejects.toMatchObject({ status: 404 })
  mock.onGet(base).reply(200, []); expect(await summary.listFleetSummaries()).toEqual([])
  mock.onPost(base).reply(200, { id: "id" }); expect(await summary.createFleetSummary({} as FleetSummaryWrite)).toEqual({ id: "id" })
  mock.onPut(`${base}/a%2Fb`).reply(200, { revision: 2 }); await summary.updateFleetSummary("a/b", { expected_revision: 1 } as FleetSummaryWrite & { expected_revision: number })
  mock.onDelete(`${base}/a%2Fb`).reply(config => { expect(JSON.parse(config.data).expected_revision).toBe(2); return [204] }); await summary.deleteFleetSummary("a/b", 2)
  mock.onPost(`${base}/a%2Fb/test-run`).reply(200, { status: "queued" }); expect(await summary.testFleetSummary("a/b")).toEqual({ status: "queued" })
 })
})
