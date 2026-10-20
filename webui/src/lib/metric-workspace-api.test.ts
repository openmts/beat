import MockAdapter from "axios-mock-adapter"
import { afterEach, expect, it } from "vitest"
import { api } from "@/lib/api-client"
import { APIError } from "@/lib/api-error"
import { createMetricDashboard, deleteMetricDashboard, getPublicDashboard, getWorkspaceSources, queryWorkspaceLatest, queryWorkspaceMetrics, rollbackMetricDashboard, saveMetricDashboard } from "@/lib/metric-workspace-api"
import { workspaceDashboard, workspaceQuery, workspaceResult, workspaceSources } from "@/test/metric-workspace-fixtures"

const mock = new MockAdapter(api)
afterEach(() => mock.reset())

it("loads sources and preserves cancellation for every request", async () => {
  const sources = workspaceSources()
  for (const [path, data] of [["/admin/metrics/catalog", sources.catalog], ["/admin/metrics/dashboards", sources.dashboards], ["/nodes/manage", sources.nodes], ["/groups", sources.groups]]) mock.onGet(String(path)).reply(200, { code: 200, data })
  const signal = new AbortController().signal
  expect(await getWorkspaceSources(signal)).toEqual(sources)
  expect(mock.history.get.every((request) => request.signal === signal)).toBe(true)
})

it("uses revisions for saves, deletion and rollback", async () => {
  const dashboard = workspaceDashboard()
  mock.onPost("/admin/metrics/dashboards").reply(201, { code: 201, data: dashboard })
  mock.onPut("/admin/metrics/dashboards/admin-default").reply((config) => {
    expect(JSON.parse(config.data)).toEqual({ definition: dashboard.definition, revision: 0 })
    return [200, { code: 200, data: dashboard }]
  })
  mock.onPost("/admin/metrics/dashboards/admin-default/rollback").reply(200, { code: 200, data: dashboard })
  mock.onDelete("/admin/metrics/dashboards/admin-default").reply((config) => { expect(config.params.revision).toBe(0); return [204] })
  expect(await createMetricDashboard(dashboard.definition)).toEqual(dashboard)
  expect(await saveMetricDashboard(dashboard)).toEqual(dashboard)
  expect(await rollbackMetricDashboard(dashboard)).toEqual(dashboard)
  await deleteMetricDashboard(dashboard)
})

it("keeps public queries separate and preserves conflict and retry statuses", async () => {
  const signal = new AbortController().signal
  mock.onPost("/admin/metrics/query").reply(200, { code: 200, data: workspaceResult() })
  mock.onPost("/metrics/query").reply(200, { code: 200, data: workspaceResult() })
  mock.onGet("/metrics/dashboards/default").reply(200, { code: 200, data: workspaceDashboard() })
  expect(await queryWorkspaceMetrics(workspaceQuery(), { signal })).toEqual(workspaceResult())
  expect(await queryWorkspaceMetrics(workspaceQuery(), { signal, public: true })).toEqual(workspaceResult())
  expect(await getPublicDashboard({ signal, nodeId: "node-one" })).toEqual(workspaceDashboard())
  mock.onPut().reply(409, { message: "revision changed" })
  await expect(saveMetricDashboard(workspaceDashboard())).rejects.toMatchObject({ status: 409, message: "revision changed" })
  expect(new APIError("busy", 429, "5").retryAfter).toBe(5)
  expect(new APIError("busy", 429, "unknown").retryAfter).toBeNull()
})
it("sends instant queries to the matching audience and tolerates a missing series list", async () => {
  const signal = new AbortController().signal
  const latest = { series: [{ ref_id: "A", node_id: "node-one", metric: "cpu", label: "CPU usage", unit: "percent", state: "ok", value: 41.5, sample_at: "2026-10-17T02:00:00.000Z" }] }
  mock.onPost("/admin/metrics/query/latest").reply(200, { code: 200, data: latest })
  mock.onPost("/metrics/query/latest").reply(200, { code: 200, data: {} })
  expect(await queryWorkspaceLatest({ series: [{ ref_id: "A", node_id: "node-one", metric: "cpu" }] }, { signal })).toEqual(latest)
  expect(await queryWorkspaceLatest({ series: [] }, { signal, public: true })).toEqual({ series: [] })
  mock.onPost("/admin/metrics/query/latest").reply(200, undefined)
  await expect(queryWorkspaceLatest({ series: [] }, { signal })).rejects.toThrow("workspace.query_failed")
})

it("rejects GPU results that lose or change the requested device dimension", async () => {
 const signal = new AbortController().signal, device_id = "a".repeat(32)
 const query = workspaceQuery(); query.series[0] = { ...query.series[0], metric: "gpu_utilization_percent", device_id }
 const result = workspaceResult(); result.series[0] = { ...result.series[0], metric: "gpu_utilization_percent" }
 mock.onPost("/metrics/query").reply(200, result)
 await expect(queryWorkspaceMetrics(query, { signal, public: true })).rejects.toThrow("workspace.query_failed")
 result.series[0].device_id = device_id
 mock.onPost("/metrics/query").reply(200, result)
 expect((await queryWorkspaceMetrics(query, { signal, public: true })).series[0].device_id).toBe(device_id)
})
