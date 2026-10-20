import { describe, expect, it } from "vitest"

import {
  detailSeriesSpecs,
  diagnosticActive,
  diagnosticPreviewInterval,
  diagnosticRetryable,
  diagnosticStateVariant,
  diagnosticStates,
  diagnosticTimeline,
  formatBytes,
  formatDiagnosticValue,
  maskDiagnosticAddress,
} from "@/lib/diagnostics-format"
import type { DiagnosticJob } from "@/lib/diagnostics-api"

function job(overrides: Partial<DiagnosticJob> = {}): DiagnosticJob {
  return {
    id: "job-1", source_node_id: "n1", target_id: "t1", kind: "mtr", state: "running", actor_user_id: "u1",
    actor_username: "owner", confirmation: "", source_node_name: "Tokyo", target_name: "Zurich", target_revision: 1,
    policy_revision: 1, protocol: "icmp", direction: "forward", ip_family: "ipv4", probes: 5, hop_limit: 10,
    duration_seconds: 0, parallel_streams: 0, bitrate_mbps: 0, policy_snapshot: "{}", target_snapshot: "{}",
    capability_digest: "d", reserved_bytes: 0, actual_bytes: 0, attempt: 1, cursor_sequence: 0, cancel_requested: false,
    error_code: "", created_at: "2026-10-15T00:00:00Z", updated_at: "2026-10-15T00:00:00Z",
    leased_at: null, started_at: null, completed_at: null, ...overrides,
  }
}

describe("diagnostic formatting", () => {
  it("maps every state to a distinct badge variant", () => {
    expect(diagnosticStates.map(diagnosticStateVariant)).toEqual([
      "outline", "secondary", "secondary", "default", "secondary", "destructive", "destructive", "outline", "destructive", "destructive",
    ])
    expect(diagnosticStateVariant("queued")).toBe("outline")
    expect(diagnosticStateVariant("succeeded")).toBe("default")
  })

  it("separates active, retryable and final states", () => {
    expect(["queued", "leased", "running"].every((state) => diagnosticActive(state as never))).toBe(true)
    expect(diagnosticActive("failed")).toBe(false)
    expect(["succeeded", "running", "leased", "queued"].map((state) => diagnosticRetryable(state as never))).toEqual([false, false, false, false])
    expect(["partial", "failed", "timed_out", "cancelled", "interrupted", "unknown"].every((state) => diagnosticRetryable(state as never))).toBe(true)
  })

  it("derives the timeline from real job fields", () => {
    const queued = diagnosticTimeline(job())
    expect(queued.map((step) => step.key)).toEqual(["authorized", "leased", "started", "measured", "completed"])
    expect(queued[0]).toMatchObject({ reached: true, current: true })
    expect(queued[1]).toMatchObject({ reached: false, current: false })

    const running = diagnosticTimeline(job({
      leased_at: "2026-10-15T00:00:10Z", started_at: "2026-10-15T00:00:20Z", cursor_sequence: 4,
      updated_at: "2026-10-15T00:00:30Z",
    }))
    expect(running.map((step) => step.reached)).toEqual([true, true, true, true, false])
    expect(running[3]).toMatchObject({ at: "2026-10-15T00:00:30Z", current: true })

    const done = diagnosticTimeline(job({ state: "succeeded", leased_at: "a", started_at: "b", cursor_sequence: 2, completed_at: "2026-10-15T00:01:00Z" }))
    expect(done.every((step) => step.reached)).toBe(true)
    expect(done.some((step) => step.current)).toBe(false)
  })

  it("masks addresses without discarding the network prefix", () => {
    expect(maskDiagnosticAddress("203.0.113.10")).toBe("203.0.113.***")
    expect(maskDiagnosticAddress("2001:db8::1")).toBe("2001:db8:****")
    expect(maskDiagnosticAddress("not-an-address")).toBe("****")
    expect(maskDiagnosticAddress(undefined)).toBe("—")
    expect(maskDiagnosticAddress("")).toBe("—")
  })

  it("formats every diagnostic metric with its unit", () => {
    expect(formatDiagnosticValue("diagnostic_iperf_bytes_per_second", 2048)).toBe("2 KiB/s")
    expect(formatDiagnosticValue("diagnostic_iperf_bytes", 1536)).toBe("1.5 KiB")
    expect(formatDiagnosticValue("diagnostic_mtr_latency_ms", 12.345)).toBe("12.35 ms")
    expect(formatDiagnosticValue("diagnostic_iperf_jitter_ms", 1.5)).toBe("1.5 ms")
    expect(formatDiagnosticValue("diagnostic_mtr_loss_percent", 0.44)).toBe("0.4%")
    expect(formatDiagnosticValue("diagnostic_iperf_loss_percent", 0)).toBe("0%")
    expect(formatDiagnosticValue("diagnostic_iperf_retransmits", 12.6)).toBe("13")
    expect(formatDiagnosticValue("diagnostic_mtr_sent_packets", 120)).toBe("120")
    expect(formatDiagnosticValue("diagnostic_mtr_received_packets", 118)).toBe("118")
    expect(formatDiagnosticValue("custom_metric", 3.14159)).toBe("3.14")
    expect(formatDiagnosticValue("custom_metric", Number.NaN)).toBe("—")
  })

  it("scales byte values and keeps the sign", () => {
    expect(formatBytes(0)).toBe("0 B")
    expect(formatBytes(1023)).toBe("1,023 B")
    expect(formatBytes(1024 * 1024)).toBe("1 MiB")
    expect(formatBytes(-2048)).toBe("-2 KiB")
    expect(formatBytes(1024 ** 4)).toBe("1 TiB")
    expect(formatBytes(1024 ** 5)).toBe("1,024 TiB")
  })

  it("picks the series a job actually produces", () => {
    expect(detailSeriesSpecs(job()).map((spec) => spec.label)).toEqual(["latency", "loss"])
    expect(detailSeriesSpecs(job({ kind: "iperf3", protocol: "tcp" })).map((spec) => spec.label))
      .toEqual(["sender", "receiver", "sender-bytes", "receiver-bytes"])
    expect(detailSeriesSpecs(job({ kind: "iperf3", protocol: "udp" })).map((spec) => spec.label))
      .toEqual(["sender", "receiver", "sender-bytes", "receiver-bytes", "jitter", "loss"])
    expect(diagnosticPreviewInterval).toBe(15_000)
  })
})
