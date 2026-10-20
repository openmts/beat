import MockAdapter from "axios-mock-adapter"
import { afterEach, beforeEach, describe, expect, it } from "vitest"
import { api } from "@/lib/api-client"
import { getPublicNodeSummary, getPublicWorkspaceSources } from "@/lib/public-workspace-api"
import { workspaceDashboard, workspaceMetric, workspaceNode, workspaceSources } from "@/test/metric-workspace-fixtures"

describe("public workspace read boundaries", () => {
  let mock: MockAdapter
  beforeEach(() => { mock = new MockAdapter(api) })
  afterEach(() => mock.restore())
  it("loads only public endpoints and filters the public index and catalog", async () => {
    const controller = new AbortController()
    mock.onGet("/metrics/dashboards/default").reply(200, { ...workspaceDashboard(), scope: "public" })
    mock.onGet("/metrics/catalog").reply(200, { version: 1, revision: 1, metrics: [workspaceMetric(), { ...workspaceMetric("secret"), public: false }] })
    mock.onGet("/nodes").reply(200, [workspaceNode(), { ...workspaceNode("private"), is_public: false }])
    mock.onGet("/groups").reply(200, [...workspaceSources().groups, { id: "private-group", name: "PRIVATE_GROUP" }])
    const sources = await getPublicWorkspaceSources({ nodeId: "node-one", signal: controller.signal })
    expect(sources.nodes.map((node) => node.id)).toEqual(["node-one"])
    expect(sources.catalog.metrics.map((metric) => metric.key)).toEqual(["cpu"])
    expect(sources.groups.map((group) => group.id)).toEqual(["group-one"])
    expect(mock.history.get.map((request) => request.url)).not.toContainEqual(expect.stringContaining("admin"))
    expect(mock.history.get.every((request) => request.signal === controller.signal)).toBe(true)
    expect(mock.history.get[0].params).toEqual({ node_id: "node-one" })
  })
  it("rejects a private template and a hidden or mismatched summary", async () => {
    mock.onGet("/metrics/dashboards/default").reply(200, workspaceDashboard())
    mock.onGet("/metrics/catalog").reply(200, { version: 1, revision: 1, metrics: [] })
    mock.onGet("/nodes").reply(200, [])
    mock.onGet("/groups").reply(200, [])
    const signal = new AbortController().signal
    await expect(getPublicWorkspaceSources({ nodeId: "node-one", signal })).rejects.toThrow()
    for (const node of [{ ...workspaceNode(), is_public: false }, workspaceNode("another")]) {
      mock.onGet("/nodes/node-one").reply(200, node)
      await expect(getPublicNodeSummary({ nodeId: "node-one", signal })).rejects.toThrow()
    }
    mock.onGet("/nodes/node-one").reply(200, workspaceNode())
    expect((await getPublicNodeSummary({ nodeId: "node-one", signal })).id).toBe("node-one")
  })
})
