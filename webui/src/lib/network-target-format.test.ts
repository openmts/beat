import { describe, expect, it } from "vitest"

import { networkTargetFormatError, normalizeTargetForType } from "@/lib/network-target-format"

describe("network target format", () => {
  it("converts HTTP targets when switching to TCP and ICMP", () => {
    expect(normalizeTargetForType("tcp", "http://example.test:8080/path")).toBe("example.test:8080")
    expect(normalizeTargetForType("tcp", "https://example.test/path")).toBe("example.test:443")
    expect(normalizeTargetForType("icmp", "https://example.test/path")).toBe("example.test")
    expect(normalizeTargetForType("icmp", "example.test:22")).toBe("example.test")
  })

  it("converts host-style targets back to HTTP", () => {
    expect(normalizeTargetForType("http", "example.test:8080")).toBe("http://example.test:8080")
    expect(normalizeTargetForType("http", "example.test")).toBe("http://example.test/")
    expect(normalizeTargetForType("http", "https://example.test/path")).toBe("https://example.test/path")
  })

  it("normalizes full-width colons and zero-width characters", () => {
    expect(normalizeTargetForType("tcp", "example.test：443")).toBe("example.test:443")
    expect(normalizeTargetForType("tcp", "example.test:\u200b443")).toBe("example.test:443")
    expect(networkTargetFormatError("tcp", "example.test：443")).toBeNull()
  })

  it("validates targets with the same shapes as the server", () => {
    expect(networkTargetFormatError("icmp", "example.test")).toBeNull()
    expect(networkTargetFormatError("icmp", "example.test:22")).toBe("icmp")
    expect(networkTargetFormatError("tcp", "example.test:443")).toBeNull()
    expect(networkTargetFormatError("tcp", "example.test")).toBe("tcp")
    expect(networkTargetFormatError("tcp", "http://example.test:80")).toBe("tcp")
    expect(networkTargetFormatError("http", "https://example.test/path")).toBeNull()
    expect(networkTargetFormatError("http", "example.test")).toBe("http")
    expect(networkTargetFormatError("http", "https://user:pass@example.test/path")).toBe("http")
  })
})
