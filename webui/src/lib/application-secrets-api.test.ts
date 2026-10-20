import MockAdapter from "axios-mock-adapter"
import { afterEach, beforeEach, describe, expect, it } from "vitest"
import { api } from "@/lib/api-client"
import { getApplicationSecretStatus, rotateApplicationDataKey } from "@/lib/application-secrets-api"

describe("application secrets API", () => {
  let mock: MockAdapter
  beforeEach(() => { mock = new MockAdapter(api) })
  afterEach(() => mock.restore())

  it("reads status and requests rotation through the owner endpoints", async () => {
    const state = { healthy: true, root_available: true, registry_valid: true, backfill_complete: true,
      rotation: "idle", generation: 2, data_keys: 2 }
    mock.onGet("/admin/application-secrets").reply(200, { code: 200, data: state })
    mock.onPost("/admin/application-secrets/rotate").reply(202, { code: 202, data: { ...state, rotation: "running" } })
    expect(await getApplicationSecretStatus()).toEqual(state)
    expect(await rotateApplicationDataKey()).toEqual({ ...state, rotation: "running" })
  })

  it("propagates rejected or unavailable rotation errors", async () => {
    mock.onPost("/admin/application-secrets/rotate").reply(409, { message: "rotation is busy" })
    await expect(rotateApplicationDataKey()).rejects.toThrow("rotation is busy")
    mock.onGet("/admin/application-secrets").reply(503, { message: "status unavailable" })
    await expect(getApplicationSecretStatus()).rejects.toThrow("status unavailable")
  })
})
