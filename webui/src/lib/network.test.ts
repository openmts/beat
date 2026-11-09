import MockAdapter from "axios-mock-adapter"
import { afterEach, beforeEach, describe, expect, it } from "vitest"

import { api } from "@/lib/api"
import * as networkApi from "@/lib/network-api"
import {
  formatLatency,
  formatNetworkInterval,
  formatPacketLoss,
  latencyRailPercent,
  latestSuccessPercent,
  networkDirectionKey,
  networkNodeLabel,
  networkSummaryByNode,
  networkTypeKey,
} from "@/lib/network-quality"
import type { NetworkTaskPayload, NetworkTaskView } from "@/types"

const mock = new MockAdapter(api)
const payload: NetworkTaskPayload = {
  name: "Probe",
  direction: "agent_to_target",
  type: "tcp",
  target_mode: "single",
  target: "example.com:443",
  targets: [],
  ip_family: "auto",
  interval_seconds: 60,
  timeout_milliseconds: 3000,
  packet_count: 1,
  all_nodes: false,
  enabled: true,
  is_public: true,
  sort_order: 0,
  node_ids: ["node-id"],
}

describe("network API", () => {
  beforeEach(() => mock.reset())
  afterEach(() => mock.reset())

  it("uses public quality and history routes", async () => {
    mock.onGet("/network/quality").reply(200, [{ task: { id: "task" }, nodes: [] }])
    mock.onGet("/network/quality/task/history").reply((config) => {
      expect(config.params).toEqual({ node_id: "node", from: "from", to: "to" })
      return [200, { task_id: "task", node_id: "node", from: "from", to: "to", points: [] }]
    })
    expect(await networkApi.listPublicNetworkQuality()).toHaveLength(1)
    expect(await networkApi.getPublicNetworkHistory("task", { nodeId: "node", from: "from", to: "to" }))
      .toMatchObject({ task_id: "task", node_id: "node" })
  })

  it("uses admin CRUD, sort, and history routes", async () => {
    mock.onGet("/network/tasks").reply(200, [])
    mock.onPost("/network/tasks", payload).reply(201, { id: "created" })
    mock.onPut("/network/tasks/task", payload).reply(200, { id: "updated" })
    mock.onDelete("/network/tasks/task").reply(202, { code: 202, data: { job: { id: "job" }, replayed: false } })
    mock.onPut("/network/tasks/sort", { ids: ["two", "one"] }).reply(200, {})
    mock.onGet("/network/tasks/task/history").reply((config) => {
      expect(config.params).toEqual({ node_id: "node", from: "from", to: "to" })
      return [200, { task_id: "task", node_id: "node", points: [] }]
    })

    expect(await networkApi.listNetworkTasks()).toEqual([])
    expect((await networkApi.createNetworkTask(payload)).id).toBe("created")
    expect((await networkApi.updateNetworkTask("task", payload)).id).toBe("updated")
    await expect(networkApi.deleteNetworkTask("task", "Probe", "11111111-1111-4111-8111-111111111111"))
      .resolves.toMatchObject({ id: "job" })
    await expect(networkApi.sortNetworkTasks(["two", "one"])).resolves.toBeUndefined()
    expect(await networkApi.getAdminNetworkHistory("task", { nodeId: "node", from: "from", to: "to" }))
      .toMatchObject({ task_id: "task", node_id: "node" })
  })
})

describe("network presentation utilities", () => {
  it("humanizes latency and intervals", () => {
    expect(formatLatency(12.6)).toBe("13 ms")
    expect(formatLatency(1234)).toBe("1.2 s")
    expect(formatLatency(12_000)).toBe("12 s")
    expect(formatLatency(-1)).toBe("--")
    expect(formatLatency(null)).toBe("--")
    expect(formatLatency(undefined)).toBe("--")
    expect(formatLatency(Number.NaN)).toBe("--")
    expect(formatPacketLoss(2.345)).toBe("2.3%")
    expect(formatPacketLoss(null)).toBe("--")
    expect(formatPacketLoss(Number.NaN)).toBe("--")
    expect(formatNetworkInterval(30)).toBe("30s")
    expect(formatNetworkInterval(120)).toBe("2m")
    expect(formatNetworkInterval(7200)).toBe("2h")
  })

  it("uses labels and computes latest task state", () => {
    const view = taskView()
    expect(networkNodeLabel(view.nodes[0].node)).toBe("Edge (node-one)")
    expect(networkNodeLabel({ id: "two", name: "node-two", alias: "" })).toBe("node-two")
    expect(networkTypeKey("http")).toBe("network.type.http")
    expect(networkDirectionKey("server_to_agent")).toBe("network.direction.server_to_agent")
    expect(latestSuccessPercent(view)).toBe(50)
    expect(latencyRailPercent(view.nodes[0])).toBe(4)
    expect(latencyRailPercent(view.nodes[1])).toBe(100)
    expect(latencyRailPercent({ node: view.nodes[0].node, latest: null })).toBe(0)
    expect(latestSuccessPercent({ ...view, nodes: [] })).toBeNull()
  })

  it("selects an ICMP summary first and prefers server-to-agent for the same type", () => {
    const views = [
      summaryView("tcp", "agent_to_target", 5, 50, null),
      summaryView("http", "server_to_agent", 0, 40, null),
      summaryView("icmp", "agent_to_target", 1, 30, 2),
      summaryView("icmp", "server_to_agent", 9, 20, 0),
    ]
    expect(networkSummaryByNode(views)["one"]).toMatchObject({
      task_id: "task-icmp-server_to_agent", direction: "server_to_agent", type: "icmp", loss_percent: 0,
    })
    expect(networkSummaryByNode(views.slice(0, 3))["one"]).toMatchObject({
      task_id: "task-icmp-agent_to_target", type: "icmp", loss_percent: 2,
    })
    expect(networkSummaryByNode([{ ...views[0], nodes: [{ ...views[0].nodes[0], latest: null }] }])["one"])
      .toMatchObject({ task_id: "task-tcp-agent_to_target", latency_ms: null, loss_percent: null })
    expect(networkSummaryByNode([
      { ...views[2], nodes: [{ ...views[2].nodes[0], latest: null }] },
      views[0],
    ])["one"]).toMatchObject({ task_id: "task-tcp-agent_to_target", latency_ms: 50 })
  })
})

function summaryView(
  type: "icmp" | "tcp" | "http",
  direction: "agent_to_target" | "server_to_agent",
  sortOrder: number,
  latency: number,
  loss: number | null,
): NetworkTaskView {
  const view = taskView()
  return {
    task: {
      ...view.task,
      id: `task-${type}-${direction}`,
      direction,
      type,
      target: type === "tcp" ? "example.com:443" : type === "http" ? "https://example.com" : "example.com",
      sort_order: sortOrder,
    },
    nodes: [{
      node: view.nodes[0].node,
      latest: {
        timestamp: "2026-07-29T00:00:00Z", latency_ms: latency, success: true,
        status_code: 0, error_code: "none", loss_percent: loss,
      },
    }],
  }
}

function taskView(): NetworkTaskView {
  return {
    task: {
      id: "task",
      name: "Probe",
      direction: "agent_to_target",
      type: "icmp",
      target_mode: "single",
      target: "example.com",
      targets: [],
      ip_family: "auto",
      interval_seconds: 60,
      timeout_milliseconds: 3000,
      packet_count: 5,
      all_nodes: true,
      enabled: true,
      is_public: true,
      sort_order: 0,
      nodes: [],
      created_at: "",
      updated_at: "",
    },
    nodes: [
      {
        node: { id: "one", name: "node-one", alias: "Edge" },
        latest: { timestamp: "", latency_ms: 20, success: true, status_code: 0, error_code: "none" },
      },
      {
        node: { id: "two", name: "node-two", alias: "" },
        latest: { timestamp: "", latency_ms: 1200, success: false, status_code: 0, error_code: "timeout" },
      },
    ],
  }
}
