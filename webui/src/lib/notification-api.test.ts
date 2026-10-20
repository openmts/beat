import MockAdapter from "axios-mock-adapter"
import { afterEach, describe, expect, it } from "vitest"
import { api } from "@/lib/api-client"
import * as notification from "@/lib/notification-api"
import { alertChannelFormFrom, alertChannelPayload } from "@/pages/admin/alert-channel-config"
import { notificationChannel, notificationDelivery, notificationSettings } from "@/test/notification-fixtures"

const mock = new MockAdapter(api)
afterEach(() => mock.reset())

describe("notification API contracts", () => {
  it("reads filtered history with cancellation and follows a delivery ID safely", async () => {
    const signal = new AbortController().signal
    const item = notificationDelivery()
    mock.onGet("/notifications/deliveries?state=pending&channel_id=c%2F1&offset=25&limit=25").reply(200, { items: [item], total: 30, offset: 25, limit: 25 })
    expect((await notification.listNotificationDeliveries({ state: "pending", channel_id: "c/1", offset: 25, limit: 25 }, signal)).items).toEqual([item])
    expect(mock.history.get[0].signal).toBe(signal)
    mock.onGet("/notifications/deliveries/d%2F1").reply(200, item)
    expect(await notification.getNotificationDelivery("d/1", signal)).toEqual(item)
    mock.onGet("/notifications/deliveries?").reply(200, { items: [], total: 0, offset: 0, limit: 25 })
    expect((await notification.listNotificationDeliveries({ state: undefined, channel_id: "" })).total).toBe(0)
  })
  it("uses separate toggle, retry and cancellation operations", async () => {
    mock.onPatch("/alerts/channels/c%2F1/enabled", { enabled: false }).reply(200, { enabled: false })
    expect(await notification.setNotificationChannelEnabled("c/1", false)).toEqual({ enabled: false })
    const retry = { id: "retry", channel_id: "c1", channel_revision: 3, state: "pending", attempt_count: 0 }
    mock.onPost("/notifications/deliveries/d1/retry", { confirm_duplicate: true }).reply(202, retry)
    expect(await notification.retryNotificationDelivery("d1")).toEqual(retry)
    mock.onPost("/notifications/deliveries/d1/cancel").reply(204)
    await notification.cancelNotificationDelivery("d1")
  })
  it("previews the same reviewed configuration including explicit clears", async () => {
    const form = alertChannelFormFrom(notificationChannel())
    form.clearSecrets = ["headers"]
    mock.onPost("/alerts/channels/preview").reply(200, { method: "POST", url: "masked", headers: {}, body: "preview", content_type: "application/json", subject: "Preview", text: "Preview" })
    expect((await notification.previewNotificationChannel(alertChannelPayload(form), "c1")).body).toBe("preview")
    const body = JSON.parse(mock.history.post[0].data)
    expect(body).toMatchObject({ channel_id: "c1", expected_revision: 3, clear_secrets: ["headers"], config: {} })
    expect(body.data["event.kind"]).toBe("alert.triggered")
  })
  it("uses settings revisions and advertises only available event kinds", async () => {
    const settings = notificationSettings()
    mock.onGet("/notifications/settings").reply(200, settings)
    expect(await notification.getNotificationSettings()).toEqual(settings)
    mock.onPut("/notifications/settings").reply(200, { ...settings, revision: 2 })
    expect((await notification.saveNotificationSettings(settings)).revision).toBe(2)
    expect(JSON.parse(mock.history.put[0].data)).toEqual({ success_retention_days: 30, dead_retention_days: 90, maximum_workers: 8, expected_revision: 1, egress_policy: { exceptions: [] } })
    mock.onGet("/notifications/event-kinds").reply(200, { items: [{ kind: "alert.triggered", available: true }, { kind: "future", available: false }] })
    expect(await notification.getNotificationEventKinds()).toEqual(["alert.triggered"])
  })
})
