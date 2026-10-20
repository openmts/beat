import MockAdapter from "axios-mock-adapter"
import { afterEach, beforeEach, describe, expect, it } from "vitest"

import { api } from "@/lib/api-client"
import * as retention from "@/lib/application-retention-api"
import type { ApplicationRetentionOverview } from "@/types/application-retention"

const mock = new MockAdapter(api)
const base = "/settings/application-retention"

const overview: ApplicationRetentionOverview = {
  settings: {
    alert_event_days: 180,
    audit_event_days: 90,
    cleanup_hour_utc: 3,
    auto_cleanup_enabled: true,
    updated_at: "2026-10-15T03:00:00Z",
  },
  status: {
    running: false,
    last_started_at: null,
    last_completed_at: null,
    last_status: "never",
    last_error_code: "",
    last_trigger: "",
    last_duration_ms: 0,
    deleted_alert_events: 0,
    deleted_audit_events: 0,
    deleted_sessions: 0,
  },
  session_retention_days: 30,
}

describe("application retention API", () => {
  beforeEach(() => mock.reset())
  afterEach(() => mock.reset())

  it("loads the overview through the unwrapped envelope", async () => {
    mock.onGet(base).reply(200, { code: 200, data: overview })
    expect(await retention.getApplicationRetention()).toEqual(overview)
  })

  it("writes policy fields and drops the server-owned timestamp", async () => {
    let body: unknown
    mock.onPut(base).reply((config) => {
      body = JSON.parse(config.data as string)
      return [200, { code: 200, data: { ...overview.settings, alert_event_days: 30 } }]
    })
    const updated = await retention.updateApplicationRetention(overview.settings)
    expect(updated.alert_event_days).toBe(30)
    expect(body).toMatchObject({ alert_event_days: 180, audit_event_days: 90, cleanup_hour_utc: 3 })
  })

  it("starts a manual cleanup without a body", async () => {
    let method = ""
    mock.onPost(`${base}/run`).reply((config) => {
      method = config.method ?? ""
      return [202, { code: 202, data: { status: "accepted" } }]
    })
    await retention.runApplicationRetention()
    expect(method).toBe("post")
  })

  it("surfaces the 409 conflict message for a concurrent run", async () => {
    mock.onPost(`${base}/run`).reply(409, { error: "application history cleanup is already running" })
    await expect(retention.runApplicationRetention())
      .rejects.toThrow("application history cleanup is already running")
  })
})
