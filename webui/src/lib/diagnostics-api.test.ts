import MockAdapter from "axios-mock-adapter"
import { afterEach, describe, expect, it } from "vitest"

import { api } from "./api-client"
import * as diagnostics from "./diagnostics-api"

const mock = new MockAdapter(api)
afterEach(() => mock.reset())

describe("diagnostics API contracts", () => {
  it("reads the policy, targets and capabilities from the owner control plane", async () => {
    mock.onGet("/admin/diagnostics/policy").reply(200, { enabled: true })
    expect(await diagnostics.getDiagnosticPolicy()).toEqual({ enabled: true })
    mock.onGet("/admin/diagnostics/targets").reply(200, [{ id: "t1" }])
    expect(await diagnostics.listDiagnosticTargets()).toEqual([{ id: "t1" }])
    mock.onGet("/admin/diagnostics/capabilities").reply(200, [{ node_id: "n1" }])
    expect(await diagnostics.listDiagnosticCapabilities()).toEqual([{ node_id: "n1" }])
  })

  it("only sends the filter fields that are set", async () => {
    mock.onGet("/admin/diagnostics/jobs").reply((config) => {
      expect(config.params).toEqual({ state: "failed", node_id: "n1", target_id: undefined, limit: 25 })
      return [200, []]
    })
    expect(await diagnostics.listDiagnosticJobs({ state: "failed", nodeId: "n1", limit: 25 })).toEqual([])
    mock.onGet("/admin/diagnostics/jobs").reply((config) => {
      expect(config.params).toEqual({ state: undefined, node_id: undefined, target_id: undefined, limit: undefined })
      return [200, []]
    })
    await diagnostics.listDiagnosticJobs()
  })

  it("creates a job from an approved payload", async () => {
    mock.onPost("/admin/diagnostics/jobs").reply((config) => {
      expect(JSON.parse(config.data)).toMatchObject({ source_node_id: "n1", target_id: "t1", kind: "iperf3", confirmation: "bandwidth-impact-accepted" })
      return [200, { id: "job-1" }]
    })
    const created = await diagnostics.createDiagnosticJob({
      source_node_id: "n1", target_id: "t1", kind: "iperf3", protocol: "tcp", direction: "forward", ip_family: "ipv4",
      probes: 0, hop_limit: 0, duration_seconds: 10, parallel_streams: 1, bitrate_mbps: 10,
      confirmation: "bandwidth-impact-accepted",
    })
    expect(created).toEqual({ id: "job-1" })
  })

  it("reads one job with its decrypted hops", async () => {
    mock.onGet("/admin/diagnostics/jobs/job-1").reply(200, { job: { id: "job-1" }, hops: [] })
    expect(await diagnostics.getDiagnosticJob("job-1")).toEqual({ job: { id: "job-1" }, hops: [] })
  })

  it("cancels, retries and deletes jobs on their own paths", async () => {
    mock.onPost("/admin/diagnostics/jobs/job-1/cancel").reply(202)
    await diagnostics.cancelDiagnosticJob("job-1")
    mock.onPost("/admin/diagnostics/jobs/job-1/retry").reply(200, { id: "job-1", attempt: 2 })
    expect(await diagnostics.retryDiagnosticJob("job-1")).toEqual({ id: "job-1", attempt: 2 })
    mock.onDelete("/admin/diagnostics/jobs/job-1").reply(204)
    await diagnostics.deleteDiagnosticJob("job-1")
    expect(mock.history.post.map((request) => request.url)).toEqual(["/admin/diagnostics/jobs/job-1/cancel", "/admin/diagnostics/jobs/job-1/retry"])
    expect(mock.history.delete.map((request) => request.url)).toEqual(["/admin/diagnostics/jobs/job-1"])
  })

  it("maps series queries onto the MTS read endpoint", async () => {
    mock.onGet("/admin/diagnostics/jobs/job-1/results").reply((config) => {
      expect(config.params).toEqual({ metric: "diagnostic_mtr_latency_ms", hop: 3, stat: "avg", direction: "forward", protocol: "tcp", role: "sender", limit: 500 })
      return [200, { metric: "diagnostic_mtr_latency_ms", unit: "ms", points: [] }]
    })
    expect(await diagnostics.getDiagnosticSeries("job-1", {
      metric: "diagnostic_mtr_latency_ms", hop: 3, stat: "avg", direction: "forward", protocol: "tcp", role: "sender", limit: 500,
    })).toMatchObject({ points: [] })

    mock.onGet("/admin/diagnostics/jobs/job-1/results").reply((config) => {
      expect(config.params).toEqual({ metric: "diagnostic_iperf_bytes", hop: undefined, stat: undefined, direction: undefined, protocol: undefined, role: undefined, limit: undefined })
      return [200, { metric: "diagnostic_iperf_bytes", unit: "B", points: null }]
    })
    await diagnostics.getDiagnosticSeries("job-1", { metric: "diagnostic_iperf_bytes" })
  })
})
