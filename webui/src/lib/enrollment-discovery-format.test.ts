import { describe, expect, it } from "vitest"
import {
  defaultGrantExpiry,
  grantState,
  joinCIDRs,
  joinEntries,
  localInstantInput,
  parseLocalInstant,
  platformLabel,
  requestStatusKey,
  splitCIDRs,
  splitEntries,
} from "@/lib/enrollment-discovery-format"
import type { EnrollmentGrant } from "@/types/enrollment"

function grant(overrides: Partial<EnrollmentGrant> = {}): EnrollmentGrant {
  return {
    id: "grant-1", name: "Fleet", secret_prefix: "beat_enroll_v1_abcd", mode: "manual", enabled: true,
    expires_at: "2026-12-01T00:00:00Z", default_group_id: "group-1", default_group_name: "Production",
    node_name_prefix: "web", tags: [], remarks: "", public: false, ssh_port: 22, source_ip_as_host: false,
    source_cidrs: [], hostname_pattern: "", claim_recovery: true, maximum_enrollments: 10,
    consumed_enrollments: 2, created_at: "2026-10-01T00:00:00Z", updated_at: "2026-10-01T00:00:00Z",
    ...overrides,
  }
}

describe("enrollment discovery formatting", () => {
  it("derives the four grant states from revocation, expiry and enablement", () => {
    const now = Date.parse("2026-11-01T00:00:00Z")
    expect(grantState(grant(), now)).toBe("active")
    expect(grantState(grant({ enabled: false }), now)).toBe("inactive")
    expect(grantState(grant({ expires_at: "2026-10-31T23:59:59Z" }), now)).toBe("expired")
    expect(grantState(grant({ expires_at: "not-a-date" }), now)).toBe("expired")
    expect(grantState(grant({ revoked_at: "2026-10-20T00:00:00Z" }), now)).toBe("revoked")
  })

  it("maps request statuses onto catalog keys", () => {
    expect(requestStatusKey("claimed")).toBe("discovery.status_claimed")
    expect(requestStatusKey("pending")).toBe("discovery.status_pending")
  })

  it("round-trips instants through the datetime-local control value", () => {
    expect(localInstantInput("2026-12-01T00:00:00Z")).toMatch(/^2026-12-0[12]T\d{2}:\d{2}$/)
    expect(parseLocalInstant("2026-12-01T08:30")).toBe(new Date("2026-12-01T08:30").toISOString())
    expect(localInstantInput("")).toBe("")
    expect(parseLocalInstant("")).toBe("")
    expect(defaultGrantExpiry(Date.parse("2026-10-15T00:00:00Z"))).toMatch(/^2026-11-1[34]T\d{2}:\d{2}$/)
  })

  it("splits and joins lists without leaving empty entries", () => {
    expect(splitEntries(" edge , , core ")).toEqual(["edge", "core"])
    expect(splitEntries("")).toEqual([])
    expect(joinEntries(undefined)).toBe("")
    expect(joinEntries(["edge", "core"])).toBe("edge, core")
    expect(splitCIDRs("10.0.0.0/8\n\n192.168.0.0/16 ")).toEqual(["10.0.0.0/8", "192.168.0.0/16"])
    expect(joinCIDRs(undefined)).toBe("")
    expect(joinCIDRs(["10.0.0.0/8"])).toBe("10.0.0.0/8")
  })

  it("renders a platform only from the reported parts", () => {
    expect(platformLabel("linux", "amd64")).toBe("linux · amd64")
    expect(platformLabel("linux", "")).toBe("linux")
    expect(platformLabel(" ", "")).toBe("")
  })
})
