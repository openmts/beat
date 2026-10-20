import { beforeEach, describe, expect, it, vi } from "vitest"

import { api } from "@/lib/api-client"
import {
  activeCalibration,
  createTrafficCalibration,
  formatCalibrationBytes,
  isCanonicalCalibrationBytes,
  listTrafficCalibrations,
  newCalibrationRequestID,
  parseCalibrationBytes,
  replaceTrafficCalibration,
  revokeTrafficCalibration,
  type TrafficCalibrationEvent,
} from "@/lib/traffic-calibration"

vi.mock("@/lib/api-client", () => ({
  api: { get: vi.fn(), post: vi.fn() },
}))

const getMock = vi.mocked(api.get)
const postMock = vi.mocked(api.post)

function calibrationEvent(overrides: Partial<TrafficCalibrationEvent> = {}): TrafficCalibrationEvent {
  return {
    id: "11111111-1111-1111-1111-111111111111",
    node_id: "node-1",
    node_name: "node",
    kind: "set",
    effective_at: "2026-10-01T00:00:00Z",
    target_sent: "1000",
    target_received: "2000",
    reason: "opening balance",
    actor: { id: "user", name: "owner", session_id: "session", request_id: "req" },
    created_at: "2026-10-01T00:01:00Z",
    ...overrides,
  }
}

describe("traffic calibration byte conversion", () => {
  it("parses binary units into canonical decimal byte strings", () => {
    expect(parseCalibrationBytes("0")).toBe("0")
    expect(parseCalibrationBytes("512 MiB")).toBe(String(512 * 1024 * 1024))
    expect(parseCalibrationBytes("1.5 GiB")).toBe(String(Math.round(1.5 * 1024 ** 3)))
    expect(parseCalibrationBytes("1PiB")).toBe(String(1024 ** 5))
    expect(parseCalibrationBytes("1048576")).toBe("1048576")
  })

  it("rejects malformed or unsupported input instead of guessing", () => {
    for (const value of ["", "-1", "1.23 GiB", "1 GiB extra", "1 XB", "abc", "1e3", "  "]) {
      expect(parseCalibrationBytes(value)).toBeUndefined()
    }
  })

  it("renders canonical byte strings back into binary units", () => {
    expect(formatCalibrationBytes("0")).toBe("0 B")
    expect(formatCalibrationBytes("512")).toBe("512 B")
    expect(formatCalibrationBytes(String(1024))).toBe("1 KiB")
    expect(formatCalibrationBytes(String(1536))).toBe("1.5 KiB")
    expect(formatCalibrationBytes(String(1024 ** 5))).toBe("1 PiB")
    expect(formatCalibrationBytes("not-bytes")).toBe("--")
  })

  it("only accepts canonical decimal strings", () => {
    expect(isCanonicalCalibrationBytes("0")).toBe(true)
    expect(isCanonicalCalibrationBytes("1000")).toBe(true)
    expect(isCanonicalCalibrationBytes("0100")).toBe(false)
    expect(isCanonicalCalibrationBytes("-1")).toBe(false)
  })

  it("generates distinct hyphenated UUID request ids", () => {
    const first = newCalibrationRequestID()
    const second = newCalibrationRequestID()
    expect(first).toMatch(/^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/)
    expect(first).not.toBe(second)
  })
})

describe("traffic calibration API", () => {
  beforeEach(() => {
    getMock.mockReset()
    postMock.mockReset()
  })

  it("lists history with bounded pagination parameters", async () => {
    getMock.mockResolvedValue({ data: { events: [calibrationEvent()], next_cursor: "cursor" } })
    const page = await listTrafficCalibrations("node-1", { limit: 25, cursor: "cursor" })
    expect(getMock).toHaveBeenCalledWith("/admin/nodes/node-1/traffic-calibrations", {
      params: { limit: 25, cursor: "cursor" }, signal: undefined,
    })
    expect(page.events[0]?.kind).toBe("set")
  })

  it("posts set, revoke and replace payloads to the owner endpoints", async () => {
    postMock.mockResolvedValue({ data: { event: calibrationEvent(), replayed: false } })
    const payload = { request_id: "req", target_sent: "1", target_received: "2", reason: "why" }
    await createTrafficCalibration("node-1", payload)
    expect(postMock).toHaveBeenLastCalledWith("/admin/nodes/node-1/traffic-calibrations", payload)
    await replaceTrafficCalibration("node-1", "event-9", payload)
    expect(postMock).toHaveBeenLastCalledWith("/admin/nodes/node-1/traffic-calibrations/event-9/replace", payload)
    await revokeTrafficCalibration("node-1", "event-9", "req-9")
    expect(postMock).toHaveBeenLastCalledWith("/admin/nodes/node-1/traffic-calibrations/event-9/revoke", { request_id: "req-9" })
  })
})

describe("active calibration", () => {
  it("returns the newest set and ignores revoked history", () => {
    const newest = calibrationEvent({ id: "newest" })
    const older = calibrationEvent({ id: "older" })
    const revoke = calibrationEvent({ id: "revoke", kind: "revoke", effective_at: undefined })
    expect(activeCalibration([newest, older, revoke])?.id).toBe("newest")
    expect(activeCalibration([revoke])).toBeUndefined()
    expect(activeCalibration([])).toBeUndefined()
  })
})
