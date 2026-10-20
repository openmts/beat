import { afterEach, describe, expect, it, vi } from "vitest"
import { randomUUID } from "./random-id"

afterEach(() => vi.unstubAllGlobals())

describe("randomUUID", () => {
  it("uses the native implementation when it is available", () => {
    vi.stubGlobal("crypto", { randomUUID: () => "native-id" })
    expect(randomUUID()).toBe("native-id")
  })

  it("falls back to Web Crypto bytes on insecure HTTP origins", () => {
    vi.stubGlobal("crypto", {
      getRandomValues: (bytes: Uint8Array) => {
        bytes.fill(0)
        return bytes
      },
    })
    expect(randomUUID()).toBe("00000000-0000-4000-8000-000000000000")
  })
})
