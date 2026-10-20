import MockAdapter from "axios-mock-adapter"
import { afterEach, beforeEach, describe, expect, it } from "vitest"
import { api } from "@/lib/api-client"
import {
  approveEnrollmentRequest,
  createEnrollmentGrant,
  listEnrollmentGrants,
  listEnrollmentRequests,
  rejectEnrollmentRequest,
  revokeEnrollmentGrant,
  rotateEnrollmentGrantSecret,
  updateEnrollmentGrant,
} from "@/lib/enrollment-discovery-api"
import type { EnrollmentGrant, EnrollmentGrantWrite, EnrollmentRequest } from "@/types/enrollment"

const write: EnrollmentGrantWrite = {
  name: "Fleet", mode: "manual", enabled: true, expires_at: "2026-12-01T00:00:00Z",
  default_group_id: "group-1", node_name_prefix: "web", tags: [], remarks: "", public: false,
  ssh_port: 22, source_ip_as_host: false, source_cidrs: [], hostname_pattern: "", claim_recovery: true,
  maximum_enrollments: 10,
}

const grant = { id: "grant-1", name: "Fleet" } as EnrollmentGrant
const request = { id: "request-1", status: "pending" } as EnrollmentRequest

describe("enrollment discovery API", () => {
  let mock: MockAdapter
  beforeEach(() => { mock = new MockAdapter(api) })
  afterEach(() => mock.restore())

  it("loads grants through the owner collection endpoint", async () => {
    mock.onGet("/admin/agent-enrollment/grants").reply(200, { code: 200, data: { grants: [grant] } })
    expect(await listEnrollmentGrants()).toEqual([grant])
  })

  it("creates and updates a grant with the policy payload", async () => {
    mock.onPost("/admin/agent-enrollment/grants", write).reply(201, { code: 201, data: { grant, secret: "beat_enroll_v1_x" } })
    expect(await createEnrollmentGrant(write)).toEqual({ grant, secret: "beat_enroll_v1_x" })
    mock.onPut("/admin/agent-enrollment/grants/grant-1", write).reply(200, { code: 200, data: { grant } })
    expect(await updateEnrollmentGrant("grant-1", write)).toEqual(grant)
  })

  it("rotates and revokes without ever sending the secret", async () => {
    mock.onPost("/admin/agent-enrollment/grants/grant-1/rotate").reply(200, { code: 200, data: { grant, secret: "beat_enroll_v1_y" } })
    mock.onPost("/admin/agent-enrollment/grants/grant-1/revoke").reply(200, { code: 200, data: { grant } })
    expect((await rotateEnrollmentGrantSecret("grant-1")).secret).toBe("beat_enroll_v1_y")
    expect(await revokeEnrollmentGrant("grant-1")).toEqual(grant)
    expect(mock.history.post.every((entry) => !entry.data)).toBe(true)
  })

  it("pages requests with allowlisted filters only", async () => {
    mock.onGet("/admin/agent-enrollment/requests", { params: { status: "pending", limit: 25, cursor: "abc" } })
      .reply(200, { code: 200, data: { requests: [request], next_cursor: "" } })
    const page = await listEnrollmentRequests({ status: "pending", cursor: "abc", limit: 25, grant_id: "" })
    expect(page.requests).toEqual([request])
    expect(mock.history.get[0].params).toEqual({ status: "pending", cursor: "abc", limit: 25 })
  })

  it("approves with final node fields and rejects with a bounded reason", async () => {
    mock.onPost("/admin/agent-enrollment/requests/request-1/approve",
      { node_name: "web-1", group_id: "group-1", tags: ["edge"], remarks: "", public: false, host: "10.0.0.9", ssh_port: 22 })
      .reply(200, { code: 200, data: { request } })
    expect((await approveEnrollmentRequest("request-1", {
      node_name: "web-1", group_id: "group-1", tags: ["edge"], remarks: "", public: false, host: "10.0.0.9", ssh_port: 22,
    })).id).toBe("request-1")
    mock.onPost("/admin/agent-enrollment/requests/request-1/reject", { reason: "policy-conflict" })
      .reply(200, { code: 200, data: { request } })
    expect((await rejectEnrollmentRequest("request-1", "policy-conflict")).id).toBe("request-1")
  })

  it("escapes identifiers before building a path", async () => {
    mock.onPut("/admin/agent-enrollment/grants/a%2Fb", write).reply(200, { code: 200, data: { grant } })
    await updateEnrollmentGrant("a/b", write)
    expect(mock.history.put[0].url).toBe("/admin/agent-enrollment/grants/a%2Fb")
  })
})
