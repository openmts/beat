import { afterEach, describe, expect, it, vi } from "vitest"
import AxiosMockAdapter from "axios-mock-adapter"
import * as client from "@/lib/api"
import { subscribeAuthInvalidated } from "@/lib/auth"

const mock = new AxiosMockAdapter(client.api)

afterEach(() => mock.reset())

describe("API client", () => {
  it("unwraps response envelopes", async () => {
    mock.onGet("/api/v1/nodes").reply(200, { code: 200, data: [{ id: "node" }] })
    await expect(client.listNodes()).resolves.toEqual([{ id: "node" }])
  })

  it("uses cookie credentials without an authorization header", async () => {
    mock.onPost("/api/v1/groups").reply((config) => {
		expect(config.headers?.Authorization).toBeUndefined()
		expect(config.withCredentials).toBe(true)
      return [201, { code: 201, data: { id: "group", name: "ops" } }]
    })
    await expect(client.createGroup("ops")).resolves.toMatchObject({ id: "group" })
  })

	it("invalidates the cookie session and returns the server message for 401", async () => {
		const listener = vi.fn()
		const unsubscribe = subscribeAuthInvalidated(listener)
    mock.onPost("/api/v1/groups").reply(401, { message: "unauthorized" })
    await expect(client.createGroup("ops")).rejects.toThrow("unauthorized")
		expect(listener).toHaveBeenCalled()
		unsubscribe()
  })

  it("posts batch commands", async () => {
    mock.onPost("/api/v1/terminal/execute", {
      node_ids: ["node"], command: "uptime",
    }).reply(200, { code: 200, data: [{ node_id: "node", output: "ok" }] })
    await expect(client.executeBatchCommand(["node"], "uptime")).resolves.toEqual([
      { node_id: "node", output: "ok" },
    ])
  })

  it("covers all resource operations", async () => {
    mock.onAny().reply((config) => {
      if (config.method === "delete") return [204]
      return [200, { code: 200, data: config.url?.includes("metrics") ? {} : [] }]
    })

    await client.listNodes("group")
    await client.getSiteSettings()
    await client.updateSiteSettings({
      site_title: "Beat", site_description: "Status", logo_url: "", favicon_url: "",
      default_theme: "system", show_ip_addresses: true, show_network_quality: true,
      updated_at: "",
    })
    await client.getMaintenanceOverview()
    await client.updateMaintenanceSettings({
      retention_days: 30, auto_cleanup_enabled: true, cleanup_hour_utc: 3,
      updated_at: "",
    })
    await client.startMaintenance()
    await client.getNode("node")
    await client.updateNode("node", { alias: "alias" })
    await client.updateNodeSort(["node"])
    await client.deleteNode("node")
    await client.listManagedNodes()
    await client.createManagedNode({
      name: "node", host: "host", port: 22, server_url: "https://beat.example",
    })
    await client.rotateAgentToken("node", "https://beat.example")
    await client.revokeAgentToken("node")
    await client.getAgentInstallConfig("node", "https://beat.example")
    await client.getNodeMetrics("node", ["cpu"], "from", "to")
    await client.nodeReport({ name: "node", host: "host", port: 22 })
    await client.listGroups()
    await client.updateGroup("group", "name")
    await client.deleteGroup("group")
    await client.setDefaultGroup("group")
    await client.updateGroupSort(["group"])
    await client.listSSHKeys()
    await client.createSSHKey({ name: "key", public_key: "public" })
    await client.generateSSHKey("key", "ed25519")
    await client.deleteSSHKey("key")
    await client.listAlertRules()
    await client.createAlertRule({ name: "rule", description: "", metric: "cpu", operator: "gt", threshold: 1, duration: 1, severity: "warning", enabled: true })
    await client.updateAlertRule("rule", { enabled: false })
    await client.deleteAlertRule("rule")
    await client.listAlertChannels()
    await client.createAlertChannel({ name: "channel", channel_type: "webhook", config: "https://example.test", enabled: true })
    await client.updateAlertChannel("channel", { enabled: false })
    await client.testAlertChannel("channel")
    await client.deleteAlertChannel("channel")
    await client.listAlertEvents()
    await client.listTrafficReportSchedules()
    const report = {
      name: "Daily", cadence: "daily" as const, timezone: "UTC", send_hour: 8,
      send_minute: 0, weekday: 1, month_day: 1, all_nodes: true, node_ids: [],
      all_channels: true, channel_ids: [], enabled: true,
    }
    await client.createTrafficReportSchedule(report)
    await client.updateTrafficReportSchedule("report", report)
    await client.testTrafficReportSchedule("report")
    await client.deleteTrafficReportSchedule("report")
  })

  it("falls back to generic Axios errors", async () => {
    mock.onGet("/api/v1/nodes").networkError()
    await expect(client.listNodes()).rejects.toThrow("Network Error")
  })
})
