import MockAdapter from "axios-mock-adapter"
import { afterEach, beforeEach, describe, expect, it } from "vitest"

import { api } from "@/lib/api-client"
import * as erasure from "@/lib/metric-erasure-api"
import type { BackupRecord } from "@/types/backup"

const mock = new MockAdapter(api)
const requestID = "11111111-1111-4111-8111-111111111111"

function backup(id: string, state: BackupRecord["state"], createdAt: string): BackupRecord {
  return {
    id, filename: `${id}.zip`, source: "generated", state, created_at: createdAt,
    completed_at: createdAt, snapshot_cutoff: createdAt, size_bytes: 1, sqlite_bytes: 1,
    metrics_bytes: 1, metric_rows: 1, error_message: "",
    updated_at: createdAt, reconciled_at: null,
  }
}

describe("metric erasure API", () => {
  beforeEach(() => mock.reset())
  afterEach(() => mock.reset())

  it("lists, reads, creates, retries, and cancels jobs", async () => {
    mock.onGet("/admin/metric-erasure/jobs", { params: { limit: 5 } })
      .reply(200, { code: 200, data: { jobs: [{ id: "job", state: "queued" }] } })
    mock.onGet("/admin/metric-erasure/jobs/job").reply(200, { code: 200, data: { id: "job" } })
    mock.onPost("/admin/metric-erasure/jobs", {
      scope: "fleet_telemetry", request_id: requestID, confirmation: "ERASE FLEET TELEMETRY",
    }).reply(202, { code: 202, data: { job: { id: "job" }, replayed: false } })
    mock.onPost("/admin/metric-erasure/jobs/job/retry").reply(200, { code: 200, data: { id: "job" } })
    mock.onPost("/admin/metric-erasure/jobs/job/cancel").reply(200, { code: 200, data: { id: "job" } })

    expect(await erasure.listMetricErasureJobs({ limit: 5 })).toEqual([{ id: "job", state: "queued" }])
    expect(await erasure.getMetricErasureJob("job")).toMatchObject({ id: "job" })
    const accepted = await erasure.createMetricErasureJob({
      scope: "fleet_telemetry", request_id: requestID, confirmation: "ERASE FLEET TELEMETRY",
    })
    expect(accepted).toMatchObject({ replayed: false })
    expect(await erasure.retryMetricErasureJob("job")).toMatchObject({ id: "job" })
    expect(await erasure.cancelMetricErasureJob("job")).toMatchObject({ id: "job" })
  })

  it("sends deletion confirmations in the request body, never in the path", async () => {
    const seen: Array<{ url?: string; body?: string }> = []
    mock.onDelete("/nodes/n1").reply((config) => {
      seen.push({ url: config.url, body: config.data })
      return [202, { code: 202, data: { job: { id: "node-job" }, replayed: true } }]
    })
    mock.onDelete("/network/tasks/t1").reply((config) => {
      seen.push({ url: config.url, body: config.data })
      return [202, { code: 202, data: { job: { id: "task-job" }, replayed: false } }]
    })

    expect(await erasure.deleteNodeWithErasure("n1", "server-one", requestID))
      .toMatchObject({ replayed: true, job: { id: "node-job" } })
    expect(await erasure.deleteNetworkTaskWithErasure("t1", "Probe", requestID))
      .toMatchObject({ replayed: false, job: { id: "task-job" } })
    expect(JSON.parse(seen[0]?.body ?? "{}")).toEqual({ request_id: requestID, confirmation: "server-one" })
    expect(JSON.parse(seen[1]?.body ?? "{}")).toEqual({ request_id: requestID, confirmation: "Probe" })
  })

  it("maps fixed phrases, targets, and entity scopes", () => {
    expect(erasure.erasurePhrase("fleet_telemetry")).toBe("ERASE FLEET TELEMETRY")
    expect(erasure.erasurePhrase("all_metric_history")).toBe("ERASE ALL METRIC HISTORY")
    expect(erasure.erasurePhrase("node_history")).toBe("")
    expect(erasure.erasureTargetKind("node_history")).toBe("node")
    expect(erasure.erasureTargetKind("delete_node")).toBe("node")
    expect(erasure.erasureTargetKind("network_task_history")).toBe("task")
    expect(erasure.erasureTargetKind("delete_network_task")).toBe("task")
    expect(erasure.erasureTargetKind("fleet_telemetry")).toBe("none")
    expect(erasure.erasureErasesEntity("delete_node")).toBe(true)
    expect(erasure.erasureErasesEntity("node_history")).toBe(false)
    expect(erasure.metricErasureScopes).toHaveLength(6)
  })

  it("tracks active states and only honours backups from the last day", () => {
    for (const state of ["queued", "claimed", "deleting", "compacting", "finalizing"] as const) {
      expect(erasure.erasureActive(state)).toBe(true)
    }
    for (const state of ["succeeded", "failed", "canceled"] as const) {
      expect(erasure.erasureActive(state)).toBe(false)
    }

    const now = Date.parse("2026-10-15T12:00:00Z")
    const records = [
      backup("stale", "ready", "2026-10-10T12:00:00Z"),
      backup("newer", "ready", "2026-10-15T06:00:00Z"),
      backup("older", "ready", "2026-10-15T01:00:00Z"),
      backup("running", "running", "2026-10-15T11:59:00Z"),
    ]
    expect(erasure.latestReadyBackup(records, now)?.id).toBe("newer")
    expect(erasure.latestReadyBackup([records[0] as BackupRecord], now)).toBeUndefined()
  })

  it("generates a 36-character hyphenated request id", () => {
    const generated = erasure.newMetricErasureRequestID()
    expect(generated).toHaveLength(36)
    expect(generated).toMatch(/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/)
    expect(erasure.newMetricErasureRequestID()).not.toBe(generated)
  })
})
