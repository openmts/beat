import { afterEach, describe, expect, it, vi } from "vitest"
import { reportPublicInteraction } from "@/lib/public-telemetry-transport"

describe("public interaction transport", () => {
  afterEach(() => vi.unstubAllGlobals())

  it("posts one bounded event without credentials", async () => {
    const fetchMock = vi.fn(async () => new Response(null, { status: 204 }))
    vi.stubGlobal("fetch", fetchMock)
    await reportPublicInteraction("page_view", "dashboard")
    expect(fetchMock).toHaveBeenCalledWith("/api/v1/public/interactions", expect.objectContaining({
      method: "POST",
      credentials: "omit",
      keepalive: true,
      body: JSON.stringify({ event: "page_view", surface: "dashboard" }),
    }))
  })

  it("swallows transport failures instead of blocking the page", async () => {
    vi.stubGlobal("fetch", vi.fn(async () => { throw new Error("offline") }))
    await expect(reportPublicInteraction("page_view", "dashboard")).resolves.toBeUndefined()
  })
})
