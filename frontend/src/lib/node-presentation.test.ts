import { describe, expect, it } from "vitest"
import { buildNodeSections, toEditNode } from "@/lib/admin-node-groups"
import { toNodeUpdatePayload } from "@/lib/node-traffic"
import type { ManagedNode } from "@/types"

const node = (overrides: Partial<ManagedNode> = {}): ManagedNode => ({
  id: "node", name: "server", alias: "Edge", group_id: "group", host: "127.0.0.1",
  port: 22, status: "online", ssh_public_key: "", cpu_model: "", os: "linux",
  platform: "ubuntu", os_version: "", kernel: "", arch: "amd64", virtualization: "",
  agent_version: "1", sort_order: 2, tags: ["edge", "prod"], is_public: true,
  public_remark: "Customer edge", private_remark: "Rack 2",
  last_seen: "", created_at: "", updated_at: "", agent_credential_status: "active",
  agent_token_prefix: "beat_agent_v1_test", agent_token_created_at: null,
  agent_token_last_used_at: null, agent_token_revoked_at: null,
  ...overrides,
})

describe("node presentation helpers", () => {
  it("orders nodes and searches labels and remarks", () => {
    const sections = buildNodeSections({
      nodes: [node(), node({
        id: "first", name: "alpha", sort_order: 0, tags: ["core"],
        public_remark: "", private_remark: "",
      })],
      groups: [{ id: "group", name: "Production" }],
      search: "rack 2",
      unassignedName: "Unassigned",
    })
    expect(sections).toHaveLength(1)
    expect(sections[0].nodes.map((item) => item.id)).toEqual(["node"])

    const sorted = buildNodeSections({
      nodes: [node(), node({ id: "first", name: "alpha", sort_order: 0 })],
      groups: [{ id: "group", name: "Production" }],
      search: "",
      unassignedName: "Unassigned",
    })
    expect(sorted[0].nodes.map((item) => item.id)).toEqual(["first", "node"])
  })

  it("round trips presentation fields through the edit payload", () => {
    const editable = toEditNode(node())
    expect(editable).toMatchObject({
      sortOrder: "2", tags: "edge, prod", isPublic: true,
      publicRemark: "Customer edge", privateRemark: "Rack 2",
    })
    expect(toNodeUpdatePayload(editable)).toMatchObject({
      sort_order: 2, tags: ["edge", "prod"], is_public: true,
      public_remark: "Customer edge", private_remark: "Rack 2",
    })
  })
})
