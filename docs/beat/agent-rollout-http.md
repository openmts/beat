# Agent rollout HTTP contract

Status: implemented and centrally registered. SQLite migration v15 creates the
rollout tables, and the authenticated server routes below are mounted by the
central router. Rollout remains disabled until `BEAT_AGENT_ROLLOUT_CONFIG`
provides the out-of-band signing trust and Sigstore policy. Deployment state is
tracked separately in [local preview](./local-preview.md).

The central router wraps owner routes with session, recent-proof and CSRF
middleware and Agent routes with active per-node authentication. The service
independently rechecks current credentials and owner authority inside its
transactions.

All JSON responses use `{code,message,data}`. Success is HTTP 200, including
`data:null` for no assignment or completed commands. Errors use fixed redacted
messages: 400 invalid payload/signature, 401 missing or invalid identity,
403 authority denied, 404 unavailable resource or disabled module, 409
conflicting state, 413 oversized request, 428 recent proof required, 429
admission or upstream rate limit, and 503 dependency or concurrency capacity
failure. Busy responses and both rate-limit paths include `Retry-After`.
Responses set no-store, nosniff and no-referrer.

## Owner routes

All paths below start with `/api/v1/admin/`. Reads require a current owner
session; writes additionally require recent proof, rechecked before commit.

| Method | Path | Input / data |
| --- | --- | --- |
| GET | `agent-rollout-status` | `Status`, also readable when disabled |
| GET | `agent-releases` | Pagination / `Page<Release>` |
| GET | `agent-releases/remote` | Remote catalog page / `Page<RemoteRelease>` |
| POST | `agent-releases/remote/{tag}/sync` | Empty / `ReleaseDetail` |
| GET | `agent-releases/{release_id}` | `ReleaseDetail` |
| POST | `agent-releases/import` | `SignedManifest` / `Release` |
| PUT | `agent-releases/{release_id}/artifacts/{artifact_id}` | Raw `application/octet-stream` / null |
| POST | `agent-releases/{release_id}/revoke` | Empty / null |
| DELETE | `agent-releases/{release_id}` | Empty / null |
| GET | `agent-rollouts` | Pagination / `Page<Rollout>` |
| POST | `agent-rollouts` | `CreateInput` / `Rollout` |
| GET | `agent-rollouts/{rollout_id}` | `Rollout` |
| GET | `agent-rollouts/{rollout_id}/targets` | Pagination / `Page<Target>` |
| DELETE | `agent-rollouts/{rollout_id}` | Empty / null |
| POST | `agent-rollouts/{rollout_id}/promote` | Empty / null |
| POST | `agent-rollouts/{rollout_id}/pause` | Empty / null |
| POST | `agent-rollouts/{rollout_id}/resume` | `{acknowledge_uncertainty:boolean}` / null |
| POST | `agent-rollouts/{rollout_id}/abort` | Empty / null |

Pagination accepts `limit` (default 50, 1–100) and opaque `cursor`. Data is
`{items:[],next_cursor:""}`; pass `next_cursor` unchanged, and empty means end.
Ordering is stable by ID. Targets use their immutable snapshot metadata. Target
pages and rollout details also return `server_time` (RFC3339), sampled by the
service for that read. Other pages do not include this field.

The remote catalog accepts only an optional `page` (1–100) and returns the fixed
repository, releases, readiness reasons and an optional `next_page`. Its sync
endpoint requires a canonical `v`-prefixed semantic-version tag and imports each
manifest and artifact through the same signature, digest and platform checks.
GitHub rate limits are returned as 429 with `Retry-After`. Remote discovery is
disabled when the signed-release module is disabled. The sync response overrides
the 30-second global write timeout with a 16-minute deadline aligned to the
import budget; failure to set that deadline returns 503.

A rollout can be deleted only after it reaches `completed` or `aborted` and no
target is `offered` or `observing`. A release can be deleted only after it is
revoked and no rollout references it. Artifact files are removed only when no
remaining release references their digest.

`CreateInput` fields: `name`, `release_id`, `node_ids`, `group_ids`, `tags`,
`policy`, `action` (`upgrade`, default, or `rollback`), `acknowledge_rollback`.
Rollback requires the explicit acknowledgement, persists its action, emits
`rollout.rollback.create`, and permits only a strictly older target version.
It uses the same signature, trust, compatibility, observation and manual-wave
checks as upgrade. Existing assignments are not silently canceled by rollback.

`Rollout`: `id`, `release_id`, `name`, `action`, `state`, `reason`, `current_wave`,
`total_waves`, `policy`. Policy fields are the JSON fields in
`agentrollout.Policy`; observation is 30–3600 seconds. Created rollouts are paused
awaiting explicit promotion. Pause/abort describe server dispatch state; they do
not claim to undo changes already applied by an Agent.

`Release`: `id`, `manifest`, `verified`, `revoked`. `ReleaseDetail` adds
`artifacts:[{id,state,size,sha256}]`. `Target`: `id`, `node_id`, `name`,
`source_version`, `os`, `arch`, `state`, `reason`, `wave`, `healthy_since`,
`last_report_at`, `lease_expires_at`. The three target times are actual persisted
values, encoded as RFC3339 or null; the UI must not infer missing samples. Owner metadata omits
signature bundles, nonce values/hashes and internal filesystem paths.

`Status` contains `enabled`, `configured`, `ready`, and fixed `reason`:
`disabled`, `unconfigured`, `unavailable`, or `ready`. A disabled instance can
answer without reading rollout tables. Configured means the service has loaded
its signing policy, verifier and keys; ready additionally requires the current
persisted policy and at least one valid, unrevoked matching key. Owner/session
query failures return the usual 403/503 instead of claiming disabled. Reads need
no recent proof. Initialization rejects invalid enabled configuration.

SignedManifest binary fields use standard JSON base64: `payload`, `signature`,
`sigstore_bundle`. Payload itself is the canonical protocol manifest JSON.
Request JSON is capped at 2 MiB; unknown fields, extra JSON values and compressed
request bodies are rejected. Artifact uploads use the same JSON-envelope
response but accept a bounded `application/octet-stream` body of at most
512 MiB.

## Agent routes

| Method | Path | Input / data |
| --- | --- | --- |
| POST | `/api/v1/agents/rollout/lease` | Protocol `Capabilities` / `Assignment` or null |
| POST | `/api/v1/agents/rollout/observations` | Protocol `Observation` / null |
| GET | `/api/v1/agents/rollout/artifacts/{assignment_id}/{artifact_id}` | Header nonce / binary stream |

Assignment includes the persisted `action`, real server `server_schema`, signed
manifest, artifact metadata and a single `expires_at`. Its initial deadline is
15 minutes preparation plus the configured observation window (at most 75
minutes total), clamped to manifest and maintenance-window expiry. Observations
never renew this deadline. A lost lease response does not regenerate a nonce or
silently redispatch; expiry enters the existing uncertainty/recovery workflow.

Downloads require `X-Beat-Rollout-Nonce`; query strings are rejected. Only the
exact artifact persisted for that assignment is authorized. The endpoint
supports one RFC byte range, ETag (`"sha256-<digest>"`), Content-Length and
Content-Range, and serves identity encoding. Multi-range requests are rejected.
The Agent retains its own cumulative ten-minute download budget from VerifiedAt;
server streams have a ten-minute deadline bounded by assignment expiry.

At most 16 downloads and 4 uploads are active per service/handler. Each download
opens and hashes a bounded private regular file outside SQLite transactions,
then rechecks authority before use. Reads recheck current active identity,
nonce, original lease, rollout window, release/signature trust and ready artifact
metadata, and reject file size/mtime changes. No long file I/O runs within the
SQLite write transaction. A client must still verify its completed artifact.

Upload stages, hashes and publishes a content-addressed private cache file
before its final transaction. That transaction rechecks owner/recent authority,
release/trust and artifact metadata, then atomically marks ready and audits.
If final authorization/audit/commit fails, a verified private cache file may
remain, but the artifact is not marked ready and is not dispatchable. This is
not a claimed cross-filesystem/database rollback.

## Integration boundaries

Migration v15 owns the release, artifact, rollout, target, attempt and event
tables, including `agent_rollouts.action` and the assignment's selected
`agent_rollout_targets.artifact_id`. The central runtime registers startup and
periodic recovery, readiness, metrics, retention and backup reconciliation.
Root signing/trust policy is still supplied out of band through
`BEAT_AGENT_ROLLOUT_CONFIG`; archived trust is metadata, not authority. Route
logs must never capture nonce headers, Authorization headers or
observation/lease bodies.

## Verification

On 2026-10-25 the CI-aligned gates passed for the current work tree:
`backend-tests`, `backend-static`, both `backend-race` shards,
`backend-privileged`, `scripts`, `themes`, `frontend` and `container`. Backend
tests met the per-package >=90% coverage requirement; static analysis reported
zero lint issues and no reachable vulnerabilities. The frontend ran 1,564 tests
at 90.15% line coverage. This is source-level and isolated-host verification;
the current work tree has not been deployed to the preview instance.
