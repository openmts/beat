import MockAdapter from "axios-mock-adapter"
import { afterEach, beforeEach, describe, expect, it } from "vitest"
import { api } from "@/lib/api-client"
import { createAgentEnrollmentToken } from "@/lib/agent-enrollment-api"

describe("agent enrollment API", () => {
  let mock: MockAdapter
  beforeEach(() => { mock = new MockAdapter(api) })
  afterEach(() => mock.restore())

  it("issues a command only through the authenticated mutation endpoint", async () => {
    const payload = { server_url: "https://beat.example.com", allow_http: false }
    const issued = { id: "join-1", token: "one-time-token", server_url: payload.server_url,
      command: "sudo ./beat-agent join --server 'https://beat.example.com' --token 'one-time-token'",
      expires_at: "2026-09-12T01:00:00Z" }
    mock.onPost("/admin/agent-enrollment-tokens", payload).reply(201, { code: 201, data: issued })
    expect(await createAgentEnrollmentToken(payload)).toEqual(issued)
    expect(mock.history.get).toHaveLength(0)
  })

  it("propagates an actionable issuance error", async () => {
    mock.onPost("/admin/agent-enrollment-tokens").reply(429, { message: "too many active invitations" })
    await expect(createAgentEnrollmentToken({ server_url: "http://beat.example.com", allow_http: true }))
      .rejects.toThrow("too many active invitations")
  })
})
