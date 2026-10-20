import type { AgentReleaseDetail, AgentRollout, AgentRolloutTarget } from "@/types/agent-rollouts"
export function agentRelease(): AgentReleaseDetail {
 return { id: "release/one", verified: true, revoked: false, manifest: { schema_version: 1, version: "2.0.0", commit: "abc", channel: "stable", built_at: "2026-01-01T00:00:00Z", expires_at: "2099-01-01T00:00:00Z", minimum_server_schema: 15, minimum_protocol: 1, maximum_protocol: 2, release_notes_digest: "a".repeat(64), signing_key_id: "release-key", revoked: false,
 artifacts: [{ id: "linux/amd64", os: "linux", arch: "amd64", kind: "binary", filename: "beat-agent", entrypoint: "beat-agent", size: 3, sha256: "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad", sbom_digest: "a".repeat(64), provenance_digest: "b".repeat(64), support: "preview" }] }, artifacts: [{ id: "linux/amd64", state: "missing", size: 3, sha256: "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad" }] }
}
export function agentRollout(): AgentRollout {
 return { id: "rollout/one", release_id: "release/one", name: "Canary fleet", action: "upgrade", state: "paused", reason: "awaiting_promotion", current_wave: -1, total_waves: 2, policy: { canary_count: 1, wave_size: 10, max_concurrent: 1, observation_seconds: 300, report_freshness_seconds: 60, max_restarts: 0, max_protocol_errors: 0, max_metric_errors: 0, max_task_failures: 0, allow_preview: false, window_start: "2098-01-01T00:00:00Z", window_end: "2098-01-02T00:00:00Z" } }
}
export function agentTarget(): AgentRolloutTarget { return { healthy_since: null, last_report_at: null, lease_expires_at: null, id: "target", node_id: "private-id", name: "Private database", source_version: "1.0.0", os: "linux", arch: "amd64", state: "held", reason: "external_manager", wave: 0 } }
