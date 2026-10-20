import { beforeEach, describe, expect, it, vi } from "vitest"
import {
  createPublicTelemetryReporter,
  disabledPublicTelemetry,
  publicSurfaceForPath,
  publicTelemetryConfig,
} from "@/lib/public-telemetry"
import { reportPublicInteraction } from "@/lib/public-telemetry-transport"

vi.mock("@/lib/public-telemetry-transport", () => ({ reportPublicInteraction: vi.fn(async () => undefined) }))

const enabled = { enabled: true, allowedEvents: ["page_view", "node_open"], sampleCeiling: 2 }

describe("public telemetry reporter", () => {
  beforeEach(() => {
    vi.clearAllMocks()
    window.sessionStorage.clear()
  })

  it("stays silent while the policy is disabled", () => {
    const reporter = createPublicTelemetryReporter()
    reporter.configure(disabledPublicTelemetry)
    reporter.report("page_view", "dashboard")
    expect(reportPublicInteraction).not.toHaveBeenCalled()
  })

  it("sends only registered and allowed events", () => {
    const reporter = createPublicTelemetryReporter()
    reporter.configure(enabled)
    reporter.report("page_view", "dashboard")
    reporter.report("node_open", "node")
    expect(reportPublicInteraction).toHaveBeenCalledTimes(2)
    expect(reportPublicInteraction).toHaveBeenNthCalledWith(1, "page_view", "dashboard")
    expect(reportPublicInteraction).toHaveBeenNthCalledWith(2, "node_open", "node")
  })

  it("stops at the session sampling ceiling and survives a reload", () => {
    const reporter = createPublicTelemetryReporter()
    reporter.configure(enabled)
    reporter.report("page_view", "dashboard")
    reporter.report("page_view", "node")
    reporter.report("node_open", "node")
    expect(reportPublicInteraction).toHaveBeenCalledTimes(2)

    const reloaded = createPublicTelemetryReporter()
    reloaded.configure(enabled)
    reloaded.report("page_view", "dashboard")
    expect(reportPublicInteraction).toHaveBeenCalledTimes(2)
  })

  it("clamps a broken ceiling to a single sample", () => {
    const reporter = createPublicTelemetryReporter()
    reporter.configure({ ...enabled, sampleCeiling: 0 })
    reporter.report("page_view", "dashboard")
    reporter.report("page_view", "dashboard")
    expect(reportPublicInteraction).toHaveBeenCalledTimes(1)
  })

  it("keeps reporting when session storage is unavailable", () => {
    const storage = { getItem: () => { throw new Error("blocked") }, setItem: () => { throw new Error("blocked") } }
    const spy = vi.spyOn(window, "sessionStorage", "get").mockReturnValue(storage as unknown as Storage)
    try {
      const reporter = createPublicTelemetryReporter()
      reporter.configure(enabled)
      reporter.report("page_view", "dashboard")
      expect(reportPublicInteraction).toHaveBeenCalledTimes(1)
    } finally {
      spy.mockRestore()
    }
  })

  it("maps only public routes to coarse surfaces", () => {
    expect(publicSurfaceForPath("/")).toBe("dashboard")
    expect(publicSurfaceForPath("/node/node-one")).toBe("node")
    expect(publicSurfaceForPath("/admin/settings")).toBeNull()
    expect(publicSurfaceForPath("/unknown")).toBeNull()
  })

  it("reads the public telemetry view defensively", () => {
    expect(publicTelemetryConfig(undefined)).toEqual(disabledPublicTelemetry)
    expect(publicTelemetryConfig({})).toEqual(disabledPublicTelemetry)
    expect(publicTelemetryConfig({ public_telemetry: { enabled: true, allowed_events: ["page_view"], sample_ceiling: 50 } }))
      .toEqual({ enabled: true, allowedEvents: ["page_view"], sampleCeiling: 50 })
    expect(publicTelemetryConfig({ public_telemetry: { enabled: true, allowed_events: undefined as unknown as string[], sample_ceiling: 1 } }).allowedEvents)
      .toEqual([])
  })
})
