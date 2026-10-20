import { describe, expect, it } from "vitest"
import {
  buildAgentConfig,
  buildAgentCommand,
  buildHealthCommand,
  normalizeServerURL,
} from "@/lib/agent-setup"

describe("agent setup examples", () => {
  it.each([
    ["https://beat.example.com", "https://beat.example.com"],
    [" http://192.168.1.48:8080/ ", "http://192.168.1.48:8080"],
    ["http://localhost:8080", "http://localhost:8080"],
    ["https://[2001:db8::1]:8443/", "https://[2001:db8::1]:8443"],
  ])("normalizes reachable server origins: %s", (input, expected) => {
    expect(normalizeServerURL(input)).toBe(expected)
  })

  it.each([
    "", "beat.example.com", "/admin", "ftp://beat.example.com", "javascript:alert(1)",
    "https://owner:secret@beat.example.com", "https://beat.example.com/admin",
    "https://beat.example.com/api/v1", "https://beat.example.com?token=secret",
    "https://beat.example.com/#fragment", "https://be\nat.example.com",
    "https://", "http://[::1", "http://beat.example.com:99999",
  ])("rejects addresses that cannot be used as the server origin: %s", (input) => {
    expect(normalizeServerURL(input)).toBeNull()
  })

  it("creates a complete JSON example without a real credential", () => {
    expect(JSON.parse(buildAgentConfig("https://beat.example.com"))).toEqual({
      server_url: "https://beat.example.com",
      agent_token: "REPLACE_WITH_NODE_AGENT_TOKEN",
      node_name: "node-one",
      advertised_host: "10.0.0.10",
      ssh_port: 22,
      report_interval: "5s",
    })
  })

  it.each(["amd64", "arm64"] as const)("builds for the selected Linux architecture: %s", (architecture) => {
    expect(buildAgentCommand(architecture)).toContain(`GOOS=linux GOARCH=${architecture}`)
    expect(buildAgentCommand(architecture)).toContain("umask 077")
    expect(buildAgentCommand(architecture)).toContain("-o beat-agent ./cmd/agent")
    expect(buildAgentCommand(architecture)).toContain("chmod 0700 ./beat-agent")
  })

  it("quotes the connectivity check as a single shell argument", () => {
    expect(buildHealthCommand("https://beat.example.com")).toBe(
      "curl --fail --show-error 'https://beat.example.com/readyz'",
    )
    expect(buildHealthCommand("https://beat'$(id).example.com")).toBe(
      "curl --fail --show-error 'https://beat'\\''$(id).example.com/readyz'",
    )
  })
})
