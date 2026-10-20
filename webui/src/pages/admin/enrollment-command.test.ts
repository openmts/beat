import { describe, expect, it } from "vitest"
import { enrollmentCommand } from "@/pages/admin/enrollment-command"

describe("enrollment command", () => {
  it("keeps https servers on the default transport policy", () => {
    const command = enrollmentCommand("https://beat.example.com", "beat_enroll_v1_secret")
    expect(command).toBe([
      "beat-agent enroll --server https://beat.example.com --grant-stdin <<'BEAT_ENROLL'",
      "beat_enroll_v1_secret",
      "BEAT_ENROLL",
    ].join("\n"))
  })

  it("enables plain http only when the address is not https", () => {
    const command = enrollmentCommand("http://192.168.1.48:8081", "beat_enroll_v1_secret")
    expect(command.split("\n")).toEqual([
      "beat-agent enroll --server http://192.168.1.48:8081 --allow-http --grant-stdin <<'BEAT_ENROLL'",
      "beat_enroll_v1_secret",
      "BEAT_ENROLL",
    ])
  })
})
