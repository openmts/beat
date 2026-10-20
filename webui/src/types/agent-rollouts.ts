export interface AgentArtifact { id: string; os: string; arch: string; kind: string; filename: string; entrypoint: string; size: number; sha256: string; sbom_digest: string; provenance_digest: string; support: string }
export interface AgentManifest { schema_version: number; version: string; commit: string; channel: string; built_at: string; expires_at: string; minimum_server_schema: number; minimum_protocol: number; maximum_protocol: number; release_notes_digest: string; signing_key_id: string; revoked: boolean; artifacts: AgentArtifact[] }
export interface SignedAgentManifest { payload: string; signature: string; sigstore_bundle: string }
export interface AgentRelease { id: string; manifest: AgentManifest; verified: boolean; revoked: boolean }
export interface AgentReleaseDetail extends AgentRelease { artifacts: { id: string; state: string; size: number; sha256: string }[] }
export interface AgentRolloutPolicy { canary_count: number; wave_size: number; max_concurrent: number; observation_seconds: number; report_freshness_seconds: number; max_restarts: number; max_protocol_errors: number; max_metric_errors: number; max_task_failures: number; allow_preview: boolean; window_start: string; window_end: string }
export interface AgentRolloutInput { name: string; release_id: string; node_ids: string[]; group_ids: string[]; tags: string[]; policy: AgentRolloutPolicy; action: "upgrade" | "rollback"; acknowledge_rollback: boolean }
export interface AgentRollout { id: string; release_id: string; name: string; action: string; state: string; reason: string; current_wave: number; total_waves: number; policy: AgentRolloutPolicy }
export interface AgentRolloutStatus { enabled: boolean; configured: boolean; ready: boolean; reason: "disabled" | "unconfigured" | "unavailable" | "ready" }
export interface AgentRolloutTarget { healthy_since: string | null; last_report_at: string | null; lease_expires_at: string | null; id: string; node_id: string; name: string; source_version: string; os: string; arch: string; state: string; reason: string; wave: number }
export interface AgentPage<T> { server_time?: string; items: T[]; next_cursor: string }
export type AgentRolloutCommand = "promote" | "pause" | "resume" | "abort"
